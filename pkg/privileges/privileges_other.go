//go:build !windows

package privileges

import "errors"

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
	return errors.New("elevação não suportada nesta plataforma")
}

func RelaunchAsAdminWithIPC(rawArgs []string) error {
	return errors.New("elevação não suportada nesta plataforma")
}

func MonitorParentProcess(parentPID int) {
	// No-op on non-Windows
}

