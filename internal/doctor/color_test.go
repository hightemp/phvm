package doctor

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/hightemp/phvm/internal/ui"
)

func TestReportColorDistinguishesStatusesAndPreservesRedactedData(t *testing.T) {
	result := &DoctorResult{
		PHPVersion: "8.3.30", Profile: "common", Errors: 1, Warnings: 1, Deferred: 1,
		Environment: "Build environment:\n  CC=/usr/bin/cc\n  linker selected by CC: /usr/bin/ld\n",
		Checks: []CheckResult{
			{Name: "cc", Required: true, Found: true, Version: "11.4", Path: "/usr/bin/cc"},
			{Name: "libcurl (lib)", Required: true, Problem: "request https://user:pass@example.invalid/?token=secret&version=8.3", Path: "/usr/lib/pkgconfig/libcurl.pc", HelpText: "check selected linker"},
			{Name: "gpg", HelpText: "optional GPG check"},
			{Name: "openssl (lib)", Required: true, Deferred: true, HelpText: "built privately"},
		},
	}
	plain := FormatResults(result)
	styled := FormatResultsWithPalette(result, ui.New(&bytes.Buffer{}, ui.Always, true))
	strip := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	if strip.ReplaceAllString(styled, "") != plain {
		t.Error("styling changed report data")
	}
	for _, accent := range []string{"\x1b[32m✓", "\x1b[1;31m✗", "\x1b[33m!", "\x1b[36m→", "\x1b[1;36mSystem Requirements Check"} {
		if !strings.Contains(styled, accent) {
			t.Errorf("report lacks semantic color %q", accent)
		}
	}
	if strings.Contains(styled, "user:pass") || strings.Contains(styled, "token=secret") || !strings.Contains(styled, "version=8.3") {
		t.Errorf("styling interfered with redaction: %q", styled)
	}
}
