// Package client is the public Go client for the Clavis API. It is shared by
// the CLI, the Kubernetes operator, and the Terraform provider.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dumhouse/clavis/pkg/problem"
)

// DefaultUserAgent identifies the client to the server.
const DefaultUserAgent = "clavis-client/0.1"

// Client talks to one Clavis server.
type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	token      string
	userAgent  string
	maxRetries int
	sleep      func(context.Context, time.Duration) error
}

// Option configures a Client.
type Option func(*Client)

// WithToken sets the bearer token used for authentication.
func WithToken(token string) Option { return func(c *Client) { c.token = token } }

// WithHTTPClient replaces the default HTTP client.
func WithHTTPClient(hc *http.Client) Option { return func(c *Client) { c.httpClient = hc } }

// WithUserAgent sets the User-Agent header.
func WithUserAgent(ua string) Option { return func(c *Client) { c.userAgent = ua } }

// WithRetries sets how many times transient failures (429/5xx) are retried.
func WithRetries(n int) Option { return func(c *Client) { c.maxRetries = n } }

// New validates baseURL and returns a client.
func New(baseURL string, opts ...Option) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("client: invalid server URL %q", baseURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("client: unsupported URL scheme %q", u.Scheme)
	}
	c := &Client{
		baseURL:    u,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		userAgent:  DefaultUserAgent,
		maxRetries: 2,
		sleep:      sleepCtx,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// BaseURL returns the server URL the client targets.
func (c *Client) BaseURL() string { return c.baseURL.String() }

// Do performs a request. in is JSON-encoded when non-nil; out is decoded when
// non-nil. It returns a *problem.Problem for non-2xx responses.
func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	var body []byte
	if in != nil {
		var err error
		body, err = json.Marshal(in)
		if err != nil {
			return fmt.Errorf("client: encode request: %w", err)
		}
	}

	target, err := c.resolve(path)
	if err != nil {
		return err
	}

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("client: build request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.userAgent)
		if in != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if attempt < c.maxRetries && isIdempotent(method) && ctx.Err() == nil {
				// Transport errors are usually transient (connection reset).
				if err := c.sleep(ctx, backoff(attempt)); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("client: %s %s: %w", method, target, err)
		}

		retry := attempt < c.maxRetries && isIdempotent(method) && shouldRetry(resp)
		if retry {
			delay := retryAfter(resp, backoff(attempt))
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
			_ = resp.Body.Close()
			if err := c.sleep(ctx, delay); err != nil {
				return err
			}
			continue
		}

		err = decodeResponse(resp, out)
		_ = resp.Body.Close()
		return err
	}
}

// Get performs a GET request.
func (c *Client) Get(ctx context.Context, path string, out any) error {
	return c.Do(ctx, http.MethodGet, path, nil, out)
}

// Post performs a POST request.
func (c *Client) Post(ctx context.Context, path string, in, out any) error {
	return c.Do(ctx, http.MethodPost, path, in, out)
}

// Put performs a PUT request.
func (c *Client) Put(ctx context.Context, path string, in, out any) error {
	return c.Do(ctx, http.MethodPut, path, in, out)
}

// Patch performs a PATCH request.
func (c *Client) Patch(ctx context.Context, path string, in, out any) error {
	return c.Do(ctx, http.MethodPatch, path, in, out)
}

// Delete performs a DELETE request.
func (c *Client) Delete(ctx context.Context, path string) error {
	return c.Do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) resolve(p string) (string, error) {
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return "", errors.New("client: absolute URLs are not allowed; pass an API path")
	}
	ref, err := url.Parse(p)
	if err != nil {
		return "", fmt.Errorf("client: invalid path %q: %w", p, err)
	}
	return c.baseURL.ResolveReference(ref).String(), nil
}

func decodeResponse(resp *http.Response, out any) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil || resp.StatusCode == http.StatusNoContent {
			return nil
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out); err != nil {
			return fmt.Errorf("client: decode response: %w", err)
		}
		return nil
	}

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var p problem.Problem
	if err := json.Unmarshal(raw, &p); err != nil || p.Status == 0 {
		p = problem.Problem{
			Type:   "about:blank",
			Title:  http.StatusText(resp.StatusCode),
			Status: resp.StatusCode,
			Code:   "http_error",
			Detail: strings.TrimSpace(string(raw)),
		}
	}
	return &p
}

func shouldRetry(resp *http.Response) bool {
	return resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
}

// isIdempotent guards retries. POST is only retried when an Idempotency-Key is
// added (M1); until then a retried create could duplicate a resource.
func isIdempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions:
		return true
	default:
		return false
	}
}

func retryAfter(resp *http.Response, fallback time.Duration) time.Duration {
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return fallback
	}
	if seconds, err := strconv.Atoi(v); err == nil && seconds >= 0 && seconds <= 60 {
		return time.Duration(seconds) * time.Second
	}
	return fallback
}

func backoff(attempt int) time.Duration {
	d := time.Duration(1<<attempt) * 100 * time.Millisecond
	if d > 2*time.Second {
		d = 2 * time.Second
	}
	return d
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
