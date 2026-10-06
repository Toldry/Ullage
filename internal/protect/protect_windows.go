//go:build windows

package protect

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const cryptProtectUIForbidden = 0x1

type dataProtectionAPI struct{}

// Native returns the DPAPI protector (current user scope). configDirectory is unused on
// Windows; DPAPI keys live in the user's profile.
func Native(configDirectory string) Protector {
	return dataProtectionAPI{}
}

func blobOf(data []byte) *windows.DataBlob {
	if len(data) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
}

func takeBlob(blob *windows.DataBlob) []byte {
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(blob.Data)))
	return append([]byte(nil), unsafe.Slice(blob.Data, blob.Size)...)
}

func (dataProtectionAPI) Protect(plain []byte) ([]byte, error) {
	var sealed windows.DataBlob
	err := windows.CryptProtectData(blobOf(plain), nil, blobOf(entropy), 0, nil,
		cryptProtectUIForbidden, &sealed)
	if err != nil {
		return nil, fmt.Errorf("DPAPI protect: %w", err)
	}
	return takeBlob(&sealed), nil
}

func (dataProtectionAPI) Unprotect(sealed []byte) ([]byte, error) {
	if len(sealed) == 0 {
		return nil, ErrUnreadable
	}
	var plain windows.DataBlob
	err := windows.CryptUnprotectData(blobOf(sealed), nil, blobOf(entropy), 0, nil,
		cryptProtectUIForbidden, &plain)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	return takeBlob(&plain), nil
}
