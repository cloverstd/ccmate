package runner

import "strings"

// ErrorClassification determines if an error is retryable.
type ErrorClassification int

const (
	ErrorRetryable    ErrorClassification = iota // Transient, can retry
	ErrorNonRetryable                            // Permanent, don't retry
)

// ClassifyError determines whether an error is retryable based on its content.
func ClassifyError(err error) ErrorClassification {
	if err == nil {
		return ErrorNonRetryable
	}

	msg := err.Error()

	// Non-retryable errors (permanent failures)
	nonRetryablePatterns := []string{
		"invalid webhook signature",
		"user not authorized",
		"template not found",
		"project configuration incomplete",
		"permission denied",
		"no such file or directory",
		"invalid bootstrap token",
		"admin already registered",
		"unknown command",
		"task has no project",
	}

	for _, pattern := range nonRetryablePatterns {
		if strings.Contains(strings.ToLower(msg), strings.ToLower(pattern)) {
			return ErrorNonRetryable
		}
	}

	// Retryable errors (transient failures)
	retryablePatterns := []string{
		"timeout",
		"connection refused",
		"connection reset",
		"temporary failure",
		"rate limit",
		"502", "503", "504",
		"i/o timeout",
		"EOF",
		"TLS handshake",
		"dns lookup",
	}

	for _, pattern := range retryablePatterns {
		if strings.Contains(strings.ToLower(msg), strings.ToLower(pattern)) {
			return ErrorRetryable
		}
	}

	// Default: retryable (optimistic)
	return ErrorRetryable
}
