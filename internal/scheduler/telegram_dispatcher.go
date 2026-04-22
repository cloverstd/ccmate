package scheduler

import (
	"context"
	"fmt"

	"github.com/cloverstd/ccmate/internal/notify/telegram"
)

// TelegramDispatcher applies Telegram inline-button actions to the scheduler.
type TelegramDispatcher struct {
	sched *Scheduler
}

func NewTelegramDispatcher(s *Scheduler) *TelegramDispatcher {
	return &TelegramDispatcher{sched: s}
}

func (d *TelegramDispatcher) HandleCallback(ctx context.Context, action telegram.CallbackAction) (string, error) {
	switch action.Action {
	case "pause":
		if err := d.sched.PauseTask(ctx, action.TaskID); err != nil {
			return "", err
		}
		return "paused", nil
	case "resume":
		if err := d.sched.ResumeTask(ctx, action.TaskID); err != nil {
			return "", err
		}
		return "resumed", nil
	case "cancel":
		if err := d.sched.CancelTask(ctx, action.TaskID); err != nil {
			return "", err
		}
		return "cancelled", nil
	case "retry":
		if err := d.sched.RetryTask(ctx, action.TaskID); err != nil {
			return "", err
		}
		return "queued for retry", nil
	default:
		return "", fmt.Errorf("unknown action: %s", action.Action)
	}
}
