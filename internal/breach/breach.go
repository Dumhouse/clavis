// Package breach checks passwords against the Have I Been Pwned range API
// using k-anonymity: only the first five SHA-1 characters leave the server.
package breach

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Checker queries the Pwned Passwords range API.
type Checker struct {
	BaseURL string
	Client  *http.Client
}

// New returns a checker. An empty base URL uses the public HIBP endpoint.
func New(baseURL string, client *http.Client) *Checker {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.pwnedpasswords.com/range/"
	}
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &Checker{BaseURL: baseURL, Client: client}
}

// Breached reports whether the password appears in the breach corpus.
func (c *Checker) Breached(ctx context.Context, password string) (bool, error) {
	sum := sha1.Sum([]byte(password))
	hash := strings.ToUpper(hex.EncodeToString(sum[:]))
	prefix, suffix := hash[:5], hash[5:]
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+prefix, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Add-Padding", "true")
	req.Header.Set("User-Agent", "clavis-password-check")
	resp, err := c.Client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("breach: range API returned %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 4<<20))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.EqualFold(parts[0], suffix) {
			count := strings.TrimSpace(parts[1])
			if count == "0" {
				return false, nil
			}
			return true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return false, err
	}
	return false, nil
}
