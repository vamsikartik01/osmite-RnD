//go:build windows

package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func runtimeDir() string {
	if d := os.Getenv("LOCALAPPDATA"); d != "" {
		return filepath.Join(d, "otmux")
	}
	return filepath.Join(os.TempDir(), "otmux")
}

// defaultShell prefers PowerShell 7, then Windows PowerShell, then cmd.
func defaultShell() []string {
	for _, name := range []string{"pwsh.exe", "powershell.exe"} {
		if p, err := exec.LookPath(name); err == nil {
			return []string{p, "-NoLogo"}
		}
	}
	if c := os.Getenv("ComSpec"); c != "" {
		return []string{c}
	}
	return []string{`C:\Windows\System32\cmd.exe`}
}

const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
)

// Detach configures cmd to run without a console, outliving the parent.
func Detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: detachedProcess | createNewProcessGroup,
		HideWindow:    true,
	}
}

func canOpenBrowser() bool { return true }

// OpenURL opens url in the default browser, in the background.
func OpenURL(url string) error {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
