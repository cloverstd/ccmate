package runner

import (
	"errors"
	"testing"
)

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected ErrorClassification
	}{
		// Retryable
		{"timeout", errors.New("context deadline exceeded: timeout"), ErrorRetryable},
		{"connection refused", errors.New("dial tcp: connection refused"), ErrorRetryable},
		{"rate limit", errors.New("API rate limit exceeded"), ErrorRetryable},
		{"503", errors.New("HTTP 503 Service Unavailable"), ErrorRetryable},
		{"EOF", errors.New("unexpected EOF"), ErrorRetryable},
		{"TLS", errors.New("TLS handshake timeout"), ErrorRetryable},

		// Non-retryable
		{"invalid signature", errors.New("invalid webhook signature"), ErrorNonRetryable},
		{"not authorized", errors.New("user not authorized"), ErrorNonRetryable},
		{"permission denied", errors.New("permission denied"), ErrorNonRetryable},
		{"no project", errors.New("task has no project"), ErrorNonRetryable},
		{"unknown command", errors.New("unknown command: deploy"), ErrorNonRetryable},

		// Default (retryable)
		{"generic error", errors.New("something went wrong"), ErrorRetryable},
		{"nil error", nil, ErrorNonRetryable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyError(tt.err)
			if result != tt.expected {
				t.Errorf("ClassifyError(%v) = %d, want %d", tt.err, result, tt.expected)
			}
		})
	}
}
