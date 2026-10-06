package nonetwork

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
)

func TestInstallBlocksDefaultClientAndResolver(t *testing.T) {
	Install()
	if _, err := http.Get("http://example.com/"); !errors.Is(err, ErrOutboundBlocked) {
		t.Fatalf("default client was not blocked: %v", err)
	}
	if _, err := net.DefaultResolver.LookupHost(context.Background(),
		"example.com"); err == nil {
		t.Fatalf("resolver was not blocked")
	}
}
