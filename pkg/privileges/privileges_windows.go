//go:build windows

package privileges

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// uacTimeout is how long the parent waits for the elevated child to call back.
// It has to cover a human reading the UAC prompt.
const uacTimeout = 120 * time.Second

// ShellExecuteExW mask flags.
const (
	seeMaskNoCloseProcess = 0x00000040 // keep hProcess open so we learn the PID
	seeMaskNoAsync        = 0x00000100 // complete before returning
	seeMaskFlagNoUI       = 0x00000400 // no error dialog, we report it ourselves
)

// shellExecuteInfoW mirrors SHELLEXECUTEINFOW.
type shellExecuteInfoW struct {
	cbSize         uint32
	fMask          uint32
	hwnd           uintptr
	lpVerb         *uint16
	lpFile         *uint16
	lpParameters   *uint16
	lpDirectory    *uint16
	nShow          int32
	hInstApp       windows.Handle
	lpIDList       uintptr
	lpClass        *uint16
	hkeyClass      windows.Handle
	dwHotKey       uint32
	hIconOrMonitor windows.Handle
	hProcess       windows.Handle
}

// processExit ends this process. It is a variable so tests can observe the
// decision instead of dying with it.
var processExit = os.Exit

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
	modshell32                = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteW         = modshell32.NewProc("ShellExecuteW")
	procShellExecuteExW       = modshell32.NewProc("ShellExecuteExW")
)

// List of high-privilege Windows security tokens to request
var criticalPrivileges = []string{
	"SeBackupPrivilege",               // Bypasses all file/folder read ACLs (NTFS backup intent)
	"SeRestorePrivilege",              // Bypasses all file/folder write ACLs
	"SeSecurityPrivilege",             // Access SACL and security descriptors
	"SeTakeOwnershipPrivilege",        // Take ownership of files if needed
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

	// Pass original args (excluding the executable itself), quoted so a path
	// with spaces survives the trip.
	var argsStr string
	if len(os.Args) > 1 {
		argsStr = escapeArgs(os.Args[1:])
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

	argsStr := escapeArgs(buildRelaunchArgs(rawArgs, ipcAddr, os.Getpid()))
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

	// Wait for the child to connect back. The UAC prompt waits for a human, so
	// the window is generous: 120 seconds.
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
	case <-time.After(uacTimeout):
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

// MonitorParentProcess waits for the parent process to terminate and exits with
// it, so an elevated child never outlives the window that asked for it.
//
// A parent that cannot be opened is a parent that is already gone or beyond
// reach: this process exits immediately instead of staying alive forever
// holding the scanned tree in memory (M7).
func MonitorParentProcess(parentPID int) {
	if parentPID <= 0 {
		// Nothing to monitor: this process was not launched by another one.
		return
	}
	go func() {
		waitForParent(uint32(parentPID))
		processExit(0)
	}()
}

// waitForParent blocks until the parent process ends or becomes unreachable.
func waitForParent(parentPID uint32) {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, parentPID)
	if err != nil {
		return
	}
	defer windows.CloseHandle(handle)
	_, _ = windows.WaitForSingleObject(handle, windows.INFINITE)
}

// WaitForProcessExit blocks until the process ends or timeout expires. A
// process that no longer exists counts as ended. A timeout of zero or less
// waits indefinitely.
func WaitForProcessExit(pid uint32, timeout time.Duration) error {
	if pid == 0 {
		return errors.New("pid inválido")
	}

	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return nil // already gone
		}
		return fmt.Errorf("não foi possível acompanhar o processo %d: %w", pid, err)
	}
	defer windows.CloseHandle(handle)

	wait := uint32(windows.INFINITE)
	if timeout > 0 {
		ms := timeout.Milliseconds()
		if ms >= int64(windows.INFINITE) {
			ms = int64(windows.INFINITE) - 1
		}
		wait = uint32(ms)
	}

	event, err := windows.WaitForSingleObject(handle, wait)
	switch {
	case event == windows.WAIT_OBJECT_0:
		return nil
	case event == uint32(windows.WAIT_TIMEOUT):
		return fmt.Errorf("o processo %d continuou em execução após %s", pid, timeout)
	case err != nil:
		return fmt.Errorf("falha ao aguardar o processo %d: %w", pid, err)
	default:
		return fmt.Errorf("falha ao aguardar o processo %d (evento %d)", pid, event)
	}
}

// LaunchElevatedHandoff starts a new elevated instance of this executable with
// the given arguments and returns its PID without waiting for it. The caller
// keeps running and decides when to hand over (see WaitForProcessExit and the
// --handoff flag), so elevating from the interface never leaves two instances
// fighting over the same port.
func LaunchElevatedHandoff(args []string) (uint32, error) {
	exePath, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("não foi possível obter o executável: %w", err)
	}

	verbPtr, err := syscall.UTF16PtrFromString("runas")
	if err != nil {
		return 0, err
	}
	exePtr, err := syscall.UTF16PtrFromString(exePath)
	if err != nil {
		return 0, err
	}
	var paramsPtr *uint16
	if line := escapeArgs(args); line != "" {
		if paramsPtr, err = syscall.UTF16PtrFromString(line); err != nil {
			return 0, err
		}
	}
	dirPtr, err := syscall.UTF16PtrFromString(filepath.Dir(exePath))
	if err != nil {
		return 0, err
	}

	info := shellExecuteInfoW{
		fMask:        seeMaskNoCloseProcess | seeMaskNoAsync | seeMaskFlagNoUI,
		lpVerb:       verbPtr,
		lpFile:       exePtr,
		lpParameters: paramsPtr,
		lpDirectory:  dirPtr,
		nShow:        windows.SW_SHOWNORMAL,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))

	// ShellExecuteExW expects a COM apartment on the calling thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err == nil {
		defer windows.CoUninitialize()
	}

	r1, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if r1 == 0 {
		if errors.Is(callErr, windows.ERROR_CANCELLED) {
			return 0, errors.New("elevação recusada pelo usuário no aviso do UAC")
		}
		return 0, fmt.Errorf("falha ao iniciar a instância elevada: %w", callErr)
	}
	if info.hProcess == 0 {
		return 0, errors.New("o Windows não devolveu o processo elevado")
	}
	defer windows.CloseHandle(info.hProcess)

	pid, err := windows.GetProcessId(info.hProcess)
	if err != nil {
		return 0, fmt.Errorf("não foi possível identificar o processo elevado: %w", err)
	}
	return pid, nil
}

// escapeArgs joins arguments into a command line the child parses back into
// exactly the same strings, quoting spaces and quotes. Empty arguments are
// dropped: they would become a stray pair of quotes.
func escapeArgs(args []string) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "" {
			continue
		}
		parts = append(parts, syscall.EscapeArg(arg))
	}
	return strings.Join(parts, " ")
}

// buildRelaunchArgs prepends the handshake flags for the elevated child and
// drops the flags that belong to this process, so the child never tries to
// elevate again nor reuses the parent's IPC address.
func buildRelaunchArgs(rawArgs []string, ipcAddr string, parentPID int) []string {
	childArgs := []string{
		"--elevated-child",
		fmt.Sprintf("--ipc-addr=%s", ipcAddr),
		fmt.Sprintf("--parent-pid=%d", parentPID),
	}

	for _, arg := range rawArgs {
		switch {
		case arg == "--admin", arg == "-admin", arg == "--elevated-child":
			continue
		case strings.HasPrefix(arg, "--ipc-addr"), strings.HasPrefix(arg, "--parent-pid"):
			continue
		}
		childArgs = append(childArgs, arg)
	}

	return childArgs
}
