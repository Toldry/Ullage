package instance

import "testing"

func TestValidLaunchURL(t *testing.T) {
	for text, want := range map[string]bool{
		"http://127.0.0.1:4000/launch?t=ab": true,
		"https://evil.example/":             false,
		"http://127.0.0.1:4000/ x":          false,
		"http://127.0.0.1.evil/":            false,
	} {
		if validLaunchURL(text) != want {
			t.Errorf("validLaunchURL(%q) != %v", text, want)
		}
	}
}
