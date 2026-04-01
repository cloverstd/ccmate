package webhook

import (
	"testing"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		input   string
		wantCmd string
		wantErr bool
	}{
		{"/ccmate run", "run", false},
		{"/ccmate pause", "pause", false},
		{"/ccmate resume", "resume", false},
		{"/ccmate retry", "retry", false},
		{"/ccmate status", "status", false},
		{"/ccmate fix-review", "fix-review", false},
		{"  /ccmate run  ", "run", false},

		// Invalid commands
		{"/ccmate", "", true},
		{"/ccmate unknown", "", true},
		{"not a command", "", true},
		{"", "", true},
		{"just some /ccmate in the middle", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			cmd, err := ParseCommand(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseCommand(%q) expected error, got cmd=%v", tt.input, cmd)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseCommand(%q) unexpected error: %v", tt.input, err)
				return
			}
			if cmd.Name != tt.wantCmd {
				t.Errorf("ParseCommand(%q).Name = %q, want %q", tt.input, cmd.Name, tt.wantCmd)
			}
		})
	}
}
