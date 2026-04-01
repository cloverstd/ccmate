package sse

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
)

// Event represents a server-sent event.
type Event struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// Broker manages SSE client subscriptions per topic (e.g., task ID).
type Broker struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan Event]struct{}
}

func NewBroker() *Broker {
	return &Broker{
		subscribers: make(map[string]map[chan Event]struct{}),
	}
}

// Subscribe registers a new client channel for the given topic.
func (b *Broker) Subscribe(topic string) chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan Event, 64)
	if b.subscribers[topic] == nil {
		b.subscribers[topic] = make(map[chan Event]struct{})
	}
	b.subscribers[topic][ch] = struct{}{}
	return ch
}

// Unsubscribe removes a client channel from the given topic.
func (b *Broker) Unsubscribe(topic string, ch chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if subs, ok := b.subscribers[topic]; ok {
		delete(subs, ch)
		close(ch)
		if len(subs) == 0 {
			delete(b.subscribers, topic)
		}
	}
}

// Publish sends an event to all subscribers of the given topic.
func (b *Broker) Publish(topic string, event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if subs, ok := b.subscribers[topic]; ok {
		for ch := range subs {
			select {
			case ch <- event:
			default:
				slog.Warn("dropping SSE event, subscriber buffer full", "topic", topic)
			}
		}
	}
}

// ServeHTTP handles SSE connections for a given topic.
func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request, topic string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := b.Subscribe(topic)
	defer b.Unsubscribe(topic, ch)

	ctx := r.Context()

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
	flusher.Flush()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(event.Data)
			if err != nil {
				slog.Error("failed to marshal SSE event", "error", err)
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
			flusher.Flush()
		}
	}
}
