// Package redact removes URL credentials from diagnostics and error chains.
package redact

import (
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
)

var urlPattern = regexp.MustCompile(`(?i)https?://[^\s"'<>\[\]()]+`)

func sensitiveKey(key string) bool {
	key, _ = url.QueryUnescape(key)
	key = strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
	for _, part := range []string{"token", "password", "passwd", "secret", "signature", "credential", "authorization"} {
		if strings.Contains(key, part) {
			return true
		}
	}
	return key == "key" || key == "apikey" || key == "auth" || key == "pwd" || key == "jwt"
}

func redactParams(params string) string {
	parts := strings.Split(params, "&")
	for i, part := range parts {
		pair := strings.SplitN(part, "=", 2)
		if len(pair) == 2 && sensitiveKey(pair[0]) {
			parts[i] = pair[0] + "=REDACTED"
		}
	}
	return strings.Join(parts, "&")
}

// URL removes userinfo and sensitive parameters, including malformed URLs.
func URL(value string) string {
	if offset := strings.Index(value, "://"); offset >= 0 {
		start := offset + 3
		end := len(value)
		if n := strings.IndexAny(value[start:], "/?#"); n >= 0 {
			end = start + n
		}
		if n := strings.LastIndex(value[start:end], "@"); n >= 0 {
			value = value[:start] + value[start+n+1:]
		}
	}
	if offset := strings.IndexByte(value, '#'); offset >= 0 {
		value = value[:offset+1] + redactParams(value[offset+1:])
	}
	if offset := strings.IndexByte(value, '?'); offset >= 0 {
		end := len(value)
		if n := strings.IndexByte(value[offset+1:], '#'); n >= 0 {
			end = offset + 1 + n
		}
		value = value[:offset+1] + redactParams(value[offset+1:end]) + value[end:]
	}
	return value
}

// Text redacts every HTTP(S) URL in a diagnostic string.
func Text(value string) string { return urlPattern.ReplaceAllStringFunc(value, URL) }

func secretsFromURL(value string) []string {
	var values []string
	add := func(raw string) {
		if raw != "" {
			values = append(values, raw)
			if decoded, err := url.QueryUnescape(raw); err == nil && decoded != raw {
				values = append(values, decoded)
			}
		}
	}
	if offset := strings.Index(value, "://"); offset >= 0 {
		start := offset + 3
		end := len(value)
		if n := strings.IndexAny(value[start:], "/?#"); n >= 0 {
			end = start + n
		}
		if n := strings.LastIndex(value[start:end], "@"); n >= 0 {
			for _, part := range strings.SplitN(value[start:start+n], ":", 2) {
				add(part)
			}
		}
	}
	for _, separator := range []byte{'?', '#'} {
		if offset := strings.IndexByte(value, separator); offset >= 0 {
			params := strings.SplitN(value[offset+1:], "#", 2)[0]
			for _, part := range strings.Split(params, "&") {
				pair := strings.SplitN(part, "=", 2)
				if len(pair) == 2 && sensitiveKey(pair[0]) {
					add(pair[1])
				}
			}
		}
	}
	return values
}

type safeError struct {
	message         string
	cause, original error
}

func (e *safeError) Error() string        { return e.message }
func (e *safeError) Unwrap() error        { return e.cause }
func (e *safeError) Is(target error) bool { return errors.Is(e.original, target) }

// Error redacts diagnostics and their causes while retaining errors.Is identity.
// URL and network error fields remain available through errors.As, sanitized.
func Error(err error, requestURL string) error {
	secrets := secretsFromURL(requestURL)
	clean := func(text string) string {
		text = Text(text)
		for _, secret := range secrets {
			text = strings.ReplaceAll(text, secret, "REDACTED")
		}
		return text
	}
	var sanitize func(error, int) error
	sanitize = func(err error, depth int) error {
		if err == nil {
			return nil
		}
		if depth > 64 {
			return &safeError{message: "error chain truncated", original: err}
		}
		switch cause := err.(type) {
		case *url.Error:
			return &url.Error{Op: clean(cause.Op), URL: clean(URL(cause.URL)), Err: sanitize(cause.Err, depth+1)}
		case *net.OpError:
			copy := *cause
			copy.Op = clean(copy.Op)
			copy.Err = sanitize(cause.Err, depth+1)
			return &copy
		default:
			return &safeError{message: clean(err.Error()), cause: sanitize(errors.Unwrap(err), depth+1), original: err}
		}
	}
	return sanitize(err, 0)
}
