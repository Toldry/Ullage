// Command ullage starts the local server and opens the UI in the default browser.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/toldry/ullage/internal/instance"
	"github.com/toldry/ullage/internal/nonetwork"
	"github.com/toldry/ullage/internal/platform"
	"github.com/toldry/ullage/internal/server"
	"github.com/toldry/ullage/internal/session"
	"github.com/toldry/ullage/web"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "0.1.0-dev"

// idleShutdown ends the process when no tab has been in touch for this long and no scan
// runs. Open tabs send a heartbeat every minute, even when in the background.
const idleShutdown = 15 * time.Minute

// instanceName names the single-instance channel; the platform adds the user's identity.
const instanceName = "Ullage"

func main() {
	nonetwork.Install()
	noBrowser := flag.Bool("no-browser", false, "print the launch link instead of opening it")
	installShortcut := flag.Bool("install-shortcut", false,
		"create the Start menu shortcut and exit")
	removeShortcut := flag.Bool("remove-shortcut", false,
		"remove the Start menu shortcut and exit")
	flag.Parse()

	var err error
	switch {
	case *installShortcut:
		err = installStartMenuShortcut()
	case *removeShortcut:
		err = platform.RemoveShortcut()
	default:
		err = run(*noBrowser)
	}
	if err != nil {
		// The release build has no console, so errors go to a message box on Windows.
		platform.ShowError(err.Error())
		os.Exit(1)
	}
}

func installStartMenuShortcut() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	return platform.InstallShortcut(executable)
}

// isTemporaryBuild reports whether the executable runs from the temp folder, as with
// "go run"; a shortcut to it would break as soon as the build is cleaned up.
func isTemporaryBuild() bool {
	executable, err := os.Executable()
	if err != nil {
		return true
	}
	temporary := filepath.Clean(os.TempDir()) + string(filepath.Separator)
	return strings.HasPrefix(strings.ToLower(executable), strings.ToLower(temporary))
}

func open(launchURL string, noBrowser bool) {
	if noBrowser {
		fmt.Println(launchURL)
	} else if err := platform.OpenBrowser(launchURL); err != nil {
		platform.ShowError("Open this link in your browser: " + launchURL)
	}
}

func run(noBrowser bool) error {
	channel, err := instance.Claim(instanceName)
	if errors.Is(err, instance.ErrAlreadyRunning) {
		launchURL, requestErr := instance.RequestLaunch(instanceName)
		if requestErr != nil {
			return fmt.Errorf("Ullage is already running but did not answer: %w", requestErr)
		}
		open(launchURL, noBrowser)
		return nil
	}
	if err != nil {
		return err
	}

	configDirectory, err := platform.ConfigDirectory()
	if err != nil {
		return err
	}
	if platform.IsElevated() {
		// Lets an elevated scan read folders like System Volume Information. Read-only.
		platform.EnableBackupPrivilege()
	}
	environment := session.NativeEnvironment(configDirectory)
	if dataDirectory, err := platform.DataDirectory(); err == nil {
		environment.HistoryPath = filepath.Join(dataDirectory, "history.json")
	}
	manager, err := session.NewManager(environment)
	if err != nil {
		return err
	}
	if manager.FirstRun() && platform.Name == "windows" && !isTemporaryBuild() {
		// Best effort: the app works the same without a Start menu entry.
		_ = installStartMenuShortcut()
	}

	quit := make(chan struct{}, 1)
	requestQuit := func() {
		select {
		case quit <- struct{}{}:
		default:
		}
	}
	options := server.Options{Version: version, Static: web.Files(), OnQuit: requestQuit}
	if platform.Name == "windows" && !platform.IsElevated() {
		options.RelaunchElevated = func() error { return platform.RelaunchElevated(nil) }
	}
	localServer, err := server.New(manager, options)
	if err != nil {
		return err
	}
	go func() {
		if err := localServer.Serve(); err != nil {
			platform.ShowError("The local server stopped: " + err.Error())
			requestQuit()
		}
	}()
	go channel.Serve(localServer.NewLaunchURL)

	open(localServer.NewLaunchURL(), noBrowser)

	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	idleCheck := time.NewTicker(time.Minute)
	defer idleCheck.Stop()
	for {
		select {
		case <-quit:
			return localServer.Close()
		case <-interrupts:
			return localServer.Close()
		case <-idleCheck.C:
			if localServer.IdleFor() > idleShutdown &&
				manager.Progress().State != session.StateRunning &&
				!manager.DeletionRunning() {
				return localServer.Close()
			}
		}
	}
}
