//go:build windows

package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Windows' own shell must read the hand-written shortcut back to the same target,
// including a non-ASCII folder name.
func TestShellLinkResolves(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "Jörg tools")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "ullage.exe")
	if err := os.WriteFile(target, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "Ullage.lnk")
	if err := os.WriteFile(link, ShellLink(target, "Local disk space analyzer"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "[Console]::OutputEncoding = [Text.Encoding]::UTF8; " +
		"$s = (New-Object -ComObject WScript.Shell).CreateShortcut('" + link + "'); " +
		"Write-Output $s.TargetPath; Write-Output $s.WorkingDirectory; " +
		"Write-Output $s.Description"
	output, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		script).CombinedOutput()
	if err != nil {
		t.Fatalf("powershell: %v %s", err, output)
	}
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(string(output), "\r", "")), "\n")
	if len(lines) != 3 || !strings.EqualFold(lines[0], target) ||
		!strings.EqualFold(lines[1], directory) || lines[2] != "Local disk space analyzer" {
		t.Fatalf("shortcut reads back as %q, want %q", lines, target)
	}
}
