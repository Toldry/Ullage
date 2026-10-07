//go:build windows

package instance

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const unsafeSizeOfSecurityAttributes = unsafe.Sizeof(windows.SecurityAttributes{})
