package remote

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestClientErrorsRedactURLPassword(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	server.Close()
	const password = "phvm-test-password"
	endpoint.User = url.UserPassword("phvm-test-user", password)

	opts := DefaultClientOptions()
	opts.Retries = 0
	opts.Timeout = time.Second
	client := NewClient(opts)
	for _, tt := range []struct {
		name    string
		request func(context.Context, string) (*http.Response, error)
	}{
		{name: "GET", request: client.Get},
		{name: "JSON", request: client.GetJSON},
		{name: "HEAD", request: client.Head},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.request(context.Background(), endpoint.String())
			if err == nil {
				t.Fatal("request to closed server should fail")
			}
			if strings.Contains(err.Error(), password) {
				t.Errorf("HTTP error exposes URL password: %v", err)
			}
			if strings.Contains(err.Error(), "phvm-test-user") {
				t.Errorf("HTTP error should remove URL userinfo: %v", err)
			}
		})
	}
}

func TestClientErrorsRedactSensitiveQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	base := server.URL
	server.Close()
	opts := DefaultClientOptions()
	opts.Retries = 0
	client := NewClient(opts)
	for _, suffix := range []string{"/?token=phvm-query-secret&version=8.3", "/bad%zz?api_key=phvm-query-secret", "/?X-Amz-Signature=phvm-query-secret"} {
		t.Run(suffix, func(t *testing.T) {
			_, err := client.Get(context.Background(), base+suffix)
			if err == nil {
				t.Fatal("request should fail")
			}
			for cause := err; cause != nil; cause = errors.Unwrap(cause) {
				if strings.Contains(cause.Error(), "phvm-query-secret") {
					t.Errorf("error chain exposes query secret: %v", cause)
				}
			}
		})
	}
}

func TestClientRedactsTransportErrorCause(t *testing.T) {
	sentinel := errors.New("transport failure")
	client := NewClient(DefaultClientOptions())
	client.httpClient.RetryMax = 0
	client.httpClient.HTTPClient.Transport = verificationTransport(func(r *http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: "transport", URL: r.URL.String(), Err: fmt.Errorf("token phvm-private-token: %w", sentinel)}
	})
	_, err := client.Get(context.Background(), "https://user:phvm-private-password@example.invalid/?token=phvm-private-token")
	if err == nil {
		t.Fatal("expected failure")
	}
	if !errors.Is(err, sentinel) {
		t.Error("transport cause identity lost")
	}
	for cause := err; cause != nil; cause = errors.Unwrap(cause) {
		for _, secret := range []string{"phvm-private-password", "phvm-private-token"} {
			if strings.Contains(cause.Error(), secret) {
				t.Errorf("cause exposes %s: %v", secret, cause)
			}
		}
	}
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		t.Error("URL error type lost")
	} else if strings.Contains(urlErr.URL, "phvm-private") {
		t.Errorf("URL error field exposes secret: %s", urlErr.URL)
	}
}
