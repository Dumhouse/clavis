package server

import "testing"

func TestPlainHTTPBaseURL(t *testing.T) {
	cases := map[string]bool{
		"http://192.168.100.234":     true,
		"http://clavis.lan:8080":     true,
		"https://clavis.example.com": false,
		"http://localhost:8080":      false,
		"http://127.0.0.1:8080":      false,
		"http://[::1]:8080":          false,
		"http://app.localhost":       false,
		"":                           false,
		"not a url":                  false,
	}
	for in, want := range cases {
		if got := PlainHTTPBaseURL(in); got != want {
			t.Errorf("PlainHTTPBaseURL(%q) = %v, want %v", in, got, want)
		}
	}
}
