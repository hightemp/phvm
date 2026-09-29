package build

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/fsutil"
	"github.com/hightemp/phvm/internal/redact"
	"github.com/hightemp/phvm/internal/toolchain"
)

type phpIdentity struct {
	API        string
	ZTS, Debug bool
}

var moduleHeaderAPI = regexp.MustCompile(`(?m)^\s*#\s*define\s+ZEND_MODULE_API_NO\s+([0-9]+)`)
var runtimePHPAPI = regexp.MustCompile(`(?m)^PHP API\s*=>\s*([0-9]+)`)

const identityCode = `echo PHP_VERSION, "\n", PHP_PREFIX, "\n", PHP_EXTENSION_DIR, "\n", (PHP_ZTS ? "1" : "0"), "\n", (PHP_DEBUG ? "1" : "0"), "\n", PHP_SAPI, "\n";`

func probePHP(ctx context.Context, env toolchain.Environment, binary string, args ...string) ([]byte, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, binary, args...)
	cmd.Env = []string(env)
	cmd.WaitDelay = time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if probeCtx.Err() != nil {
		return nil, probeCtx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("PHP probe: %w: %s", redact.Error(err, ""), redact.Text(stderr.String()))
	}
	if stderr.Len() != 0 || bytes.Contains(output, []byte("Warning:")) || bytes.Contains(output, []byte("Fatal error:")) {
		return nil, fmt.Errorf("PHP startup diagnostics: %s", redact.Text(stderr.String()+string(output)))
	}
	return output, nil
}

func validatePHPInstallation(ctx context.Context, candidate, final, version string) (*phpIdentity, error) {
	root, err := os.OpenRoot(candidate)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for _, name := range []string{filepath.Join("bin", core.PHPBinary()), "bin/php-config", "bin/phpize", "etc/php.ini", "include/php/Zend/zend_modules.h"} {
		info, err := root.Lstat(filepath.FromSlash(name))
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("candidate file must be regular: %s", name)
		}
	}
	php := filepath.Join(candidate, "bin", core.PHPBinary())
	env := toolchain.Current("PHPRC=", "PHP_INI_SCAN_DIR=")
	output, err := probePHP(ctx, env, php, "-n", "-r", identityCode)
	if err != nil {
		return nil, err
	}
	fields := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(fields) != 6 || fields[0] != version || fields[1] != final || fields[5] != "cli" {
		return nil, fmt.Errorf("PHP identity/version/prefix does not match requested installation: %s", redact.Text(string(output)))
	}
	if fields[3] != "0" && fields[3] != "1" || fields[4] != "0" && fields[4] != "1" {
		return nil, fmt.Errorf("invalid PHP ABI flags")
	}
	extRel, err := filepath.Rel(final, fields[2])
	if err != nil || !filepath.IsLocal(extRel) || extRel == "." {
		return nil, fmt.Errorf("PHP extension directory is outside final prefix")
	}
	config := filepath.Join(candidate, "bin", "php-config")
	for _, item := range []struct{ flag, want string }{{"--prefix", final}, {"--version", version}, {"--extension-dir", fields[2]}} {
		output, err := probePHP(ctx, env, config, item.flag)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(string(output)) != item.want {
			return nil, fmt.Errorf("php-config %s does not match PHP runtime", item.flag)
		}
	}
	header, err := root.ReadFile(filepath.Join("include", "php", "Zend", "zend_modules.h"))
	if err != nil {
		return nil, err
	}
	api := moduleHeaderAPI.FindSubmatch(header)
	if len(api) != 2 {
		return nil, fmt.Errorf("PHP module API missing from installed headers")
	}
	output, err = probePHP(ctx, env, php, "-n", "-i")
	if err != nil {
		return nil, err
	}
	runtimeAPI := runtimePHPAPI.FindSubmatch(output)
	if len(runtimeAPI) != 2 || !bytes.Equal(api[1], runtimeAPI[1]) {
		return nil, fmt.Errorf("PHP runtime/header module ABI mismatch")
	}
	probeDir, err := os.MkdirTemp("", "phvm-php-probe-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(probeDir)
	probeRoot, err := os.OpenRoot(probeDir)
	if err != nil {
		return nil, err
	}
	defer probeRoot.Close()
	etc, err := root.OpenRoot("etc")
	if err != nil {
		return nil, err
	}
	defer etc.Close()
	if err := fsutil.CopyRootTree(etc, probeRoot); err != nil {
		return nil, err
	}
	if err := rewriteProbePaths(probeRoot, ".", final, candidate); err != nil {
		return nil, err
	}
	env = toolchain.Current("PHPRC="+filepath.Join(probeDir, "php.ini"), "PHP_INI_SCAN_DIR="+filepath.Join(probeDir, "conf.d"))
	output, err = probePHP(ctx, env, php, "-c", filepath.Join(probeDir, "php.ini"), "-d", "extension_dir="+filepath.Join(candidate, extRel), "-d", "display_startup_errors=1", "-d", "display_errors=stderr", "-d", "log_errors=0", "-r", "echo PHP_VERSION;")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(output)) != version {
		return nil, fmt.Errorf("configured PHP runtime returned unexpected version")
	}
	return &phpIdentity{API: string(api[1]), ZTS: fields[3] == "1", Debug: fields[4] == "1"}, nil
}

func rewriteProbePaths(root *os.Root, path, final, candidate string) error {
	dir, err := root.Open(path)
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	_ = dir.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := filepath.Join(path, entry.Name())
		if entry.IsDir() {
			if err := rewriteProbePaths(root, name, final, candidate); err != nil {
				return err
			}
			continue
		}
		data, err := root.ReadFile(name)
		if err != nil {
			return err
		}
		if err := fsutil.AtomicWriteRoot(root, name, []byte(strings.ReplaceAll(string(data), final, candidate)), 0600); err != nil {
			return err
		}
	}
	return nil
}
