// Package nonetwork disables Go's outbound networking defaults at runtime. It is one of
// several layers enforcing "no outbound connections"; see ARCHITECTURE.md §6.
package nonetwork

import (
	"context"
	"errors"
	"net"
	"net/http"
)

// ErrOutboundBlocked is returned by every attempt to dial out.
var ErrOutboundBlocked = errors.New("ullage never makes outbound network connections")

type refusingTransport struct{}

func (refusingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, ErrOutboundBlocked
}

// Install replaces the default HTTP transport and DNS resolver with ones that refuse.
// Call it first thing in main.
func Install() {
	http.DefaultTransport = refusingTransport{}
	http.DefaultClient = &http.Client{Transport: refusingTransport{}}
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return nil, ErrOutboundBlocked
		},
	}
}
