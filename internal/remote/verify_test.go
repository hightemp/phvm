package remote

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testFingerprint = "0123456789ABCDEF0123456789ABCDEF01234567"

func TestMain(m *testing.M) {
	if os.Getenv("PHVM_TEST_GPG") == "1" {
		args := strings.Join(os.Args[1:], " ")
		if record := os.Getenv("PHVM_TEST_GPG_RECORD"); record != "" {
			f, err := os.OpenFile(record, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				os.Exit(3)
			}
			_, _ = f.WriteString(args + "\n")
			_ = f.Close()
		}
		if strings.Contains(args, "--import") {
			os.Exit(0)
		}
		switch os.Getenv("PHVM_TEST_GPG_RESULT") {
		case "valid":
			_, _ = os.Stdout.WriteString("[GNUPG:] GOODSIG 89ABCDEF Test Release Key\n[GNUPG:] VALIDSIG " + testFingerprint + " 2026-09-28 0 0 4 0 1 10 00 " + testFingerprint + "\n")
		case "bad":
			_, _ = os.Stdout.WriteString("[GNUPG:] BADSIG 89ABCDEF Test Release Key\n")
			os.Exit(1)
		case "unknown":
			_, _ = os.Stdout.WriteString("[GNUPG:] ERRSIG 89ABCDEF 1 10 00 0 9\n[GNUPG:] NO_PUBKEY 89ABCDEF\n")
			os.Exit(2)
		case "no-status":
			_, _ = os.Stdout.WriteString("gpg: something succeeded\n")
		case "stderr-status":
			_, _ = os.Stderr.WriteString("[GNUPG:] VALIDSIG " + testFingerprint + "\n")
		case "revoked":
			_, _ = os.Stdout.WriteString("[GNUPG:] KEYREVOKED\n[GNUPG:] VALIDSIG " + testFingerprint + "\n")
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestVerifyRealGPG(t *testing.T) {
	gpg, err := exec.LookPath("gpg")
	if err != nil {
		t.Skip("GPG unavailable")
	}
	homes := []string{t.TempDir(), t.TempDir()}
	run := func(home string, args ...string) []byte {
		t.Helper()
		flags := append([]string{"--batch", "--no-tty", "--no-options", "--homedir", home}, args...)
		out, err := exec.Command(gpg, flags...).CombinedOutput()
		if err != nil {
			if runtime.GOOS == "windows" && (strings.Contains(string(out), "No agent running") || strings.Contains(string(out), "can't connect to the gpg-agent")) {
				t.Skipf("native GPG agent is unavailable on this runner: %v", err)
			}
			t.Fatalf("fixture GPG: %v\n%s", err, out)
		}
		return out
	}
	for i, home := range homes {
		run(home, "--pinentry-mode", "loopback", "--passphrase", "", "--quick-generate-key", fmt.Sprintf("phvm-test-%d <test%d@example.invalid>", i, i), "ed25519", "sign", "0")
		home := home
		t.Cleanup(func() {
			if cmd, err := exec.LookPath("gpgconf"); err == nil {
				_ = exec.Command(cmd, "--homedir", home, "--kill", "gpg-agent").Run()
			}
		})
	}
	trusted := run(homes[0], "--armor", "--export")
	dir := t.TempDir()
	payload := filepath.Join(dir, "payload")
	asc := payload + ".asc"
	if err := os.WriteFile(payload, []byte("signed payload"), 0600); err != nil {
		t.Fatal(err)
	}
	client := NewClient(DefaultClientOptions())
	client.httpClient.HTTPClient.Transport = verificationTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != PHPKeyringURL {
			t.Errorf("unexpected trust URL: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(trusted))), Header: make(http.Header), ContentLength: -1}, nil
	})
	v := NewVerifier(client, dir)
	v.SetGPGFallback(true)
	run(homes[0], "--armor", "--detach-sign", "--output", asc, payload)
	sum, err := v.ComputeSHA256(payload)
	if err != nil {
		t.Fatal(err)
	}
	result, err := v.Verify(context.Background(), payload, sum, asc, PHPKeyringURL)
	if err != nil || !result.GPGVerified || result.GPGFingerprint == "" {
		t.Fatalf("trusted signature: %+v %v", result, err)
	}
	if err := os.Remove(asc); err != nil {
		t.Fatal(err)
	}
	run(homes[1], "--armor", "--detach-sign", "--output", asc, payload)
	t.Setenv("GNUPGHOME", homes[1])
	result, err = v.Verify(context.Background(), payload, sum, asc, PHPKeyringURL)
	if err == nil || result.GPGVerified || result.GPGSkipped {
		t.Errorf("personal key bypassed trust anchor: %+v %v", result, err)
	}
}

type verificationTransport func(*http.Request) (*http.Response, error)

func (f verificationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func prepareGPG(t *testing.T, dir, outcome string) string {
	t.Helper()
	bin := filepath.Join(dir, "tools")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	name := "gpg"
	if filepath.Ext(source) == ".exe" {
		name += ".exe"
	}
	out, err := os.OpenFile(filepath.Join(bin, name), os.O_CREATE|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PHVM_TEST_GPG", "1")
	t.Setenv("PHVM_TEST_GPG_RESULT", outcome)
	record := filepath.Join(dir, "gpg-commands")
	t.Setenv("PHVM_TEST_GPG_RECORD", record)
	return record
}

func TestVerifyGPGStates(t *testing.T) {
	for _, tt := range []struct {
		name, outcome                                                               string
		enabled, fallback, signature, available, wantErr, wantVerified, wantSkipped bool
	}{
		{"valid", "valid", true, true, true, true, false, true, false},
		{"bad signature never falls back", "bad", true, true, true, true, true, false, false},
		{"unknown signing key", "unknown", true, true, true, true, true, false, false},
		{"missing status", "no-status", true, true, true, true, true, false, false},
		{"status must come from stdout", "stderr-status", true, true, true, true, true, false, false},
		{"revoked signer", "revoked", true, true, true, true, true, false, false},
		{"strict missing signature", "valid", true, false, false, true, true, false, false},
		{"optional missing signature", "valid", true, true, false, true, false, false, true},
		{"strict missing gpg", "valid", true, false, true, false, true, false, false},
		{"optional missing gpg", "valid", true, true, true, false, false, false, true},
		{"disabled", "bad", false, false, true, true, false, false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			record := prepareGPG(t, dir, tt.outcome)
			if !tt.available {
				t.Setenv("PATH", filepath.Join(dir, "empty"))
			}
			payload := filepath.Join(dir, "php.tar.xz")
			if err := os.WriteFile(payload, []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			asc := payload + ".asc"
			if tt.signature {
				if err := os.WriteFile(asc, []byte("signature"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "php-keyring.gpg"), []byte("fixture keyring"), 0600); err != nil {
				t.Fatal(err)
			}
			client := NewClient(DefaultClientOptions())
			client.httpClient.HTTPClient.Transport = verificationTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("fixture keyring")), Header: make(http.Header), ContentLength: -1}, nil
			})
			v := NewVerifier(client, dir)
			v.SetGPGEnabled(tt.enabled)
			v.SetGPGFallback(tt.fallback)
			sum, err := v.ComputeSHA256(payload)
			if err != nil {
				t.Fatal(err)
			}
			result, err := v.Verify(context.Background(), payload, sum, asc, NewPHPNetAPI(client).KeyringURL())
			if (err != nil) != tt.wantErr {
				t.Errorf("error=%v, wantErr=%v", err, tt.wantErr)
			}
			if result.GPGVerified != tt.wantVerified || result.GPGSkipped != tt.wantSkipped {
				t.Errorf("result=%+v, want verified=%v skipped=%v", result, tt.wantVerified, tt.wantSkipped)
			}
			if tt.wantVerified {
				commands, err := os.ReadFile(record)
				if err != nil {
					t.Fatal(err)
				}
				for _, line := range strings.Split(strings.TrimSpace(string(commands)), "\n") {
					if !strings.Contains(line, "--homedir") {
						t.Errorf("GPG used user's home: %s", line)
					}
				}
			}
		})
	}
}

func TestPHPKeyringDoesNotFollowMirror(t *testing.T) {
	opts := DefaultClientOptions()
	opts.Mirror = "https://mirror.example.invalid"
	got := NewPHPNetAPI(NewClient(opts)).KeyringURL()
	if got != "https://www.php.net/distributions/php-keyring.gpg" {
		t.Errorf("trust anchor follows mirror: %s", got)
	}
}
