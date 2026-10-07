//go:build windows

package instance

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

const (
	pipeBufferBytes = 4096
	launchRequest   = "launch"
)

// Channel is the claimed single-instance pipe.
type Channel struct {
	name       string
	attributes *windows.SecurityAttributes
	first      windows.Handle
}

// pipeName is per user, so two accounts on one PC each get their own instance.
func pipeName(name string) (string, *windows.SecurityAttributes, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", nil, err
	}
	sid := user.User.Sid.String()
	// Only this user may open the pipe; the default would let everyone read from it.
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;" + sid + ")")
	if err != nil {
		return "", nil, err
	}
	attributes := &windows.SecurityAttributes{SecurityDescriptor: descriptor}
	attributes.Length = uint32(unsafeSizeOfSecurityAttributes)
	return `\\.\pipe\` + name + "-" + sid, attributes, nil
}

// Claim makes this process the single instance, or returns ErrAlreadyRunning.
func Claim(name string) (*Channel, error) {
	path, attributes, err := pipeName(name)
	if err != nil {
		return nil, err
	}
	handle, err := createPipe(path, attributes, true)
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_PIPE_BUSY) {
		return nil, ErrAlreadyRunning
	}
	if err != nil {
		return nil, err
	}
	return &Channel{name: path, attributes: attributes, first: handle}, nil
}

func createPipe(path string, attributes *windows.SecurityAttributes, first bool) (
	windows.Handle, error) {
	encoded, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	flags := uint32(windows.PIPE_ACCESS_DUPLEX)
	if first {
		// Fails if the name exists, which is what makes this a single-instance lock.
		flags |= windows.FILE_FLAG_FIRST_PIPE_INSTANCE
	}
	return windows.CreateNamedPipe(encoded, flags,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|
			windows.PIPE_REJECT_REMOTE_CLIENTS,
		windows.PIPE_UNLIMITED_INSTANCES, pipeBufferBytes, pipeBufferBytes, 0, attributes)
}

// Serve answers launch requests with newLaunchURL until the process ends.
func (channel *Channel) Serve(newLaunchURL func() string) {
	handle := channel.first
	for {
		if err := windows.ConnectNamedPipe(handle, nil); err == nil ||
			errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
			channel.answer(handle, newLaunchURL)
		}
		_ = windows.DisconnectNamedPipe(handle)
		_ = windows.CloseHandle(handle)
		next, err := createPipe(channel.name, channel.attributes, false)
		if err != nil {
			return
		}
		handle = next
	}
}

func (channel *Channel) answer(handle windows.Handle, newLaunchURL func() string) {
	// The handle is read and written directly: wrapping it in an *os.File would attach a
	// finalizer that closes it a second time later.
	line, err := bufio.NewReader(handleIO(handle)).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != launchRequest {
		return
	}
	_, _ = handleIO(handle).Write([]byte(newLaunchURL() + "\n"))
	_ = windows.FlushFileBuffers(handle)
}

// handleIO reads and writes a pipe handle without taking ownership of it.
type handleIO windows.Handle

func (handle handleIO) Read(buffer []byte) (int, error) {
	var count uint32
	err := windows.ReadFile(windows.Handle(handle), buffer, &count, nil)
	if count == 0 && err == nil {
		return 0, errors.New("pipe closed")
	}
	return int(count), err
}

func (handle handleIO) Write(buffer []byte) (int, error) {
	var count uint32
	err := windows.WriteFile(windows.Handle(handle), buffer, &count, nil)
	return int(count), err
}

// RequestLaunch asks the running instance for a new launch link.
func RequestLaunch(name string) (string, error) {
	path, _, err := pipeName(name)
	if err != nil {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if _, err := file.Write([]byte(launchRequest + "\n")); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(file).ReadString('\n')
	if err != nil {
		return "", err
	}
	url := strings.TrimSpace(line)
	if !validLaunchURL(url) {
		return "", fmt.Errorf("unexpected reply from the running instance")
	}
	return url, nil
}
