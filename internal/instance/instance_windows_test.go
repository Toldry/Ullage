//go:build windows

package instance

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestSecondClaimAsksTheFirst(t *testing.T) {
	name := fmt.Sprintf("UllageTest-%d", time.Now().UnixNano())
	channel, err := Claim(name)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	go channel.Serve(func() string {
		calls++
		return fmt.Sprintf("http://127.0.0.1:1234/launch?t=%d", calls)
	})
	if _, err := Claim(name); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second claim: %v", err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		url, err := RequestLaunch(name)
		if err != nil || url != fmt.Sprintf("http://127.0.0.1:1234/launch?t=%d", attempt) {
			t.Fatalf("request %d: %q %v", attempt, url, err)
		}
	}
}

func TestRequestWithoutInstanceFails(t *testing.T) {
	if _, err := RequestLaunch(fmt.Sprintf("UllageNone-%d", time.Now().UnixNano())); err == nil {
		t.Fatal("request succeeded without a running instance")
	}
}
