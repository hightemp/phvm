// Package remote provides HTTP client and remote API access for phvm.
package remote

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hashicorp/go-retryablehttp"
)

// Client is an HTTP client with retry support.
type Client struct {
	httpClient *retryablehttp.Client
	userAgent  string
	mirror     string
}

// ClientOptions holds client configuration.
type ClientOptions struct {
	UserAgent string
	Timeout   time.Duration
	Retries   int
	Mirror    string
}

// DefaultClientOptions returns default client options.
func DefaultClientOptions() ClientOptions {
	return ClientOptions{
		UserAgent: "phvm/1.0.0",
		Timeout:   60 * time.Second,
		Retries:   3,
		Mirror:    "https://www.php.net",
	}
}

// NewClient creates a new Client.
func NewClient(opts ClientOptions) *Client {
	retryClient := retryablehttp.NewClient()
	retryClient.RetryMax = opts.Retries
	retryClient.RetryWaitMin = 1 * time.Second
	retryClient.RetryWaitMax = 30 * time.Second
	retryClient.HTTPClient.Timeout = opts.Timeout
	retryClient.Logger = nil // Disable default logging

	// Custom retry policy that retries on 503 (WAF) and network errors
	retryClient.CheckRetry = func(ctx context.Context, resp *http.Response, err error) (bool, error) {
		if err != nil {
			return true, nil
		}
		if resp.StatusCode == 503 || resp.StatusCode == 429 {
			return true, nil
		}
		return retryablehttp.DefaultRetryPolicy(ctx, resp, err)
	}

	// Use default exponential backoff (LinearJitterBackoff provides good behavior)
	retryClient.Backoff = retryablehttp.LinearJitterBackoff

	return &Client{
		httpClient: retryClient,
		userAgent:  opts.UserAgent,
		mirror:     opts.Mirror,
	}
}

// Get performs a GET request.
func (c *Client) Get(ctx context.Context, url string) (*http.Response, error) {
	req, err := retryablehttp.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "*/*")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}

	return resp, nil
}

// GetJSON performs a GET request expecting JSON response.
func (c *Client) GetJSON(ctx context.Context, url string) (*http.Response, error) {
	req, err := retryablehttp.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}

	return resp, nil
}

// Download downloads a file and returns a reader with the content length.
func (c *Client) Download(ctx context.Context, url string) (io.ReadCloser, int64, error) {
	resp, err := c.Get(ctx, url)
	if err != nil {
		return nil, 0, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	return resp.Body, resp.ContentLength, nil
}

// Head performs a HEAD request.
func (c *Client) Head(ctx context.Context, url string) (*http.Response, error) {
	req, err := retryablehttp.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}

	return resp, nil
}

// Exists checks if a URL exists (returns 200).
func (c *Client) Exists(ctx context.Context, url string) bool {
	resp, err := c.Head(ctx, url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Mirror returns the configured mirror URL.
func (c *Client) Mirror() string {
	return c.mirror
}

// SetMirror sets the mirror URL.
func (c *Client) SetMirror(mirror string) {
	c.mirror = mirror
}

// DefaultClient returns a client with default options.
var DefaultClient = NewClient(DefaultClientOptions())
