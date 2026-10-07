// Package instance keeps Ullage to one running copy per user. A second launch asks the
// running copy for a fresh launch link and opens it, instead of starting a second server
// with its own scan. On Windows this goes through a named pipe that only the current
// user can open; it never touches the network.
package instance

import (
	"errors"
	"strings"
)

// ErrAlreadyRunning means another copy holds the channel.
var ErrAlreadyRunning = errors.New("Ullage is already running")

// validLaunchURL checks a reply before anything opens it in a browser.
func validLaunchURL(text string) bool {
	return strings.HasPrefix(text, "http://127.0.0.1:") && !strings.ContainsAny(text, " \r\n")
}
