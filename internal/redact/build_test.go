package redact

import (
	"strings"
	"testing"
)

func TestBuildDiagnosticsRedactLabelsURLsAndEnvironmentValues(t *testing.T) {
	input := "TOKEN=split-secret-value\nAuthorization: Bearer bearer-secret-value\n" +
		"-DAPI_PASSWORD='quoted secret value'\nKEY=key-secret\nSIGNATURE=sig-secret\nmonkey=bananas\nhttps://user:pass@example.invalid/file?token=query-secret-value\n" +
		"environment-secret-value\n"
	secrets := EnvironmentSecrets([]string{"PHVM_BUILD_SECRET=environment-secret-value"})
	clean := WithSecrets(input, secrets)
	for _, secret := range []string{"split-secret-value", "bearer-secret-value", "quoted secret value", "key-secret", "sig-secret", "user:pass", "query-secret-value", "environment-secret-value"} {
		if strings.Contains(clean, secret) {
			t.Errorf("diagnostic disclosed %q: %s", secret, clean)
		}
	}
	if strings.Count(clean, "REDACTED") < 7 || !strings.Contains(clean, "example.invalid") || !strings.Contains(clean, "monkey=bananas") {
		t.Errorf("nonsecret diagnostic data lost or not redacted: %s", clean)
	}
}
