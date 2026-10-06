package tiers

import (
	"testing"

	"github.com/toldry/ullage/internal/platform"
	"github.com/toldry/ullage/internal/recipes"
	"github.com/toldry/ullage/internal/scanner"
)

func windowsLocations() platform.KnownLocations {
	return platform.KnownLocations{
		Variables: map[string][]string{
			"SystemRoot":         {`C:\Windows`},
			"SystemDrive":        {`C:`},
			"ProgramFiles":       {`C:\Program Files`},
			"ProgramFilesX86":    {`C:\Program Files (x86)`},
			"ProgramData":        {`C:\ProgramData`},
			"ProfilesDirectory":  {`C:\Users`},
			"UserProfile":        {`C:\Users\mara`, `C:\Users\Public`},
			"CurrentUserProfile": {`C:\Users\mara`},
			"AnyVolumeRoot":      {`C:`, `D:`},
		},
		CaseInsensitive: true,
		PathSeparator:   '\\',
	}
}

func newWindowsClassifier(t *testing.T,
	applications []platform.InstalledApplication) *Classifier {
	t.Helper()
	library, err := recipes.Load()
	if err != nil {
		t.Fatal(err)
	}
	return New(library, windowsLocations(), "windows", applications)
}

// classify walks a path the way the scanner would: the first segment is the root.
func classify(classifier *Classifier, segments ...string) scanner.Classification {
	classification, state := classifier.Root(segments[0])
	for _, segment := range segments[1:] {
		classification, state = classifier.Child(classification, state, segment,
			platform.EntryDirectory)
	}
	return classification
}

func recipeOf(classifier *Classifier, classification scanner.Classification) string {
	claim := classifier.ClaimByID(classification.ClaimID)
	if claim == nil {
		return ""
	}
	return claim.Recipe.ID
}

func TestWindowsClassification(t *testing.T) {
	classifier := newWindowsClassifier(t, nil)
	cases := []struct {
		segments []string
		tier     scanner.Tier
		recipe   string
	}{
		{[]string{`C:\`, "Windows", "System32", "ntoskrnl.exe"}, scanner.TierSystem, ""},
		{[]string{`C:\`, "Windows", "WinSxS", "amd64_x"}, scanner.TierKnown, "component-store"},
		{[]string{`C:\`, "Windows", "Temp"}, scanner.TierKnown, "system-temp"},
		{[]string{`C:\`, "pagefile.sys"}, scanner.TierKnown, "pagefile"},
		{[]string{`D:\`, "pagefile.sys"}, scanner.TierKnown, "pagefile"},
		{[]string{`C:\`, "hiberfil.sys"}, scanner.TierKnown, "hibernation-file"},
		{[]string{`C:\`, "Users", "mara", "AppData", "Local", "Docker", "wsl", "disk",
			"docker_data.vhdx"}, scanner.TierKnown, "docker-desktop-wsl"},
		{[]string{`C:\`, "Users", "mara", "AppData", "Local", "Packages",
			"CanonicalGroupLimited.Ubuntu_79rhkp1fndgsc", "LocalState", "ext4.vhdx"},
			scanner.TierKnown, "wsl-distro-disk"},
		{[]string{`c:\`, "USERS", "Mara", "appdata", "LOCAL", "npm-cache", "_cacache"},
			scanner.TierKnown, "npm-cache"},
		{[]string{`C:\`, "Users", "mara", "projects", "app", "node_modules"},
			scanner.TierUnknown, ""},
		{[]string{`C:\`, "Program Files", "Vendor", "app.exe"}, scanner.TierSystem, ""},
		{[]string{`C:\`, "$Recycle.Bin", "S-1-5-21"}, scanner.TierKnown, "recycle-bin"},
		{[]string{`C:\Windows\WinSxS`, "x"}, scanner.TierKnown, "component-store"},
		{[]string{`C:\Users\mara\.nuget`, "packages"}, scanner.TierKnown, "nuget-packages"},
		{[]string{`C:\Users\mara\AppData\Local\Google\Chrome\User Data`, "Profile 2",
			"Cache"}, scanner.TierKnown, "browser-caches"},
		{[]string{`C:\Users\mara\AppData\Local\Google\Chrome\User Data`, "Profile 2",
			"Bookmarks"}, scanner.TierUnknown, ""},
		{[]string{`C:\Users\mara\AppData\Roaming\Adobe\Common`, "Media Cache Files"},
			scanner.TierKnown, "adobe-media-cache"},
		{[]string{`C:\Users\mara\AppData\Local\Packages`,
			"SpotifyAB.SpotifyMusic_zpdnekdrzrea0", "LocalCache", "Spotify", "Data"},
			scanner.TierKnown, "spotify-cache"},
		{[]string{`C:\Windows\SoftwareDistribution`, "Download"}, scanner.TierKnown,
			"windows-update-downloads"},
		{[]string{`C:\Users\mara\Videos`, "Captures"}, scanner.TierUnknown, ""},
	}
	for _, testCase := range cases {
		got := classify(classifier, testCase.segments...)
		if got.Tier != testCase.tier || recipeOf(classifier, got) != testCase.recipe {
			t.Errorf("%v: got %s/%s, want %s/%s", testCase.segments, got.Tier,
				recipeOf(classifier, got), testCase.tier, testCase.recipe)
		}
	}
}

func TestInstalledApplicationsClaimTheirFoldersButNotBroadOnes(t *testing.T) {
	applications := []platform.InstalledApplication{
		{DisplayName: "Editor", InstallLocation: `C:\Program Files\Editor\`},
		{DisplayName: "Greedy root", InstallLocation: `C:\`},
		{DisplayName: "Greedy profile", InstallLocation: `C:\Users\mara`},
		{DisplayName: "Greedy programs", InstallLocation: `C:\Program Files`},
		{DisplayName: "User app", InstallLocation: `C:\Users\mara\AppData\Local\Programs\Tool`},
		{DisplayName: "Brackets [x]", InstallLocation: `D:\Apps\Brackets [x]`},
	}
	classifier := newWindowsClassifier(t, applications)

	editor := classify(classifier, `C:\`, "Program Files", "Editor", "bin")
	if editor.Tier != scanner.TierKnown || classifier.ClaimLabel(editor.ClaimID) !=
		"Installed applications · Editor" {
		t.Errorf("editor = %+v %q", editor, classifier.ClaimLabel(editor.ClaimID))
	}
	if got := classify(classifier, `C:\`, "Users", "mara", "notes"); got.Tier !=
		scanner.TierUnknown {
		t.Errorf("profile claimed by a greedy install location: %+v", got)
	}
	if got := classify(classifier, `C:\`, "Users", "mara", "AppData", "Local", "Programs",
		"Tool"); got.Tier != scanner.TierKnown {
		t.Errorf("user-scope app not claimed: %+v", got)
	}
	if got := classify(classifier, `D:\`, "Apps", "Brackets [x]"); got.Tier !=
		scanner.TierKnown {
		t.Errorf("install location with glob characters must match literally: %+v", got)
	}
	if got := classify(classifier, `D:\`, "Apps", "Brackets x"); got.Tier ==
		scanner.TierKnown {
		t.Errorf("literal location matched as a glob")
	}
}

func TestLinuxClassificationUsesTheSameData(t *testing.T) {
	library, err := recipes.Load()
	if err != nil {
		t.Fatal(err)
	}
	locations := platform.KnownLocations{
		Variables: map[string][]string{"UserProfile": {"/home/ada"},
			"AnyVolumeRoot": {"/"}},
		PathSeparator: '/',
	}
	classifier := New(library, locations, "linux", nil)
	if got := classify(classifier, "/", "home", "ada", ".npm", "_cacache"); got.Tier !=
		scanner.TierKnown {
		t.Errorf("npm cache not claimed on linux: %+v", got)
	}
	if got := classify(classifier, "/", "usr", "lib"); got.Tier != scanner.TierSystem {
		t.Errorf("/usr not System: %+v", got)
	}
	if got := classify(classifier, "/", "home", "ada", ".NPM"); got.Tier !=
		scanner.TierUnknown {
		t.Errorf("linux matching must be case-sensitive: %+v", got)
	}
}

func TestExclusionPatterns(t *testing.T) {
	matcher := NewPathMatcher([]string{"~/vault/**", "D:/vm/**", "secrets"},
		windowsLocations())
	matching := []string{`C:\Users\mara\vault`, `C:\Users\MARA\Vault\deep\x`, `D:\vm\disk`,
		`C:\Users\mara\projects\secrets`}
	for _, path := range matching {
		if !matcher.Matches(path) {
			t.Errorf("%s should be excluded", path)
		}
	}
	for _, path := range []string{`C:\Users\mara\projects`, `C:\vm\x`, `C:\secrets-not`} {
		if matcher.Matches(path) {
			t.Errorf("%s should not be excluded", path)
		}
	}
}
