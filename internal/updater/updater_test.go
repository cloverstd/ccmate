package updater

import "testing"

func TestIsNewer(t *testing.T) {
	tests := []struct {
		name      string
		current   string
		candidate string
		want      bool
	}{
		{"dev vs tag", "dev", "v1.2.3", true},
		{"empty vs tag", "", "v1.2.3", true},
		{"tag vs dev", "v1.2.3", "dev", false},
		{"identical", "v1.2.3", "v1.2.3", false},
		{"identical without v", "1.2.3", "v1.2.3", false},
		{"patch bump", "v1.2.3", "v1.2.4", true},
		{"minor bump", "v1.2.9", "v1.3.0", true},
		{"major bump", "v1.9.9", "v2.0.0", true},
		{"older", "v1.2.4", "v1.2.3", false},
		{"prerelease suffix ignored", "v1.2.3", "v1.2.3-rc1", false},
		{"newer over prerelease", "v1.2.3-rc1", "v1.2.4", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsNewer(tc.current, tc.candidate)
			if got != tc.want {
				t.Errorf("IsNewer(%q, %q) = %v, want %v", tc.current, tc.candidate, got, tc.want)
			}
		})
	}
}

func TestPlatformAsset(t *testing.T) {
	if got := PlatformAsset(); got == "" {
		t.Fatal("PlatformAsset returned empty string")
	}
}
