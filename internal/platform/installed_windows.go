//go:build windows

package platform

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

const uninstallKeyPath = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`

// InstalledApplications reads the Uninstall registry keys that back Settings > Apps,
// from both registry views and from the current user's hive.
func InstalledApplications() ([]InstalledApplication, error) {
	sources := []struct {
		root   registry.Key
		access uint32
	}{
		{registry.LOCAL_MACHINE, registry.WOW64_64KEY},
		{registry.LOCAL_MACHINE, registry.WOW64_32KEY},
		{registry.CURRENT_USER, 0},
	}
	seen := map[string]bool{}
	var applications []InstalledApplication
	for _, source := range sources {
		for _, application := range readUninstallKey(source.root, source.access) {
			key := strings.ToLower(application.DisplayName + "|" + application.InstallLocation)
			if !seen[key] {
				seen[key] = true
				applications = append(applications, application)
			}
		}
	}
	return applications, nil
}

func readUninstallKey(root registry.Key, view uint32) []InstalledApplication {
	parent, err := registry.OpenKey(root, uninstallKeyPath,
		registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE|view)
	if err != nil {
		return nil
	}
	defer parent.Close()
	names, err := parent.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}
	var applications []InstalledApplication
	for _, name := range names {
		child, err := registry.OpenKey(parent, name, registry.QUERY_VALUE|view)
		if err != nil {
			continue
		}
		application, ok := readApplication(child)
		child.Close()
		if ok {
			applications = append(applications, application)
		}
	}
	return applications
}

func readApplication(key registry.Key) (InstalledApplication, bool) {
	displayName, _, err := key.GetStringValue("DisplayName")
	if err != nil || strings.TrimSpace(displayName) == "" {
		return InstalledApplication{}, false
	}
	// Components hidden from Settings > Apps are parts of other products.
	if systemComponent, _, err := key.GetIntegerValue("SystemComponent"); err == nil &&
		systemComponent == 1 {
		return InstalledApplication{}, false
	}
	publisher, _, _ := key.GetStringValue("Publisher")
	location, _, _ := key.GetStringValue("InstallLocation")
	estimatedKilobytes, _, _ := key.GetIntegerValue("EstimatedSize")
	return InstalledApplication{
		DisplayName:     strings.TrimSpace(displayName),
		Publisher:       strings.TrimSpace(publisher),
		InstallLocation: strings.Trim(strings.TrimSpace(location), `"`),
		EstimatedBytes:  int64(estimatedKilobytes) * 1024,
	}, true
}
