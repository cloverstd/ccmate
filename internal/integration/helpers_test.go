package integration

import (
	"github.com/cloverstd/ccmate/internal/webhook"
)

// setupTestProcessor creates a webhook processor for testing (no git provider).
func setupTestProcessor(env *testEnv) *webhook.Processor {
	return webhook.NewProcessor(env.client, nil)
}
