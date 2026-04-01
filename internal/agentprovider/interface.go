package agentprovider

import (
	"context"
	"fmt"

	"github.com/cloverstd/ccmate/internal/model"
)

// SessionHandle is an opaque reference to a running agent session.
type SessionHandle struct {
	ID       string
	Provider string
}

// StartSessionRequest contains parameters for starting an agent session.
type StartSessionRequest struct {
	WorkDir      string
	SystemPrompt string
	TaskPrompt   string
	Model        string
}

// UserInput represents input sent to an agent.
type UserInput struct {
	Text     string
	ImageURL string
}

// AgentAdapter defines the unified interface for all agent providers.
type AgentAdapter interface {
	StartSession(ctx context.Context, req StartSessionRequest) (*SessionHandle, error)
	SendInput(ctx context.Context, handle *SessionHandle, input UserInput) error
	StreamEvents(ctx context.Context, handle *SessionHandle) (<-chan model.AgentEvent, error)
	Interrupt(ctx context.Context, handle *SessionHandle) error
	Resume(ctx context.Context, handle *SessionHandle) error
	Close(ctx context.Context, handle *SessionHandle) error
	Capabilities() model.AgentCapabilities
}

// AgentFactory creates AgentAdapter instances.
type AgentFactory interface {
	Name() string
	Create(cfg AgentConfig) (AgentAdapter, error)
}

// AgentConfig holds configuration for an agent provider.
type AgentConfig struct {
	Binary string
	Model  string
	Extra  map[string]string
}

// Registry holds registered agent factories.
type Registry struct {
	factories map[string]AgentFactory
}

func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]AgentFactory)}
}

func (r *Registry) Register(factory AgentFactory) {
	r.factories[factory.Name()] = factory
}

func (r *Registry) Create(name string, cfg AgentConfig) (AgentAdapter, error) {
	factory, ok := r.factories[name]
	if !ok {
		return nil, fmt.Errorf("unknown agent provider: %s", name)
	}
	return factory.Create(cfg)
}

func (r *Registry) Get(name string) (AgentFactory, bool) {
	f, ok := r.factories[name]
	return f, ok
}
