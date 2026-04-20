package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/cloverstd/ccmate/internal/agentprovider"
	"github.com/cloverstd/ccmate/internal/model"
)

// Factory creates Claude Code agent adapters.
type Factory struct{}

func (f *Factory) Name() string { return "claude-code" }

func (f *Factory) Create(cfg agentprovider.AgentConfig) (agentprovider.AgentAdapter, error) {
	binary := cfg.Binary
	if binary == "" {
		binary = "claude"
	}
	permissionMode := cfg.Extra["permission_mode"]
	if permissionMode == "" {
		permissionMode = "bypassPermissions"
	}
	return &Adapter{
		binary:         binary,
		model:          cfg.Model,
		debug:          cfg.Extra["debug"] == "true",
		permissionMode: permissionMode,
		allowedTools:   cfg.Extra["allowed_tools"],
		disallowedTools: cfg.Extra["disallowed_tools"],
	}, nil
}

// Adapter implements AgentAdapter for Claude Code CLI using stream-json bidirectional mode.
type Adapter struct {
	binary          string
	model           string
	debug           bool
	permissionMode  string
	allowedTools    string
	disallowedTools string

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	cancel  context.CancelFunc
	running bool
}

func (a *Adapter) StartSession(ctx context.Context, req agentprovider.StartSessionRequest) (*agentprovider.SessionHandle, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	sessionCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	args := []string{
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
	}

	if a.permissionMode != "" {
		args = append(args, "--permission-mode", a.permissionMode)
	}

	if a.model != "" {
		args = append(args, "--model", a.model)
	}

	if req.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", req.SystemPrompt)
	}

	// Resume mode: continue an existing session
	if req.ResumeSessionID != "" {
		args = append(args, "--resume", req.ResumeSessionID)
	}

	if a.allowedTools != "" {
		args = append(args, "--allowedTools", a.allowedTools)
	}
	if a.disallowedTools != "" {
		args = append(args, "--disallowedTools", a.disallowedTools)
	}

	a.cmd = exec.CommandContext(sessionCtx, a.binary, args...)
	a.cmd.Dir = req.WorkDir
	a.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Inherit parent environment, only filter out CLAUDECODE to prevent nested detection
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, "CLAUDECODE=") {
			filtered = append(filtered, e)
		}
	}
	a.cmd.Env = filtered

	a.running = true

	handle := &agentprovider.SessionHandle{
		ID:       fmt.Sprintf("claude-%p", a.cmd),
		Provider: "claude-code",
	}

	if a.debug {
		fullCmd := a.binary + " " + strings.Join(args, " ")
		handle.DebugInfo = map[string]string{
			"binary":          a.binary,
			"full_command":    fullCmd,
			"workdir":         req.WorkDir,
			"permission_mode": a.permissionMode,
			"model":           a.model,
			"system_prompt_len": fmt.Sprintf("%d", len(req.SystemPrompt)),
			"task_prompt_len":   fmt.Sprintf("%d", len(req.TaskPrompt)),
		}
		for i, arg := range args {
			handle.DebugInfo[fmt.Sprintf("arg_%d", i)] = arg
		}
		slog.Info("[DEBUG] claude command", "command", fullCmd, "workdir", req.WorkDir)
	}

	// Store task prompt to send after start
	handle.DebugInfo = mergeMap(handle.DebugInfo, map[string]string{
		"_task_prompt": req.TaskPrompt,
	})

	return handle, nil
}

// SendInput sends a user message to the running claude process via stdin.
func (a *Adapter) SendInput(ctx context.Context, handle *agentprovider.SessionHandle, input agentprovider.UserInput) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.stdin == nil {
		return fmt.Errorf("no active stdin pipe")
	}

	msg := map[string]interface{}{
		"type": "user",
		"message": map[string]interface{}{
			"role":    "user",
			"content": input.Text,
		},
	}

	return a.writeJSON(msg)
}

func (a *Adapter) StreamEvents(ctx context.Context, handle *agentprovider.SessionHandle) (<-chan model.AgentEvent, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.cmd == nil {
		return nil, fmt.Errorf("no active session")
	}

	stdout, err := a.cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("getting stdout pipe: %w", err)
	}

	stderr, err := a.cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("getting stderr pipe: %w", err)
	}

	stdinPipe, err := a.cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("getting stdin pipe: %w", err)
	}
	a.stdin = stdinPipe

	if err := a.cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting claude: %w", err)
	}

	// Send the initial task prompt via stdin
	taskPrompt := ""
	if handle.DebugInfo != nil {
		taskPrompt = handle.DebugInfo["_task_prompt"]
	}
	if taskPrompt != "" {
		initMsg := map[string]interface{}{
			"type": "user",
			"message": map[string]interface{}{
				"role":    "user",
				"content": taskPrompt,
			},
		}
		if err := a.writeJSON(initMsg); err != nil {
			slog.Error("failed to send initial prompt", "error", err)
		}
	}

	ch := make(chan model.AgentEvent, 64)

	go func() {
		defer close(ch)

		// Read stderr in background
		go func() {
			scanner := bufio.NewScanner(stderr)
			scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
			for scanner.Scan() {
				line := scanner.Text()
				if line == "" {
					continue
				}
				ch <- model.AgentEvent{
					Type:    model.AgentEventError,
					Payload: map[string]interface{}{"message": line},
				}
			}
		}()

		// Parse JSON stream from stdout
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 10*1024*1024) // 64KB base, 10MB max

		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}

			var raw map[string]interface{}
			if err := json.Unmarshal([]byte(line), &raw); err != nil {
				continue
			}

			for _, event := range translateClaudeEvent(raw) {
				select {
				case ch <- event:
				case <-ctx.Done():
					return
				}
			}
		}

		// Wait for process to finish
		exitErr := a.cmd.Wait()
		if exitErr != nil {
			ch <- model.AgentEvent{
				Type:    model.AgentEventError,
				Payload: map[string]interface{}{"message": fmt.Sprintf("claude exited with error: %v", exitErr)},
			}
		}
	}()

	return ch, nil
}

func (a *Adapter) Interrupt(ctx context.Context, handle *agentprovider.SessionHandle) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Gracefully close stdin first — claude will finish current work and exit
	if a.stdin != nil {
		a.stdin.Close()
		a.stdin = nil
	}

	// Send SIGINT instead of SIGKILL for graceful shutdown
	if a.cmd != nil && a.cmd.Process != nil {
		_ = a.cmd.Process.Signal(syscall.SIGINT)
	}

	a.running = false
	return nil
}

func (a *Adapter) Resume(ctx context.Context, handle *agentprovider.SessionHandle) error {
	return fmt.Errorf("use --resume flag with a new session to resume")
}

func (a *Adapter) Close(ctx context.Context, handle *agentprovider.SessionHandle) error {
	return a.Interrupt(ctx, handle)
}

func (a *Adapter) Capabilities() model.AgentCapabilities {
	return model.AgentCapabilities{
		SupportsImage:     true,
		SupportsResume:    false,
		SupportsStreaming: true,
	}
}

// writeJSON sends a JSON message to claude's stdin.
func (a *Adapter) writeJSON(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}
	data = append(data, '\n')
	_, err = a.stdin.Write(data)
	return err
}

// translateClaudeEvent converts Claude Code stream-json events to unified AgentEvents.
// A single raw event may produce multiple unified events (e.g. tool_result blocks).
func translateClaudeEvent(raw map[string]interface{}) []model.AgentEvent {
	eventType, _ := raw["type"].(string)

	switch eventType {
	case "system":
		// System metadata — preserve under raw key, do not leak status/subtype into runner's break condition.
		payload := map[string]interface{}{"subtype_kind": "system", "raw": raw}
		if sid, ok := raw["session_id"].(string); ok {
			payload["session_id"] = sid
		}
		if subtype, ok := raw["subtype"].(string); ok {
			payload["subtype"] = subtype
		}
		return []model.AgentEvent{{Type: model.AgentEventRunStatus, Payload: payload}}

	case "assistant":
		var textParts []string
		var toolCalls []map[string]interface{}

		if msg, ok := raw["message"].(map[string]interface{}); ok {
			if contentBlocks, ok := msg["content"].([]interface{}); ok {
				for _, block := range contentBlocks {
					b, ok := block.(map[string]interface{})
					if !ok {
						continue
					}
					switch b["type"] {
					case "text":
						if text, ok := b["text"].(string); ok {
							textParts = append(textParts, text)
						}
					case "tool_use":
						toolCalls = append(toolCalls, map[string]interface{}{
							"tool_use_id": b["id"],
							"name":        b["name"],
							"input":       b["input"],
						})
					}
				}
			}
		}

		if len(toolCalls) > 0 {
			return []model.AgentEvent{{
				Type: model.AgentEventToolCall,
				Payload: map[string]interface{}{
					"tools": toolCalls,
					"text":  strings.Join(textParts, "\n"),
				},
			}}
		}

		return []model.AgentEvent{{
			Type:    model.AgentEventMessageDelta,
			Payload: map[string]interface{}{"content": strings.Join(textParts, "\n")},
		}}

	case "user":
		// Claude echoes tool_result blocks inside a user message. Surface them as tool.result events
		// so the runner persists them and the UI can pair them with the originating tool.call.
		var events []model.AgentEvent
		if msg, ok := raw["message"].(map[string]interface{}); ok {
			if contentBlocks, ok := msg["content"].([]interface{}); ok {
				for _, block := range contentBlocks {
					b, ok := block.(map[string]interface{})
					if !ok {
						continue
					}
					if b["type"] == "tool_result" {
						events = append(events, model.AgentEvent{
							Type: model.AgentEventToolResult,
							Payload: map[string]interface{}{
								"tool_use_id": b["tool_use_id"],
								"is_error":    b["is_error"],
								"result": map[string]interface{}{
									"content":     b["content"],
									"is_error":    b["is_error"],
									"tool_use_id": b["tool_use_id"],
								},
							},
						})
					}
				}
			}
		}
		if len(events) > 0 {
			return events
		}
		// Non-tool-result user echo (rare) — record without leaking a "status" field.
		return []model.AgentEvent{{
			Type:    model.AgentEventRunStatus,
			Payload: map[string]interface{}{"subtype_kind": "user_echo", "raw": raw},
		}}

	case "result":
		// Terminal event for a turn. The final text was already streamed via assistant
		// content blocks as MessageDelta, so we do NOT re-emit it as MessageCompleted —
		// doing so would duplicate the message in the UI. Only signal turn completion.
		return []model.AgentEvent{{
			Type: model.AgentEventTurnCompleted,
			Payload: map[string]interface{}{
				"session_id":  raw["session_id"],
				"cost_usd":    raw["cost_usd"],
				"duration_ms": raw["duration_ms"],
				"is_error":    raw["is_error"],
			},
		}}

	default:
		// Unknown event — wrap raw under a namespaced key so nested fields (e.g. status on
		// subagent task_notification) don't leak into the runner's break condition.
		return []model.AgentEvent{{
			Type:    model.AgentEventRunStatus,
			Payload: map[string]interface{}{"subtype_kind": eventType, "raw": raw},
		}}
	}
}

func mergeMap(base, extra map[string]string) map[string]string {
	if base == nil {
		base = make(map[string]string)
	}
	for k, v := range extra {
		base[k] = v
	}
	return base
}
