//go:build windows

package privileges

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// PrivilegeStatus reports the current elevation and security token status.
type PrivilegeStatus struct {
	IsAdmin         bool            `json:"isAdmin"`
	IsElevated      bool            `json:"isElevated"`
	ActiveUser      string          `json:"activeUser"`
	EnabledTokens   map[string]bool `json:"enabledTokens"`
	HasBackupAccess bool            `json:"hasBackupAccess"`
	CanElevate      bool            `json:"canElevate"`
}

var (
	modadvapi32               = windows.NewLazySystemDLL("advapi32.dll")
	procAdjustTokenPrivileges = modadvapi32.NewProc("AdjustTokenPrivileges")
	procLookupPrivilegeValueW = modadvapi32.NewProc("LookupPrivilegeValueW")
	modshell32                 = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteW          = modshell32.NewProc("ShellExecuteW")
)

// List of high-privilege Windows security tokens to request
var criticalPrivileges = []string{
	"SeBackupPrivilege",              // Bypasses all file/folder read ACLs (NTFS backup intent)
	"SeRestorePrivilege",             // Bypasses all file/folder write ACLs
	"SeSecurityPrivilege",            // Access SACL and security descriptors
	"SeTakeOwnershipPrivilege",       // Take ownership of files if needed
	"SeIncreaseBasePriorityPrivilege", // Allow high priority I/O threads
	"SeProfileSingleProcessPrivilege",
}

// CheckPrivilegeStatus inspects current process token and elevation.
func CheckPrivilegeStatus() PrivilegeStatus {
	isAdmin, isElevated := checkElevation()
	userName := os.Getenv("USERNAME")

	status := PrivilegeStatus{
		IsAdmin:         isAdmin,
		IsElevated:      isElevated,
		ActiveUser:      userName,
		EnabledTokens:   make(map[string]bool),
		HasBackupAccess: false,
		CanElevate:      !isElevated,
	}

	for _, priv := range criticalPrivileges {
		enabled := isPrivilegeEnabled(priv)
		status.EnabledTokens[priv] = enabled
		if priv == "SeBackupPrivilege" && enabled {
			status.HasBackupAccess = true
		}
	}

	return status
}

// EnableAllBackupPrivileges acquires SeBackupPrivilege and other elevated rights on the process token.
func EnableAllBackupPrivileges() (map[string]bool, error) {
	results := make(map[string]bool)

	token, err := openProcessToken(windows.TOKEN_ADJUST_PRIVILEGES | windows.TOKEN_QUERY)
	if err != nil {
		return results, fmt.Errorf("openProcessToken falhou: %w", err)
	}
	defer token.Close()

	for _, privName := range criticalPrivileges {
		err := enableTokenPrivilege(token, privName)
		results[privName] = (err == nil)
	}

	return results, nil
}

func openProcessToken(desiredAccess uint32) (windows.Token, error) {
	var token windows.Token
	currentProcess := windows.CurrentProcess()
	err := windows.OpenProcessToken(currentProcess, desiredAccess, &token)
	if err != nil {
		return 0, err
	}
	return token, nil
}

func enableTokenPrivilege(token windows.Token, privilegeName string) error {
	privNamePtr, err := syscall.UTF16PtrFromString(privilegeName)
	if err != nil {
		return err
	}

	var luid windows.LUID
	r1, _, errLookup := procLookupPrivilegeValueW.Call(
		0,
		uintptr(unsafe.Pointer(privNamePtr)),
		uintptr(unsafe.Pointer(&luid)),
	)
	if r1 == 0 {
		return fmt.Errorf("LookupPrivilegeValue(%s) falhou: %w", privilegeName, errLookup)
	}

	type TOKEN_PRIVILEGES struct {
		PrivilegeCount uint32
		Privileges     [1]windows.LUIDAndAttributes
	}

	var tp TOKEN_PRIVILEGES
	tp.PrivilegeCount = 1
	tp.Privileges[0].Luid = luid
	tp.Privileges[0].Attributes = windows.SE_PRIVILEGE_ENABLED

	r1, _, errAdjust := procAdjustTokenPrivileges.Call(
		uintptr(token),
		0,
		uintptr(unsafe.Pointer(&tp)),
		uintptr(unsafe.Sizeof(tp)),
		0,
		0,
	)
	if r1 == 0 {
		return fmt.Errorf("AdjustTokenPrivileges(%s) falhou: %w", privilegeName, errAdjust)
	}

	return nil
}

func isPrivilegeEnabled(privilegeName string) bool {
	token, err := openProcessToken(windows.TOKEN_QUERY)
	if err != nil {
		return false
	}
	defer token.Close()

	privNamePtr, err := syscall.UTF16PtrFromString(privilegeName)
	if err != nil {
		return false
	}

	var luid windows.LUID
	r1, _, _ := procLookupPrivilegeValueW.Call(
		0,
		uintptr(unsafe.Pointer(privNamePtr)),
		uintptr(unsafe.Pointer(&luid)),
	)
	if r1 == 0 {
		return false
	}

	var tokenPrivs [64]byte
	var retLen uint32
	err = windows.GetTokenInformation(
		token,
		windows.TokenPrivileges,
		&tokenPrivs[0],
		uint32(len(tokenPrivs)),
		&retLen,
	)
	if err != nil && err != windows.ERROR_INSUFFICIENT_BUFFER {
		return false
	}

	buf := tokenPrivs[:]
	if retLen > uint32(len(tokenPrivs)) {
		buf = make([]byte, retLen)
		err = windows.GetTokenInformation(
			token,
			windows.TokenPrivileges,
			&buf[0],
			retLen,
			&retLen,
		)
		if err != nil {
			return false
		}
	}

	type LUIDAndAttributes struct {
		Luid       windows.LUID
		Attributes uint32
	}
	type TOKEN_PRIVILEGES_HEADER struct {
		PrivilegeCount uint32
	}

	header := (*TOKEN_PRIVILEGES_HEADER)(unsafe.Pointer(&buf[0]))
	if header.PrivilegeCount == 0 {
		return false
	}

	slicePtr := unsafe.Pointer(uintptr(unsafe.Pointer(&buf[0])) + unsafe.Sizeof(header.PrivilegeCount))
	privs := (*[1024]LUIDAndAttributes)(slicePtr)[:header.PrivilegeCount:header.PrivilegeCount]

	for _, p := range privs {
		if p.Luid == luid {
			return (p.Attributes & windows.SE_PRIVILEGE_ENABLED) != 0
		}
	}

	return false
}

func checkElevation() (bool, bool) {
	token, err := openProcessToken(windows.TOKEN_QUERY)
	if err != nil {
		return false, false
	}
	defer token.Close()

	var elevation uint32
	var retLen uint32
	err = windows.GetTokenInformation(
		token,
		windows.TokenElevation,
		(*byte)(unsafe.Pointer(&elevation)),
		uint32(unsafe.Sizeof(elevation)),
		&retLen,
	)
	if err != nil {
		return false, false
	}

	isElevated := elevation != 0

	// Check Administrators group membership
	var adminSID *windows.SID
	err = windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&adminSID,
	)
	if err != nil {
		return isElevated, isElevated
	}
	defer windows.FreeSid(adminSID)

	isMember, err := token.IsMember(adminSID)
	if err != nil {
		return isElevated, isElevated
	}

	return isMember, isElevated
}

// RelaunchAsAdmin executes a new instance of the current application with the "runas" verb.
func RelaunchAsAdmin() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("não foi possível obter o executável: %w", err)
	}

	exePtr, err := syscall.UTF16PtrFromString(exePath)
	if err != nil {
		return err
	}

	verbPtr, err := syscall.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}

	// Pass original args (excluding the executable itself)
	var argsStr string
	if len(os.Args) > 1 {
		argsStr = strings.Join(os.Args[1:], " ")
	}
	argsPtr, _ := syscall.UTF16PtrFromString(argsStr)

	cwd, _ := os.Getwd()
	cwdPtr, _ := syscall.UTF16PtrFromString(filepath.Dir(exePath))
	if cwd != "" {
		cwdPtr, _ = syscall.UTF16PtrFromString(cwd)
	}

	r1, _, errCall := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(exePtr)),
		uintptr(unsafe.Pointer(argsPtr)),
		uintptr(unsafe.Pointer(cwdPtr)),
		windows.SW_SHOWNORMAL,
	)

	// ShellExecute returns > 32 on success
	if r1 <= 32 {
		return fmt.Errorf("falha na elevação UAC (código %d): %w", r1, errCall)
	}

	return nil
}

// RelaunchAsAdminWithIPC launches a hidden (SW_HIDE) elevated instance and streams all its output to the current terminal.
func RelaunchAsAdminWithIPC(rawArgs []string) error {
	// 1. Create local IPC listener
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("falha ao criar socket IPC local: %w", err)
	}
	defer ln.Close()

	ipcAddr := ln.Addr().String()

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("não foi possível obter o executável: %w", err)
	}

	exePtr, err := syscall.UTF16PtrFromString(exePath)
	if err != nil {
		return err
	}

	verbPtr, err := syscall.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}

	// Filter out --admin flag to prevent recursive elevation requests, add --elevated-child, --ipc-addr, and --parent-pid
	var childArgs []string
	childArgs = append(childArgs, 
		"--elevated-child", 
		fmt.Sprintf("--ipc-addr=%s", ipcAddr),
		fmt.Sprintf("--parent-pid=%d", os.Getpid()),
	)

	for _, arg := range rawArgs {
		if arg != "--admin" && arg != "-admin" && !strings.HasPrefix(arg, "--ipc-addr") && arg != "--elevated-child" && !strings.HasPrefix(arg, "--parent-pid") {
			childArgs = append(childArgs, arg)
		}
	}

	argsStr := strings.Join(childArgs, " ")
	argsPtr, _ := syscall.UTF16PtrFromString(argsStr)

	cwd, _ := os.Getwd()
	cwdPtr, _ := syscall.UTF16PtrFromString(filepath.Dir(exePath))
	if cwd != "" {
		cwdPtr, _ = syscall.UTF16PtrFromString(cwd)
	}

	// Launch with SW_HIDE so the elevated child process is completely invisible (no popup window)
	r1, _, errCall := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(exePtr)),
		uintptr(unsafe.Pointer(argsPtr)),
		uintptr(unsafe.Pointer(cwdPtr)),
		windows.SW_HIDE,
	)

	if r1 <= 32 {
		return fmt.Errorf("elevação UAC cancelada ou recusada pelo usuário (código %d): %w", r1, errCall)
	}

	// Wait for child to connect back (with timeout of 35 seconds for UAC prompt)
	type acceptResult struct {
		conn net.Conn
		err  error
	}
	accChan := make(chan acceptResult, 1)
	go func() {
		conn, err := ln.Accept()
		accChan <- acceptResult{conn: conn, err: err}
	}()

	var childConn net.Conn
	select {
	case res := <-accChan:
		if res.err != nil {
			return fmt.Errorf("falha ao aceitar conexão do processo elevado: %w", res.err)
		}
		childConn = res.conn
	case <-time.After(35 * time.Second):
		return fmt.Errorf("tempo limite excedido aguardando aprovação UAC")
	}
	defer childConn.Close()

	// Handle Ctrl+C in parent console to terminate cleanly
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		_ = childConn.Close()
		os.Exit(0)
	}()

	// Stream child stdout/logs directly into the parent terminal's os.Stdout
	_, _ = io.Copy(os.Stdout, childConn)
	return nil
}

// MonitorParentProcess waits for the parent process to terminate and immediately exits, preventing zombie processes.
func MonitorParentProcess(parentPID int) {
	if parentPID <= 0 {
		return
	}
	go func() {
		handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(parentPID))
		if err != nil {
			return
		}
		defer windows.CloseHandle(handle)

		// Wait indefinitely until the parent process terminates or is killed
		event, err := windows.WaitForSingleObject(handle, windows.INFINITE)
		if err == nil && (event == windows.WAIT_OBJECT_0 || event == windows.WAIT_FAILED) {
			os.Exit(0)
		}
	}()
}


