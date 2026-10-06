// Command ullage starts the local server and opens the UI in the default browser.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

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

func main() {
	nonetwork.Install()
	noBrowser := flag.Bool("no-browser", false, "print the launch link instead of opening it")
	flag.Parse()

	if err := run(*noBrowser); err != nil {
		fmt.Fprintln(os.Stderr, "ullage:", err)
		os.Exit(1)
	}
}

func run(noBrowser bool) error {
	configDirectory, err := platform.ConfigDirectory()
	if err != nil {
		return err
	}
	if platform.IsElevated() {
		// Lets an elevated scan read folders like System Volume Information. Read-only.
		platform.EnableBackupPrivilege()
	}
	manager, err := session.NewManager(session.NativeEnvironment(configDirectory))
	if err != nil {
		return err
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
			fmt.Fprintln(os.Stderr, "ullage: server stopped:", err)
			requestQuit()
		}
	}()

	launchURL := localServer.NewLaunchURL()
	if noBrowser {
		fmt.Println(launchURL)
	} else if err := platform.OpenBrowser(launchURL); err != nil {
		fmt.Println("Open this link in your browser:", launchURL)
	}

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
				manager.Progress().State != session.StateRunning {
				return localServer.Close()
			}
		}
	}
}
