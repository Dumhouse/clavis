package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ReleasesURL is the public "latest release" endpoint.
const ReleasesURL = "https://api.github.com/repos/Dumhouse/clavis/releases/latest"

// Interval is how often a copy of Clavis looks for a new release.
const Interval = 24 * time.Hour

// Latest asks the releases endpoint for the newest published version, without
// a leading "v". It sends nothing about the installation.
func Latest(ctx context.Context, client *http.Client, url string) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "clavis-update-check")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("releases: %s", resp.Status)
	}
	var body struct {
		Tag        string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", err
	}
	version := strings.TrimPrefix(body.Tag, "v")
	if _, ok := parse(version); !ok || body.Prerelease || body.Draft {
		return "", errors.New("releases: no usable version")
	}
	return version, nil
}

// parse reads "1.2.3" (a "v" prefix and any "+build" or "-pre" suffix are
// ignored) into numbers.
func parse(version string) ([3]int, bool) {
	version = strings.TrimPrefix(version, "v")
	if i := strings.IndexAny(version, "+-"); i >= 0 {
		version = version[:i]
	}
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var out [3]int
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

// Newer reports whether latest is a higher release than current. Versions that
// aren't numbers ("dev") are never reported as older.
func Newer(latest, current string) bool {
	l, ok := parse(latest)
	if !ok {
		return false
	}
	c, ok := parse(current)
	if !ok {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// Drifted reports whether two versions differ in major or minor, the point
// where a CLI and a server may stop understanding each other.
func Drifted(a, b string) bool {
	x, ok := parse(a)
	if !ok {
		return false
	}
	y, ok := parse(b)
	if !ok {
		return false
	}
	return x[0] != y[0] || x[1] != y[1]
}
