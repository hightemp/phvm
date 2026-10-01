package ui

import (
	"bytes"
	"regexp"
	"testing"
)

func TestAutomaticColorsRespectStreamConfigAndEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		mode                 Mode
		configured, terminal bool
		noColor, dumb, want  bool
	}{
		{"terminal", Auto, true, true, false, false, true},
		{"pipe", Auto, true, false, false, false, false},
		{"config disabled", Auto, false, true, false, false, false},
		{"NO_COLOR", Auto, true, true, true, false, false},
		{"dumb terminal", Auto, true, true, false, true, false},
		{"explicit always", Always, false, false, true, true, true},
		{"explicit never", Never, true, true, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := paletteFor(tc.mode, tc.configured, tc.terminal, tc.noColor, tc.dumb)
			if p.Enabled() != tc.want {
				t.Errorf("color enabled=%v want=%v", p.Enabled(), tc.want)
			}
		})
	}
}

func TestStylesPreservePlainTextAndCanOverrideLibraryDefaults(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	plain := New(&bytes.Buffer{}, Auto, true)
	forced := New(&bytes.Buffer{}, Always, true)
	strip := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	for _, tone := range []Tone{Heading, Name, Value, Success, Warning, Error, Info, Muted} {
		text := "PHP 8.3.30 — libcurl"
		if plain.Text(tone, text) != text {
			t.Error("plain palette changed text")
		}
		styled := forced.Text(tone, text)
		if styled == text || strip.ReplaceAllString(styled, "") != text {
			t.Errorf("style changed data or ignored explicit color: %q", styled)
		}
	}
}
