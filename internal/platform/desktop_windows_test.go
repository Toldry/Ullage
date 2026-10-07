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
	idList, err := ItemIDList(target)
	if err != nil {
		t.Fatal(err)
	}
	content := ShellLink(target, "Local disk space analyzer", idList)
	if err := os.WriteFile(link, content, 0o644); err != nil {
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
	// The runner's temp folder has an 8.3 short name; compare long forms.
	same := func(first, second string) bool {
		return strings.EqualFold(CanonicalPath(first), CanonicalPath(second))
	}
	if len(lines) != 3 || !same(lines[0], target) || !same(lines[1], directory) ||
		lines[2] != "Local disk space analyzer" {
		t.Fatalf("shortcut reads back as %q, want %q", lines, target)
	}
}
