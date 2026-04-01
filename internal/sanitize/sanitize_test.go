package sanitize

import (
	"strings"
	"testing"
)

func TestSanitizeText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "GitHub token",
			input: "Using token ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij",
			want:  "Using token [REDACTED]",
		},
		{
			name:  "Authorization header",
			input: "Authorization: Bearer sk-ant-abc123def456ghi789jkl012mno345pqr678",
			want:  "[REDACTED]",
		},
		{
			name:  "Anthropic API key",
			input: "key: sk-ant-abcdefghijklmnopqrstuvwxyz012345678",
			want:  "[REDACTED]",
		},
		{
			name:  "normal text unchanged",
			input: "This is a normal log message with no secrets",
			want:  "This is a normal log message with no secrets",
		},
		{
			name:  "SSH private key",
			input: "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQ...\n-----END RSA PRIVATE KEY-----",
			want:  "[REDACTED]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeText(tt.input)
			if result != tt.want {
				t.Errorf("SanitizeText() = %q, want %q", result, tt.want)
			}
		})
	}
}

func TestSanitizeForWeb(t *testing.T) {
	input := `<script>alert("xss")</script>`
	result := SanitizeForWeb(input)

	if strings.Contains(result, "<script>") {
		t.Errorf("SanitizeForWeb did not escape HTML tags: %s", result)
	}
	if !strings.Contains(result, "&lt;script&gt;") {
		t.Errorf("SanitizeForWeb did not properly escape: %s", result)
	}
}

func TestSanitizeMap(t *testing.T) {
	input := map[string]interface{}{
		"content": "Token: ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij",
		"count":   42,
		"nested": map[string]interface{}{
			"secret": "Authorization: Bearer abc123def456ghi789jkl012mno345pqr",
		},
	}

	result := SanitizeMap(input)

	if content, ok := result["content"].(string); ok {
		if strings.Contains(content, "ghp_") {
			t.Error("GitHub token not redacted in content")
		}
	}

	if result["count"] != 42 {
		t.Error("non-string values should be preserved")
	}

	if nested, ok := result["nested"].(map[string]interface{}); ok {
		if secret, ok := nested["secret"].(string); ok {
			if strings.Contains(secret, "Bearer") {
				t.Error("Authorization header not redacted in nested map")
			}
		}
	}
}
