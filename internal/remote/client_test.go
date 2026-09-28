package remote

import (
	"context"
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
			if !strings.Contains(err.Error(), "phvm-test-user:***") {
				t.Errorf("HTTP error should retain the URL with a redacted password: %v", err)
			}
		})
	}
}
