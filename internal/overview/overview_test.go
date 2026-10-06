package overview

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toldry/ullage/internal/platform"
	"github.com/toldry/ullage/internal/recipes"
	"github.com/toldry/ullage/internal/scanner"
	"github.com/toldry/ullage/internal/tiers"
)

func TestDisplayPath(t *testing.T) {
	cases := map[[2]string]string{
		{`C:\Users\mara\projects`, `C:\Users\mara`}: "~/projects",
		{`C:\Users\MARA`, `C:\Users\mara`}:          "~",
		{`C:\Users\marathon\x`, `C:\Users\mara`}:    "C:/Users/marathon/x",
		{`D:\data`, `C:\Users\mara`}:                "D:/data",
		{"/home/ada/.cache", "/home/ada"}:           "~/.cache",
	}
	for input, want := range cases {
		if got := DisplayPath(input[0], input[1]); got != want {
			t.Errorf("DisplayPath(%q) = %q, want %q", input[0], got, want)
		}
	}
}

func write(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildPartitionsAndDrillsIntoDominantFolders(t *testing.T) {
	root := t.TempDir()
	profile := filepath.Join(root, "home", "ada")
	write(t, filepath.Join(profile, "projects", "a", "big.bin"), 6<<20)
	write(t, filepath.Join(profile, "projects", "b", "big.bin"), 5<<20)
	write(t, filepath.Join(profile, "Documents", "doc.bin"), 2<<20)
	write(t, filepath.Join(profile, ".npm", "_cacache", "blob.bin"), 3<<20)

	library, err := recipes.Load()
	if err != nil {
		t.Fatal(err)
	}
	locations := platform.KnownLocations{Variables: map[string][]string{
		"UserProfile": {profile}, "CurrentUserProfile": {profile}}, PathSeparator: '/'}
	classifier := tiers.New(library, locations, "linux", nil)
	result, err := scanner.Start(context.Background(), scanner.Options{Roots: []string{root},
		Reader: platform.NewDirectoryReader(), Classifier: classifier}).Wait()
	if err != nil {
		t.Fatal(err)
	}
	model := Build(Input{Result: result, Classifier: classifier, CurrentProfile: profile,
		Elevated: true})

	if model.Totals.Known < 3<<20 || model.Totals.Unknown < 13<<20 {
		t.Fatalf("totals = %+v", model.Totals)
	}
	if model.Totals.Used != model.Totals.System+model.Totals.Known+model.Totals.Unknown {
		t.Fatalf("folder scan should have no unaccounted space: %+v", model.Totals)
	}
	if model.Known.RecipesMatched != 1 || model.Groups[1].Items[0].Label != "npm cache" {
		t.Fatalf("known = %+v %+v", model.Known, model.Groups[1])
	}
	if model.UnknownFolders != 3 {
		t.Fatalf("unknown folders = %d, want 3", model.UnknownFolders)
	}
	labels := []string{}
	for _, item := range model.Groups[0].Items {
		labels = append(labels, item.Label)
	}
	joined := strings.Join(labels, ",")
	if !strings.Contains(joined, "~/projects") || !strings.Contains(joined, "~/Documents") {
		t.Fatalf("unknown items did not drill down to the profile: %v", labels)
	}
}
