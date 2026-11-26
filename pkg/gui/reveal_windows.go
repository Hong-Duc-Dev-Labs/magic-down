//go:build windows

package gui

import (
	"fmt"
	"os/exec"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	ole32DLL                       = syscall.NewLazyDLL("ole32.dll")
	shell32DLL                     = syscall.NewLazyDLL("shell32.dll")
	procCoInitializeEx             = ole32DLL.NewProc("CoInitializeEx")
	procCoUninitialize             = ole32DLL.NewProc("CoUninitialize")
	procILCreateFromPathW          = shell32DLL.NewProc("ILCreateFromPathW")
	procILFree                     = shell32DLL.NewProc("ILFree")
	procSHOpenFolderAndSelectItems = shell32DLL.NewProc("SHOpenFolderAndSelectItems")
	procShellExecuteW              = shell32DLL.NewProc("ShellExecuteW")
)

const (
	coinitApartmentThreaded = 0x2
	coinitDisableOle1DDE    = 0x4
	rpcEChangedMode         = 0x80010106
	swShowNormal            = 1
)

func buildExplorerSelectCmdLine(absPath string) string {
	return `explorer.exe /select,"` + absPath + `"`
}

func buildExplorerDirCmdLine(absPath string) string {
	return `explorer.exe "` + absPath + `"`
}

func revealInFileManager(target revealTarget) error {
	if target.Path == "" {
		return nil
	}
	if target.SelectFile {
		if err := shOpenFolderAndSelectItem(target.Path); err == nil {
			return nil
		}
		return launchExplorerWithCmdLine(buildExplorerSelectCmdLine(target.Path))
	}
	if err := shellOpenDirectory(target.Path); err == nil {
		return nil
	}
	return launchExplorerWithCmdLine(buildExplorerDirCmdLine(target.Path))
}

// shOpenFolderAndSelectItem opens the parent folder in Windows Explorer and
// selects absPath using the Win32 Shell API. Unlike explorer.exe CLI parsing,
// PIDL-based selection handles spaces, commas, square brackets, and Unicode
// (Korean, Vietnamese, CJK, etc.) without argument escaping issues.
func shOpenFolderAndSelectItem(absPath string) error {
	pathPtr, err := syscall.UTF16PtrFromString(absPath)
	if err != nil {
		return err
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hrInit, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded|coinitDisableOle1DDE)
	if hrInit == 0 || hrInit == 1 {
		defer procCoUninitialize.Call()
	} else if uint32(hrInit) != rpcEChangedMode {
		return fmt.Errorf("CoInitializeEx failed: 0x%08x", uint32(hrInit))
	}

	pidl, _, _ := procILCreateFromPathW.Call(uintptr(unsafe.Pointer(pathPtr)))
	if pidl == 0 {
		return fmt.Errorf("ILCreateFromPathW failed for %q", absPath)
	}
	defer procILFree.Call(pidl)

	hr, _, _ := procSHOpenFolderAndSelectItems.Call(pidl, 0, 0, 0)
	if int32(hr) < 0 {
		return fmt.Errorf("SHOpenFolderAndSelectItems failed: 0x%08x", uint32(hr))
	}
	return nil
}

func shellOpenDirectory(absDir string) error {
	verbPtr, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	dirPtr, err := syscall.UTF16PtrFromString(absDir)
	if err != nil {
		return err
	}
	ret, _, _ := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(dirPtr)),
		0,
		0,
		swShowNormal,
	)
	if ret <= 32 {
		return fmt.Errorf("ShellExecuteW failed with code %d", ret)
	}
	return nil
}

func launchExplorerWithCmdLine(cmdLine string) error {
	cmd := exec.Command("explorer.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: cmdLine,
	}
	return cmd.Start()
}

func openExternalURL(rawURL string) error {
	if rawURL == "" {
		return nil
	}
	return shellOpenDirectory(rawURL)
}
