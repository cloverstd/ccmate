package sanitize

import (
	"regexp"
	"strings"
)

var sensitivePatterns = []*regexp.Regexp{
	// Generic tokens and keys
	regexp.MustCompile(`(?i)(token|key|secret|password|passwd|credential|auth)[\s]*[=:]\s*['"]?[A-Za-z0-9+/=_\-]{16,}['"]?`),
	// Authorization headers
	regexp.MustCompile(`(?i)Authorization:\s*(?:Bearer|Basic|Token)\s+[A-Za-z0-9+/=_\-\.]+`),
	// GitHub tokens
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]{36,}`),
	// Generic API keys
	regexp.MustCompile(`(?i)api[_-]?key[\s]*[=:]\s*['"]?[A-Za-z0-9+/=_\-]{16,}['"]?`),
	// SSH private keys
	regexp.MustCompile(`-----BEGIN [A-Z ]+ PRIVATE KEY-----[\s\S]*?-----END [A-Z ]+ PRIVATE KEY-----`),
	// AWS keys
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	// Anthropic API keys
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{32,}`),
	// OpenAI API keys
	regexp.MustCompile(`sk-[A-Za-z0-9\-]{32,}`),
}

const redactedText = "[REDACTED]"

// SanitizeText replaces sensitive patterns in text with [REDACTED].
func SanitizeText(text string) string {
	result := text
	for _, pattern := range sensitivePatterns {
		result = pattern.ReplaceAllString(result, redactedText)
	}
	return result
}

// SanitizeMap sanitizes all string values in a map.
func SanitizeMap(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case string:
			result[k] = SanitizeText(val)
		case map[string]interface{}:
			result[k] = SanitizeMap(val)
		default:
			result[k] = v
		}
	}
	return result
}

// StripHTML removes HTML tags from text to prevent XSS.
func StripHTML(text string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	return re.ReplaceAllString(text, "")
}

// SanitizeForWeb sanitizes text for safe web rendering.
func SanitizeForWeb(text string) string {
	text = SanitizeText(text)
	// Escape HTML entities
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	text = strings.ReplaceAll(text, "\"", "&quot;")
	text = strings.ReplaceAll(text, "'", "&#39;")
	return text
}
