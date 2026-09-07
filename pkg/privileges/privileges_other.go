//go:build !windows

package privileges

import (
	"errors"
	"time"
)

// errUnsupported is returned by every elevation entry point: outside Windows
// there is no UAC and no process token to adjust.
var errUnsupported = errors.New("elevação não suportada nesta plataforma")

type PrivilegeStatus struct {
	IsAdmin         bool            `json:"isAdmin"`
	IsElevated      bool            `json:"isElevated"`
	ActiveUser      string          `json:"activeUser"`
	EnabledTokens   map[string]bool `json:"enabledTokens"`
	HasBackupAccess bool            `json:"hasBackupAccess"`
	CanElevate      bool            `json:"canElevate"`
}

func CheckPrivilegeStatus() PrivilegeStatus {
	return PrivilegeStatus{
		IsAdmin:         true,
		IsElevated:      true,
		ActiveUser:      "user",
		EnabledTokens:   make(map[string]bool),
		HasBackupAccess: true,
		CanElevate:      false,
	}
}

func EnableAllBackupPrivileges() (map[string]bool, error) {
	return make(map[string]bool), nil
}

func RelaunchAsAdmin() error {
	return errUnsupported
}

func RelaunchAsAdminWithIPC(rawArgs []string) error {
	return errUnsupported
}

// LaunchElevatedHandoff cannot elevate here, so it reports the failure instead
// of pretending a child was started.
func LaunchElevatedHandoff(args []string) (uint32, error) {
	return 0, errUnsupported
}

// WaitForProcessExit is not implemented outside Windows.
func WaitForProcessExit(pid uint32, timeout time.Duration) error {
	return errUnsupported
}

func MonitorParentProcess(parentPID int) {
	// No-op on non-Windows: there is no elevated child to keep in step.
}
