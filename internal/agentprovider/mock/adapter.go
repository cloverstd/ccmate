package mock

import (
	"context"
	"fmt"
	"time"

	"github.com/cloverstd/ccmate/internal/agentprovider"
	"github.com/cloverstd/ccmate/internal/model"
)

// Factory creates mock agent adapters for testing.
type Factory struct{}

func (f *Factory) Name() string { return "mock" }

func (f *Factory) Create(cfg agentprovider.AgentConfig) (agentprovider.AgentAdapter, error) {
	return &Adapter{debug: cfg.Extra["debug"] == "true"}, nil
}

// Adapter is a mock agent that simulates development work.
type Adapter struct {
	running bool
	debug   bool
}

func (a *Adapter) StartSession(ctx context.Context, req agentprovider.StartSessionRequest) (*agentprovider.SessionHandle, error) {
	a.running = true
	handle := &agentprovider.SessionHandle{
		ID:       "mock-session-1",
		Provider: "mock",
	}
	if a.debug {
		handle.DebugInfo = map[string]string{
			"provider":          "mock (NOT calling real agent)",
			"note":              "Configure agent_providers in Settings with name=claude-code to use real Claude",
			"workdir":           req.WorkDir,
			"system_prompt_len": fmt.Sprintf("%d", len(req.SystemPrompt)),
			"task_prompt_len":   fmt.Sprintf("%d", len(req.TaskPrompt)),
		}
	}
	return handle, nil
}

func (a *Adapter) SendInput(ctx context.Context, handle *agentprovider.SessionHandle, input agentprovider.UserInput) error {
	return nil
}

func (a *Adapter) StreamEvents(ctx context.Context, handle *agentprovider.SessionHandle) (<-chan model.AgentEvent, error) {
	ch := make(chan model.AgentEvent, 10)

	go func() {
		defer close(ch)

		// Simulate agent work
		events := []model.AgentEvent{
			{Type: model.AgentEventRunStatus, Payload: map[string]interface{}{"status": "started"}},
			{Type: model.AgentEventMessageDelta, Payload: map[string]interface{}{"content": "Analyzing the issue..."}},
			{Type: model.AgentEventToolCall, Payload: map[string]interface{}{"tool": "read_file", "args": "main.go"}},
			{Type: model.AgentEventToolResult, Payload: map[string]interface{}{"result": "file contents..."}},
			{Type: model.AgentEventMessageDelta, Payload: map[string]interface{}{"content": "Making changes..."}},
			{Type: model.AgentEventMessageCompleted, Payload: map[string]interface{}{"content": "I've implemented the requested changes."}},
			{Type: model.AgentEventRunStatus, Payload: map[string]interface{}{"status": "completed"}},
		}

		for _, event := range events {
			select {
			case <-ctx.Done():
				return
			case ch <- event:
				time.Sleep(500 * time.Millisecond)
			}
		}
	}()

	return ch, nil
}

func (a *Adapter) Interrupt(ctx context.Context, handle *agentprovider.SessionHandle) error {
	a.running = false
	return nil
}

func (a *Adapter) Resume(ctx context.Context, handle *agentprovider.SessionHandle) error {
	a.running = true
	return nil
}

func (a *Adapter) Close(ctx context.Context, handle *agentprovider.SessionHandle) error {
	a.running = false
	return nil
}

func (a *Adapter) Capabilities() model.AgentCapabilities {
	return model.AgentCapabilities{
		SupportsImage:     false,
		SupportsResume:    false,
		SupportsStreaming: true,
	}
}
