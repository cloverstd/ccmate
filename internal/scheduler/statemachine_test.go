package scheduler

import (
	"testing"

	"github.com/cloverstd/ccmate/internal/model"
)

func TestIsValidTransition(t *testing.T) {
	tests := []struct {
		from  model.TaskStatus
		to    model.TaskStatus
		valid bool
	}{
		{model.TaskStatusQueued, model.TaskStatusRunning, true},
		{model.TaskStatusRunning, model.TaskStatusPaused, true},
		{model.TaskStatusRunning, model.TaskStatusWaitingUser, true},
		{model.TaskStatusRunning, model.TaskStatusSucceeded, true},
		{model.TaskStatusRunning, model.TaskStatusFailed, true},
		{model.TaskStatusRunning, model.TaskStatusCancelled, true},
		{model.TaskStatusPaused, model.TaskStatusQueued, true},
		{model.TaskStatusWaitingUser, model.TaskStatusRunning, true},
		{model.TaskStatusFailed, model.TaskStatusQueued, true},

		// Invalid transitions
		{model.TaskStatusQueued, model.TaskStatusSucceeded, false},
		{model.TaskStatusSucceeded, model.TaskStatusRunning, false},
		{model.TaskStatusFailed, model.TaskStatusRunning, false},
		{model.TaskStatusPaused, model.TaskStatusSucceeded, false},
		{model.TaskStatusCancelled, model.TaskStatusQueued, false},
	}

	for _, tt := range tests {
		name := string(tt.from) + " -> " + string(tt.to)
		t.Run(name, func(t *testing.T) {
			result := IsValidTransition(tt.from, tt.to)
			if result != tt.valid {
				t.Errorf("IsValidTransition(%s, %s) = %v, want %v", tt.from, tt.to, result, tt.valid)
			}
		})
	}
}

func TestTaskStatusIsActive(t *testing.T) {
	activeStatuses := []model.TaskStatus{
		model.TaskStatusQueued,
		model.TaskStatusRunning,
		model.TaskStatusPaused,
		model.TaskStatusWaitingUser,
	}
	for _, s := range activeStatuses {
		if !s.IsActive() {
			t.Errorf("expected %s to be active", s)
		}
	}

	inactiveStatuses := []model.TaskStatus{
		model.TaskStatusPending,
		model.TaskStatusSucceeded,
		model.TaskStatusFailed,
		model.TaskStatusCancelled,
	}
	for _, s := range inactiveStatuses {
		if s.IsActive() {
			t.Errorf("expected %s to be inactive", s)
		}
	}
}
