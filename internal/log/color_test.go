package log

import (
	"bytes"
	"strings"
	"testing"

	"github.com/hightemp/phvm/internal/ui"
)

func TestLoggerDoesNotColorRedirectedOutput(t *testing.T) {
	var out bytes.Buffer
	l := New(&out, LevelNormal)
	l.SetNoColor(false)
	l.Success("PHP installed")
	l.Warn("check dependencies")
	l.PrintVersions([]string{"8.3.30"}, "8.3.30", "8.3.30")
	if strings.Contains(out.String(), "\x1b[") {
		t.Errorf("redirected output contains terminal color codes: %q", out.String())
	}
	for _, text := range []string{"✓ PHP installed", "! check dependencies", "* 8.3.30 (current, default)"} {
		if !strings.Contains(out.String(), text) {
			t.Errorf("plain output lost %q: %q", text, out.String())
		}
	}
}

func TestIndependentLoggersDoNotChangeEachOthersColors(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var colored, plain bytes.Buffer
	first := New(&colored, LevelNormal)
	first.SetColorMode(ui.Always)
	second := New(&plain, LevelNormal)
	second.SetNoColor(true)
	first.Success("first logger")
	first.Error("request https://user:pass@example.invalid/?token=secret&version=8.3")
	second.Success("second logger")
	if !strings.Contains(colored.String(), "\x1b[") || strings.Contains(plain.String(), "\x1b[") {
		t.Errorf("loggers share color state: colored=%q plain=%q", colored.String(), plain.String())
	}
	if strings.Contains(colored.String(), "user:pass") || strings.Contains(colored.String(), "token=secret") || !strings.Contains(colored.String(), "version=8.3") {
		t.Errorf("colored diagnostics broke redaction: %q", colored.String())
	}
}

func TestProgressDoesNotAddColorCodesToRedirectedOutput(t *testing.T) {
	previous := Default()
	t.Cleanup(func() { SetDefault(previous) })
	var out bytes.Buffer
	SetDefault(New(&out, LevelNormal))
	bar := NewProgressBar(ProgressOptions{Writer: &out, Total: 100, Description: "download"})
	if err := bar.Add(50); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b[") || strings.Contains(out.String(), "[green]") {
		t.Errorf("redirected progress has terminal codes or style tags: %q", out.String())
	}
}
