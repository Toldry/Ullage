// Package session owns the per-launch state: the running or finished scan, its session
// code, and the scan log. HTTP handlers call into it; it never touches HTTP itself.
package session

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/toldry/ullage/internal/overview"
	"github.com/toldry/ullage/internal/platform"
	"github.com/toldry/ullage/internal/recipes"
	"github.com/toldry/ullage/internal/scanner"
	"github.com/toldry/ullage/internal/storage"
	"github.com/toldry/ullage/internal/tiers"
)

// State of the scan lifecycle.
type State string

const (
	StateIdle      State = "idle"
	StateRunning   State = "running"
	StateFinished  State = "finished"
	StateCancelled State = "cancelled"
	StateFailed    State = "failed"
)

// Environment is everything the manager needs from the operating system. Tests replace
// it with fakes; production uses NativeEnvironment.
type Environment struct {
	Reader                platform.DirectoryReader
	Locations             platform.KnownLocations
	PlatformName          string
	Elevated              bool
	ListVolumes           func() ([]platform.Volume, error)
	VolumeFor             func(path string) (platform.Volume, error)
	InstalledApplications func() ([]platform.InstalledApplication, error)
	BeginBackgroundMode   func() func()
	ConfigDirectory       string
}

// NativeEnvironment wires the real platform implementation.
func NativeEnvironment(configDirectory string) Environment {
	return Environment{
		Reader:                platform.NewDirectoryReader(),
		Locations:             platform.DiscoverKnownLocations(),
		PlatformName:          platform.Name,
		Elevated:              platform.IsElevated(),
		ListVolumes:           platform.ListVolumes,
		VolumeFor:             platform.VolumeFor,
		InstalledApplications: platform.InstalledApplications,
		BeginBackgroundMode:   platform.BeginBackgroundMode,
		ConfigDirectory:       configDirectory,
	}
}

// Completed is a finished scan and everything derived from it.
type Completed struct {
	Code       string
	Result     *scanner.Result
	Classifier *tiers.Classifier
	Overview   overview.Model
}

type running struct {
	scan       *scanner.Scan
	cancel     context.CancelFunc
	code       string
	roots      []string
	classifier *tiers.Classifier
	volumes    map[string]platform.Volume
	expected   int64
}

// Manager is safe for concurrent use.
type Manager struct {
	environment Environment
	library     *recipes.Library

	lock      sync.Mutex
	config    storage.Config
	state     State
	failure   string
	current   *running
	completed *Completed
	log       *eventLog
}

// NewManager loads the recipe library and saved config.
func NewManager(environment Environment) (*Manager, error) {
	library, err := recipes.Load()
	if err != nil {
		return nil, err
	}
	config, err := storage.LoadConfig(environment.ConfigDirectory)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return &Manager{environment: environment, library: library, config: config,
		state: StateIdle, log: newEventLog(400)}, nil
}

// Config returns the current preferences.
func (manager *Manager) Config() storage.Config {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	return manager.config
}

// UpdateConfig saves new preferences.
func (manager *Manager) UpdateConfig(update func(*storage.Config)) (storage.Config, error) {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	next := manager.config
	next.Exclusions = append([]string{}, manager.config.Exclusions...)
	update(&next)
	if err := storage.SaveConfig(manager.environment.ConfigDirectory, next); err != nil {
		return manager.config, err
	}
	manager.config = next
	return next, nil
}

// Elevated reports whether the process runs with administrator rights.
func (manager *Manager) Elevated() bool { return manager.environment.Elevated }

// CurrentProfile is the path rendered as ~.
func (manager *Manager) CurrentProfile() string {
	profiles := manager.environment.Locations.Variables["CurrentUserProfile"]
	if len(profiles) == 0 {
		return ""
	}
	return profiles[0]
}

// Volumes lists drives the user can scan.
func (manager *Manager) Volumes() ([]platform.Volume, error) {
	return manager.environment.ListVolumes()
}

// ScanRequest is what the Scan screen submits.
type ScanRequest struct {
	Roots                []string `json:"roots"`
	IncludeOtherProfiles bool     `json:"includeOtherProfiles"`
	Exclusions           []string `json:"exclusions"`
}

// ErrScanRunning is returned when a scan is requested while another runs.
var ErrScanRunning = errors.New("a scan is already running")

// StartScan validates the roots and begins scanning.
func (manager *Manager) StartScan(request ScanRequest) error {
	roots, volumes, expected, err := manager.validateRoots(request.Roots)
	if err != nil {
		return err
	}
	applications, _ := manager.environment.InstalledApplications()
	classifier := tiers.New(manager.library, manager.environment.Locations,
		manager.environment.PlatformName, applications)

	manager.lock.Lock()
	defer manager.lock.Unlock()
	if manager.state == StateRunning {
		return ErrScanRunning
	}
	manager.config.IncludeOtherProfiles = request.IncludeOtherProfiles
	manager.config.Exclusions = cleanPatterns(request.Exclusions)
	// Saving preferences is a convenience; a read-only profile must not block scanning.
	_ = storage.SaveConfig(manager.environment.ConfigDirectory, manager.config)

	code, err := newSessionCode()
	if err != nil {
		return err
	}
	manager.log = newEventLog(400)
	scanContext, cancel := context.WithCancel(context.Background())
	restorePriority := func() {}
	if manager.environment.BeginBackgroundMode != nil {
		restorePriority = manager.environment.BeginBackgroundMode()
	}
	scan := scanner.Start(scanContext, scanner.Options{
		Roots:      roots,
		Reader:     manager.environment.Reader,
		Classifier: classifier,
		Exclude:    manager.exclusionPredicate(request),
		OnEvent:    manager.recordEvent,
	})
	manager.current = &running{scan: scan, cancel: cancel, code: code, roots: roots,
		classifier: classifier, volumes: volumes, expected: expected}
	manager.state = StateRunning
	manager.failure = ""
	go manager.awaitScan(manager.current, restorePriority)
	return nil
}

func cleanPatterns(patterns []string) []string {
	cleaned := []string{}
	for _, pattern := range patterns {
		if trimmed := strings.TrimSpace(pattern); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return cleaned
}

// validateRoots accepts local volume roots and local folders only. Network paths are
// refused because scanning them would send traffic over the network.
func (manager *Manager) validateRoots(requested []string) ([]string,
	map[string]platform.Volume, int64, error) {
	if len(requested) == 0 {
		return nil, nil, 0, errors.New("choose at least one drive or folder")
	}
	knownVolumes, err := manager.environment.ListVolumes()
	if err != nil {
		return nil, nil, 0, err
	}
	byRoot := map[string]platform.Volume{}
	for _, volume := range knownVolumes {
		byRoot[filepath.Clean(volume.RootPath)] = volume
	}
	var roots []string
	volumes := map[string]platform.Volume{}
	var expected int64
	for _, root := range requested {
		cleaned := filepath.Clean(root)
		// Check the raw input too: on Unix, Clean collapses a leading "//".
		trimmed := strings.TrimSpace(root)
		if strings.HasPrefix(trimmed, `\\`) || strings.HasPrefix(trimmed, "//") ||
			strings.HasPrefix(cleaned, `\\`) {
			return nil, nil, 0, fmt.Errorf("%s is a network path; Ullage only scans local disks",
				root)
		}
		if !filepath.IsAbs(cleaned) {
			return nil, nil, 0, fmt.Errorf("%s is not an absolute path", root)
		}
		if volume, isVolumeRoot := byRoot[cleaned]; isVolumeRoot {
			volumes[cleaned] = volume
			expected += volume.UsedBytes()
		} else if _, err := manager.environment.VolumeFor(cleaned); err != nil {
			return nil, nil, 0, fmt.Errorf("%s is not on a local disk: %w", root, err)
		}
		roots = append(roots, cleaned)
	}
	return roots, volumes, expected, nil
}

// exclusionPredicate combines the user's patterns with the other-profiles option.
func (manager *Manager) exclusionPredicate(request ScanRequest) func(string) bool {
	locations := manager.environment.Locations
	matcher := tiers.NewPathMatcher(request.Exclusions, locations)
	otherProfiles := map[string]bool{}
	if !request.IncludeOtherProfiles {
		current := manager.CurrentProfile()
		for _, profile := range locations.Variables["UserProfile"] {
			name := strings.ToLower(filepath.Base(profile))
			shared := name == "public" || name == "default" || name == "default user" ||
				name == "all users"
			if !shared && !strings.EqualFold(profile, current) {
				otherProfiles[normalizeKey(profile, locations.CaseInsensitive)] = true
			}
		}
	}
	return func(path string) bool {
		if otherProfiles[normalizeKey(path, locations.CaseInsensitive)] {
			return true
		}
		return matcher.Matches(path)
	}
}

func normalizeKey(path string, caseInsensitive bool) string {
	cleaned := filepath.Clean(path)
	if caseInsensitive {
		return strings.ToLower(cleaned)
	}
	return cleaned
}

func (manager *Manager) awaitScan(current *running, restorePriority func()) {
	result, err := current.scan.Wait()
	restorePriority()
	manager.lock.Lock()
	defer manager.lock.Unlock()
	if manager.current != current {
		return
	}
	switch {
	case errors.Is(err, context.Canceled):
		manager.state = StateCancelled
	case err != nil:
		manager.state = StateFailed
		manager.failure = err.Error()
	default:
		progress := current.scan.Snapshot()
		manager.completed = &Completed{
			Code:       current.code,
			Result:     result,
			Classifier: current.classifier,
			Overview: overview.Build(overview.Input{
				Result: result, Classifier: current.classifier, Volumes: current.volumes,
				Elevated: manager.environment.Elevated, CurrentProfile: manager.CurrentProfile(),
				Files: progress.Files, Directories: progress.Directories,
			}),
		}
		manager.state = StateFinished
	}
}

// Pause, Resume and Cancel control the running scan; they are no-ops otherwise.
func (manager *Manager) Pause() { manager.withRunning(func(r *running) { r.scan.Pause() }) }

// Resume continues a paused scan.
func (manager *Manager) Resume() { manager.withRunning(func(r *running) { r.scan.Resume() }) }

// Cancel stops the running scan. A previous completed scan stays available.
func (manager *Manager) Cancel() { manager.withRunning(func(r *running) { r.cancel() }) }

func (manager *Manager) withRunning(action func(*running)) {
	manager.lock.Lock()
	current := manager.current
	isRunning := manager.state == StateRunning
	manager.lock.Unlock()
	if isRunning && current != nil {
		action(current)
	}
}

// Completed returns the last finished scan, or nil.
func (manager *Manager) Completed() *Completed {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	return manager.completed
}

// Progress is the live view of the current scan.
type Progress struct {
	State          State    `json:"state"`
	Failure        string   `json:"failure,omitempty"`
	SessionCode    string   `json:"sessionCode"`
	Roots          []string `json:"roots"`
	Files          int64    `json:"files"`
	Directories    int64    `json:"directories"`
	AllocatedBytes int64    `json:"allocatedBytes"`
	ExpectedBytes  int64    `json:"expectedBytes"`
	SystemBytes    int64    `json:"systemBytes"`
	KnownBytes     int64    `json:"knownBytes"`
	UnknownBytes   int64    `json:"unknownBytes"`
	DeniedCount    int64    `json:"deniedCount"`
	LinkCount      int64    `json:"linkCount"`
	Placeholders   int64    `json:"placeholders"`
	CurrentPath    string   `json:"currentPath"`
	ElapsedSeconds float64  `json:"elapsedSeconds"`
	Paused         bool     `json:"paused"`
	LogSequence    int64    `json:"logSequence"`
}

// Progress returns a snapshot of the current scan state.
func (manager *Manager) Progress() Progress {
	manager.lock.Lock()
	state, failure, current := manager.state, manager.failure, manager.current
	logSequence := manager.log.lastSequence()
	manager.lock.Unlock()
	progress := Progress{State: state, Failure: failure, LogSequence: logSequence,
		Roots: []string{}}
	if current == nil {
		return progress
	}
	snapshot := current.scan.Snapshot()
	progress.SessionCode = current.code
	for _, root := range current.roots {
		progress.Roots = append(progress.Roots,
			overview.DisplayPath(root, manager.CurrentProfile()))
	}
	progress.Files = snapshot.Files
	progress.Directories = snapshot.Directories
	progress.AllocatedBytes = snapshot.AllocatedBytes
	progress.ExpectedBytes = current.expected
	progress.SystemBytes = snapshot.TierBytes[scanner.TierSystem]
	progress.KnownBytes = snapshot.TierBytes[scanner.TierKnown]
	progress.UnknownBytes = snapshot.TierBytes[scanner.TierUnknown]
	progress.DeniedCount = snapshot.DeniedCount
	progress.LinkCount = snapshot.LinkCount
	progress.Placeholders = snapshot.Placeholders
	progress.CurrentPath = overview.DisplayPath(snapshot.CurrentPath, manager.CurrentProfile())
	progress.ElapsedSeconds = snapshot.Elapsed.Seconds()
	progress.Paused = snapshot.Paused
	return progress
}

// LogSince returns scan log lines after the given sequence number.
func (manager *Manager) LogSince(sequence int64) []LogLine {
	manager.lock.Lock()
	log := manager.log
	manager.lock.Unlock()
	return log.since(sequence)
}

func (manager *Manager) recordEvent(event scanner.Event) {
	manager.lock.Lock()
	log := manager.log
	manager.lock.Unlock()
	log.add(LogLine{
		Time:  event.Time,
		Kind:  string(event.Kind),
		Text:  event.Text,
		Path:  overview.DisplayPath(event.Path, manager.CurrentProfile()),
		Bytes: event.Bytes,
	})
}

// sessionAlphabet omits 0/O and 1/I/L so codes survive being read aloud or retyped.
const sessionAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

func newSessionCode() (string, error) {
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	code := make([]byte, len(random))
	for index, value := range random {
		code[index] = sessionAlphabet[int(value)%len(sessionAlphabet)]
	}
	return string(code), nil
}

// LogLine is one entry of the scan log shown on the Scanning screen.
type LogLine struct {
	Sequence int64     `json:"sequence"`
	Time     time.Time `json:"time"`
	Kind     string    `json:"kind"`
	Text     string    `json:"text"`
	Path     string    `json:"path,omitempty"`
	Bytes    int64     `json:"bytes"`
}

type eventLog struct {
	lock     sync.Mutex
	capacity int
	lines    []LogLine
	next     int64
}

func newEventLog(capacity int) *eventLog {
	return &eventLog{capacity: capacity, next: 1}
}

func (log *eventLog) add(line LogLine) {
	log.lock.Lock()
	defer log.lock.Unlock()
	line.Sequence = log.next
	log.next++
	log.lines = append(log.lines, line)
	if len(log.lines) > log.capacity {
		log.lines = log.lines[len(log.lines)-log.capacity:]
	}
}

func (log *eventLog) since(sequence int64) []LogLine {
	log.lock.Lock()
	defer log.lock.Unlock()
	result := []LogLine{}
	for _, line := range log.lines {
		if line.Sequence > sequence {
			result = append(result, line)
		}
	}
	return result
}

func (log *eventLog) lastSequence() int64 {
	log.lock.Lock()
	defer log.lock.Unlock()
	return log.next - 1
}
