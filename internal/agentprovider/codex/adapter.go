package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	"github.com/cloverstd/ccmate/internal/agentprovider"
	"github.com/cloverstd/ccmate/internal/model"
)

// Factory creates Codex agent adapters.
type Factory struct{}

func (f *Factory) Name() string { return "codex" }

func (f *Factory) Create(cfg agentprovider.AgentConfig) (agentprovider.AgentAdapter, error) {
	binary := cfg.Binary
	if binary == "" {
		binary = "codex"
	}
	return &Adapter{
		binary: binary,
		model:  cfg.Model,
		debug:  cfg.Extra["debug"] == "true",
	}, nil
}

// Adapter implements AgentAdapter for Codex CLI `exec --json`.
type Adapter struct {
	binary string
	model  string
	debug  bool

	mu     sync.Mutex
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr io.ReadCloser
}

func (a *Adapter) StartSession(ctx context.Context, req agentprovider.StartSessionRequest) (*agentprovider.SessionHandle, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	prompt := strings.TrimSpace(req.SystemPrompt + "\n\n" + req.TaskPrompt)
	args := []string{
		"exec",
		"--json",
		"--skip-git-repo-check",
		"--dangerously-bypass-approvals-and-sandbox",
		"-C", req.WorkDir,
	}
	if a.model != "" {
		args = append(args, "-m", a.model)
	}
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, a.binary, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("getting stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("getting stderr pipe: %w", err)
	}

	a.cmd = cmd
	a.stdout = stdout
	a.stderr = stderr

	handle := &agentprovider.SessionHandle{
		ID:       fmt.Sprintf("codex-%p", cmd),
		Provider: "codex",
	}
	if a.debug {
		handle.DebugInfo = map[string]string{
			"binary":            a.binary,
			"model":             a.model,
			"workdir":           req.WorkDir,
			"system_prompt_len": fmt.Sprintf("%d", len(req.SystemPrompt)),
			"task_prompt_len":   fmt.Sprintf("%d", len(req.TaskPrompt)),
		}
	}
	return handle, nil
}

func (a *Adapter) SendInput(ctx context.Context, handle *agentprovider.SessionHandle, input agentprovider.UserInput) error {
	return fmt.Errorf("codex exec does not support interactive follow-up input")
}

func (a *Adapter) StreamEvents(ctx context.Context, handle *agentprovider.SessionHandle) (<-chan model.AgentEvent, error) {
	a.mu.Lock()
	cmd := a.cmd
	stdout := a.stdout
	stderr := a.stderr
	a.mu.Unlock()

	if cmd == nil || stdout == nil || stderr == nil {
		return nil, fmt.Errorf("no active session")
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting codex: %w", err)
	}

	ch := make(chan model.AgentEvent, 64)
	go func() {
		defer close(ch)

		go func() {
			scanner := bufio.NewScanner(stderr)
			scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" {
					continue
				}
				ch <- model.AgentEvent{
					Type: model.AgentEventError,
					Payload: map[string]interface{}{
						"message": line,
					},
				}
			}
		}()

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var raw map[string]interface{}
			if err := json.Unmarshal([]byte(line), &raw); err != nil {
				ch <- model.AgentEvent{
					Type: model.AgentEventError,
					Payload: map[string]interface{}{
						"message": fmt.Sprintf("invalid codex json: %s", line),
					},
				}
				continue
			}
			a.emitEvent(ch, raw)
		}

		if err := cmd.Wait(); err != nil {
			ch <- model.AgentEvent{
				Type: model.AgentEventError,
				Payload: map[string]interface{}{
					"message": err.Error(),
				},
			}
		}
	}()

	return ch, nil
}

func (a *Adapter) emitEvent(ch chan<- model.AgentEvent, raw map[string]interface{}) {
	eventType, _ := raw["type"].(string)
	switch eventType {
	case "thread.started":
		if threadID, _ := raw["thread_id"].(string); threadID != "" {
			ch <- model.AgentEvent{
				Type: model.AgentEventRunStatus,
				Payload: map[string]interface{}{
					"status":     "started",
					"session_id": threadID,
				},
			}
		}
	case "turn.started":
		ch <- model.AgentEvent{Type: model.AgentEventRunStatus, Payload: map[string]interface{}{"status": "running"}}
	case "turn.completed":
		ch <- model.AgentEvent{Type: model.AgentEventRunStatus, Payload: map[string]interface{}{"status": "completed"}}
	case "item.started":
		if item, ok := raw["item"].(map[string]interface{}); ok {
			a.emitItemStarted(ch, item)
		}
	case "item.completed":
		if item, ok := raw["item"].(map[string]interface{}); ok {
			a.emitItemCompleted(ch, item)
		}
	}
}

func (a *Adapter) emitItemStarted(ch chan<- model.AgentEvent, item map[string]interface{}) {
	itemType, _ := item["type"].(string)
	if itemType == "command_execution" {
		ch <- model.AgentEvent{
			Type: model.AgentEventToolCall,
			Payload: map[string]interface{}{
				"text": "Codex command execution",
				"tools": []map[string]interface{}{
					{
						"name":  "Bash",
						"input": map[string]interface{}{"command": item["command"]},
					},
				},
			},
		}
	}
}

func (a *Adapter) emitItemCompleted(ch chan<- model.AgentEvent, item map[string]interface{}) {
	itemType, _ := item["type"].(string)
	switch itemType {
	case "agent_message":
		text, _ := item["text"].(string)
		ch <- model.AgentEvent{
			Type:    model.AgentEventMessageCompleted,
			Payload: map[string]interface{}{"content": text},
		}
	case "command_execution":
		ch <- model.AgentEvent{
			Type: model.AgentEventToolResult,
			Payload: map[string]interface{}{
				"result": map[string]interface{}{
					"command":   item["command"],
					"output":    item["aggregated_output"],
					"status":    item["status"],
					"exit_code": item["exit_code"],
				},
			},
		}
	}
}

func (a *Adapter) Interrupt(ctx context.Context, handle *agentprovider.SessionHandle) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cmd != nil && a.cmd.Process != nil {
		return a.cmd.Process.Kill()
	}
	return nil
}

func (a *Adapter) Resume(ctx context.Context, handle *agentprovider.SessionHandle) error {
	return fmt.Errorf("codex exec does not support resume")
}

func (a *Adapter) Close(ctx context.Context, handle *agentprovider.SessionHandle) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cmd != nil && a.cmd.Process != nil {
		_ = a.cmd.Process.Kill()
	}
	return nil
}

func (a *Adapter) Capabilities() model.AgentCapabilities {
	return model.AgentCapabilities{
		SupportsImage:     false,
		SupportsResume:    false,
		SupportsStreaming: true,
	}
}
