package service

import "testing"

func TestSafeReturnPath(t *testing.T) {
	cases := map[string]string{
		"":                  "/",
		"/":                 "/",
		"/orgs/acme":        "/orgs/acme",
		"/a?b=c#d":          "/a?b=c#d",
		"https://evil.test": "/",
		"//evil.test":       "/",
		`/\evil.test`:       "/",
		"relative":          "/",
	}
	for in, want := range cases {
		if got := SafeReturnPath(in); got != want {
			t.Errorf("SafeReturnPath(%q) = %q, want %q", in, got, want)
		}
	}
}
