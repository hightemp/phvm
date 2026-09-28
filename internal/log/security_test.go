package log

import (
	"bytes"
	"strings"
	"testing"
)

func TestLoggerRedactsURLSecrets(t *testing.T) {
	var out bytes.Buffer
	l := New(&out, LevelDebug)
	l.SetNoColor(true)
	for _, tt := range []struct {
		name  string
		write func(string, ...interface{})
	}{
		{"error", l.Error}, {"warning", l.Warn}, {"debug", l.Debug}, {"print", l.Print},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out.Reset()
			tt.write("request %s", "https://phvm-user:phvm-password@example.invalid/bad%zz?api_key=phvm-secret&version=8.3")
			for _, secret := range []string{"phvm-user", "phvm-password", "phvm-secret"} {
				if strings.Contains(out.String(), secret) {
					t.Errorf("log exposes %s: %s", secret, out.String())
				}
			}
			if !strings.Contains(out.String(), "version=8.3") {
				t.Error("public query parameter was lost")
			}
		})
	}
}
