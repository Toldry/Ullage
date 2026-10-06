//go:build linux

package protect

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// keyFileProtector is the Linux backend: AES-256-GCM with a key in a file readable only by
// the user. It is weaker than DPAPI (anyone who can read the user's files can read the
// key) and exists so the Linux build works and the code path is tested; a Secret Service
// backend can replace it.
type keyFileProtector struct {
	keyPath string
}

const keyFileName = "secret.key"

// Native returns the key-file protector rooted at configDirectory.
func Native(configDirectory string) Protector {
	return keyFileProtector{keyPath: filepath.Join(configDirectory, keyFileName)}
}

func (protector keyFileProtector) cipher(create bool) (cipher.AEAD, error) {
	key, err := os.ReadFile(protector.keyPath)
	if errors.Is(err, fs.ErrNotExist) && create {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(protector.keyPath), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(protector.keyPath, key, 0o600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("%w: key file has the wrong length", ErrUnreadable)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (protector keyFileProtector) Protect(plain []byte) ([]byte, error) {
	aead, err := protector.cipher(true)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plain, entropy), nil
}

func (protector keyFileProtector) Unprotect(sealed []byte) ([]byte, error) {
	aead, err := protector.cipher(false)
	if err != nil {
		return nil, err
	}
	if len(sealed) < aead.NonceSize() {
		return nil, ErrUnreadable
	}
	nonce, body := sealed[:aead.NonceSize()], sealed[aead.NonceSize():]
	plain, err := aead.Open(nil, nonce, body, entropy)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	return plain, nil
}
