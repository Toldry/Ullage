package protect

import (
	"bytes"
	"errors"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	protector := Native(t.TempDir())
	secret := []byte(`{"rules":["~/clients/**"]}`)
	sealed, err := protector.Protect(secret)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("clients")) {
		t.Fatalf("sealed data contains plaintext")
	}
	opened, err := protector.Unprotect(sealed)
	if err != nil || !bytes.Equal(opened, secret) {
		t.Fatalf("round trip failed: %q %v", opened, err)
	}
}

func TestTamperedOrEmptyDataFailsClosed(t *testing.T) {
	protector := Native(t.TempDir())
	sealed, err := protector.Protect([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	sealed[len(sealed)-1] ^= 0xFF
	if _, err := protector.Unprotect(sealed); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("tampered data: %v", err)
	}
	for _, garbage := range [][]byte{nil, []byte("not sealed at all")} {
		if _, err := protector.Unprotect(garbage); !errors.Is(err, ErrUnreadable) {
			t.Fatalf("garbage %q: %v", garbage, err)
		}
	}
}
