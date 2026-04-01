package runner

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
)

// SecurityConfig holds process-level security settings.
type SecurityConfig struct {
	RunnerUser string // System user for subprocess (e.g., "ccmate-runner")
}

// ApplyProcessLimits sets resource limits and isolation on the agent subprocess.
func ApplyProcessLimits(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Create new process group for clean termination
	}

	// Try to run as ccmate-runner user if it exists
	if runnerUser, err := user.Lookup("ccmate-runner"); err == nil {
		uid, _ := strconv.Atoi(runnerUser.Uid)
		gid, _ := strconv.Atoi(runnerUser.Gid)
		cmd.SysProcAttr.Credential = &syscall.Credential{
			Uid: uint32(uid),
			Gid: uint32(gid),
		}
		slog.Debug("running agent as ccmate-runner user", "uid", uid, "gid", gid)
	}

	// Minimal environment - don't inherit parent env
	cmd.Env = []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=" + os.TempDir(),
		"LANG=en_US.UTF-8",
	}
}

// KillProcessGroup sends SIGKILL to the entire process group.
func KillProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		return fmt.Errorf("getting pgid: %w", err)
	}
	// Kill entire process group (negative PID)
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
		return fmt.Errorf("killing process group: %w", err)
	}
	slog.Info("killed process group", "pgid", pgid)
	return nil
}

// ApplyNetworkRestrictions uses iptables to restrict network access for a user.
// This requires root privileges and the ccmate-runner user to exist.
func ApplyNetworkRestrictions(runnerUser string) error {
	if runnerUser == "" {
		return nil
	}

	// Block metadata service (169.254.169.254 - AWS/GCP/Azure)
	rules := [][]string{
		{"-A", "OUTPUT", "-m", "owner", "--uid-owner", runnerUser, "-d", "169.254.169.254", "-j", "DROP"},
		// Block common internal networks
		{"-A", "OUTPUT", "-m", "owner", "--uid-owner", runnerUser, "-d", "10.0.0.0/8", "-j", "DROP"},
		{"-A", "OUTPUT", "-m", "owner", "--uid-owner", runnerUser, "-d", "172.16.0.0/12", "-j", "DROP"},
		{"-A", "OUTPUT", "-m", "owner", "--uid-owner", runnerUser, "-d", "192.168.0.0/16", "-j", "DROP"},
		// Block localhost (except needed services)
		{"-A", "OUTPUT", "-m", "owner", "--uid-owner", runnerUser, "-d", "127.0.0.0/8", "-j", "DROP"},
	}

	for _, rule := range rules {
		cmd := exec.Command("iptables", rule...)
		if err := cmd.Run(); err != nil {
			slog.Warn("failed to apply iptables rule", "rule", rule, "error", err)
			// Don't fail hard - iptables may not be available
		}
	}

	return nil
}

// CleanupNetworkRestrictions removes iptables rules for the runner user.
func CleanupNetworkRestrictions(runnerUser string) {
	if runnerUser == "" {
		return
	}

	rules := [][]string{
		{"-D", "OUTPUT", "-m", "owner", "--uid-owner", runnerUser, "-d", "169.254.169.254", "-j", "DROP"},
		{"-D", "OUTPUT", "-m", "owner", "--uid-owner", runnerUser, "-d", "10.0.0.0/8", "-j", "DROP"},
		{"-D", "OUTPUT", "-m", "owner", "--uid-owner", runnerUser, "-d", "172.16.0.0/12", "-j", "DROP"},
		{"-D", "OUTPUT", "-m", "owner", "--uid-owner", runnerUser, "-d", "192.168.0.0/16", "-j", "DROP"},
		{"-D", "OUTPUT", "-m", "owner", "--uid-owner", runnerUser, "-d", "127.0.0.0/8", "-j", "DROP"},
	}

	for _, rule := range rules {
		cmd := exec.Command("iptables", rule...)
		_ = cmd.Run()
	}
}
