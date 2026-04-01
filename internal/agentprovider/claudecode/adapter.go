package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
	return &Adapter{
		binary: binary,
		model:  cfg.Model,
	}, nil
}

// Adapter implements AgentAdapter for Claude Code CLI.
type Adapter struct {
	binary string
	model  string

	mu      sync.Mutex
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	running bool
}

func (a *Adapter) StartSession(ctx context.Context, req agentprovider.StartSessionRequest) (*agentprovider.SessionHandle, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	sessionCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	args := []string{
		"--print",
		"--output-format", "stream-json",
	}

	if a.model != "" {
		args = append(args, "--model", a.model)
	}

	if req.SystemPrompt != "" {
		args = append(args, "--system-prompt", req.SystemPrompt)
	}

	args = append(args, req.TaskPrompt)

	a.cmd = exec.CommandContext(sessionCtx, a.binary, args...)
	a.cmd.Dir = req.WorkDir
	a.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	a.cmd.Env = []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=" + os.TempDir(),
		"LANG=en_US.UTF-8",
	}
	a.running = true

	return &agentprovider.SessionHandle{
		ID:       fmt.Sprintf("claude-%p", a.cmd),
		Provider: "claude-code",
	}, nil
}

func (a *Adapter) SendInput(ctx context.Context, handle *agentprovider.SessionHandle, input agentprovider.UserInput) error {
	// Claude Code CLI in --print mode doesn't support interactive input
	// For follow-up interactions, start a new session with context
	return fmt.Errorf("claude code --print mode does not support interactive input; use --resume or start new session")
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

	if err := a.cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting claude: %w", err)
	}

	ch := make(chan model.AgentEvent, 64)

	go func() {
		defer close(ch)

		// Read stderr in background
		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				ch <- model.AgentEvent{
					Type: model.AgentEventError,
					Payload: map[string]interface{}{
						"message": scanner.Text(),
					},
				}
			}
		}()

		// Parse JSON stream from stdout
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}

			var raw map[string]interface{}
			if err := json.Unmarshal([]byte(line), &raw); err != nil {
				continue
			}

			event := translateClaudeEvent(raw)
			select {
			case ch <- event:
			case <-ctx.Done():
				return
			}
		}

		// Wait for process to finish
		if err := a.cmd.Wait(); err != nil {
			ch <- model.AgentEvent{
				Type: model.AgentEventError,
				Payload: map[string]interface{}{
					"message": fmt.Sprintf("claude exited with error: %v", err),
				},
			}
		}

		ch <- model.AgentEvent{
			Type:    model.AgentEventRunStatus,
			Payload: map[string]interface{}{"status": "completed"},
		}
	}()

	return ch, nil
}

func (a *Adapter) Interrupt(ctx context.Context, handle *agentprovider.SessionHandle) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.cancel != nil {
		a.cancel()
	}
	a.running = false
	return nil
}

func (a *Adapter) Resume(ctx context.Context, handle *agentprovider.SessionHandle) error {
	return fmt.Errorf("claude code resume requires starting a new session with --resume flag")
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

// translateClaudeEvent converts Claude Code JSON stream output to unified AgentEvent.
func translateClaudeEvent(raw map[string]interface{}) model.AgentEvent {
	eventType, _ := raw["type"].(string)

	switch eventType {
	case "assistant":
		content := ""
		if msg, ok := raw["message"].(map[string]interface{}); ok {
			if contentBlocks, ok := msg["content"].([]interface{}); ok {
				for _, block := range contentBlocks {
					if b, ok := block.(map[string]interface{}); ok {
						if text, ok := b["text"].(string); ok {
							content += text
						}
					}
				}
			}
		}
		return model.AgentEvent{
			Type:    model.AgentEventMessageDelta,
			Payload: map[string]interface{}{"content": content},
		}

	case "tool_use":
		return model.AgentEvent{
			Type: model.AgentEventToolCall,
			Payload: map[string]interface{}{
				"tool":  raw["name"],
				"input": raw["input"],
			},
		}

	case "tool_result":
		return model.AgentEvent{
			Type: model.AgentEventToolResult,
			Payload: map[string]interface{}{
				"result": raw["content"],
			},
		}

	case "result":
		return model.AgentEvent{
			Type: model.AgentEventMessageCompleted,
			Payload: map[string]interface{}{
				"content": raw["result"],
			},
		}

	default:
		return model.AgentEvent{
			Type:    model.AgentEventRunStatus,
			Payload: raw,
		}
	}
}
