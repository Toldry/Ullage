package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	sessionCookieName = "ullage_session"
	tokenHeaderName   = "X-Ullage-Token"
	launchTokenTTL    = 10 * time.Minute
)

// Guard enforces the localhost rules: exact Host and Origin, a session cookie obtained
// through a single-use launch link, and a header token on every API call.
type Guard struct {
	expectedHost   string
	expectedOrigin string
	sessionSecret  string
	apiToken       string

	lock         sync.Mutex
	launchTokens map[string]launchToken
	now          func() time.Time
}

type launchToken struct {
	expires time.Time
	usedAt  time.Time
}

func randomToken() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(bytes)
}

// NewGuard creates fresh secrets for a server listening on 127.0.0.1:port.
func NewGuard(port int) *Guard {
	host := "127.0.0.1:" + strconv.Itoa(port)
	return &Guard{
		expectedHost:   host,
		expectedOrigin: "http://" + host,
		sessionSecret:  randomToken(),
		apiToken:       randomToken(),
		launchTokens:   map[string]launchToken{},
		now:            time.Now,
	}
}

// NewLaunchURL mints a single-use link that the app opens in the browser.
func (guard *Guard) NewLaunchURL() string {
	token := randomToken()
	guard.lock.Lock()
	guard.launchTokens[token] = launchToken{expires: guard.now().Add(launchTokenTTL)}
	guard.lock.Unlock()
	return guard.expectedOrigin + "/launch?t=" + token
}

// launchResult explains why a launch link was refused, for the invalid-link page.
type launchResult struct {
	accepted bool
	reason   string
}

func (guard *Guard) redeemLaunchToken(token string) launchResult {
	guard.lock.Lock()
	defer guard.lock.Unlock()
	now := guard.now()
	for candidate, state := range guard.launchTokens {
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(token)) != 1 {
			continue
		}
		switch {
		case !state.usedAt.IsZero():
			return launchResult{reason: "already used at " + state.usedAt.Format("15:04:05")}
		case now.After(state.expires):
			return launchResult{reason: "expired"}
		}
		state.usedAt = now
		guard.launchTokens[candidate] = state
		return launchResult{accepted: true}
	}
	return launchResult{reason: "not issued by this Ullage window"}
}

// checkTransport rejects requests not addressed to our exact host and origin. This stops
// DNS rebinding (wrong Host) and cross-site requests (wrong Origin).
func (guard *Guard) checkTransport(request *http.Request) (int, string) {
	if request.Host != guard.expectedHost {
		return http.StatusMisdirectedRequest, "wrong host"
	}
	origin := request.Header.Get("Origin")
	if origin != "" && origin != guard.expectedOrigin {
		return http.StatusForbidden, "wrong origin"
	}
	if request.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return http.StatusForbidden, "cross-site request"
	}
	return 0, ""
}

func (guard *Guard) hasSessionCookie(request *http.Request) bool {
	cookie, err := request.Cookie(sessionCookieName)
	return err == nil &&
		subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(guard.sessionSecret)) == 1
}

func (guard *Guard) hasAPIToken(request *http.Request) bool {
	header := request.Header.Get(tokenHeaderName)
	return header != "" &&
		subtle.ConstantTimeCompare([]byte(header), []byte(guard.apiToken)) == 1
}

// sessionCookie is HttpOnly so page scripts cannot read it and SameSite=Strict so other
// sites cannot make the browser send it.
func (guard *Guard) sessionCookie() *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    guard.sessionSecret,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
}
