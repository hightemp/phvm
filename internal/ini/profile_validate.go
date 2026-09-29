package ini

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/hightemp/phvm/internal/redact"
	"github.com/hightemp/phvm/internal/toolchain"
)

func validateConfiguration(ctx context.Context, php, etc string) error {
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, php, "-c", filepath.Join(etc, "php.ini"), "-d", "display_startup_errors=1", "-d", "display_errors=stderr", "-d", "log_errors=0", "-r", `echo "phvm-ini-ok";`)
	cmd.Env = []string(toolchain.Current("PHPRC="+filepath.Join(etc, "php.ini"), "PHP_INI_SCAN_DIR="+filepath.Join(etc, "conf.d")))
	cmd.WaitDelay = time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if probeCtx.Err() != nil {
		return probeCtx.Err()
	}
	if err != nil {
		return fmt.Errorf("validate ini profile with PHP: %w: %s", redact.Error(err, ""), redact.Text(stderr.String()))
	}
	if stderr.Len() != 0 || string(output) != "phvm-ini-ok" {
		return fmt.Errorf("ini profile startup failed: %s", redact.Text(stderr.String()+string(output)))
	}
	return nil
}
