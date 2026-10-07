//go:build linux

package instance

// Channel is a no-op on Linux: single-instance handling is not implemented for the
// future Linux port yet, so every launch starts its own server.
type Channel struct{}

// Claim always succeeds on Linux.
func Claim(string) (*Channel, error) { return &Channel{}, nil }

// Serve does nothing on Linux.
func (*Channel) Serve(func() string) {}

// RequestLaunch is never needed on Linux, because Claim never fails.
func RequestLaunch(string) (string, error) { return "", ErrAlreadyRunning }
