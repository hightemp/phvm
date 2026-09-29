package cli

import (
	"fmt"
	"path/filepath"
	"strings"
)

func phpConfigureFixture(prefix, version, capture string) string {
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	extDir := filepath.Join(prefix, "lib/php/extensions/no-debug-non-zts-20230831")
	php := fmt.Sprintf("#!/bin/sh\ncase \"$*\" in\n-n*-r*) printf '%%s\\n' %s %s %s 0 0 cli;;\n*-r*) echo %s;;\n*-i*) echo 'PHP API => 20230831';;\n*) echo 'PHP %s';;\nesac\n", quote(version), quote(prefix), quote(extDir), quote(version), version)
	config := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in\n--prefix) echo %s;;\n--version) echo %s;;\n--extension-dir) echo %s;;\n*) exit 1;;\nesac\n", quote(prefix), quote(version), quote(extDir))
	php = strings.ReplaceAll(php, "$", "$$")
	config = strings.ReplaceAll(config, "$", "$$")
	php = strings.ReplaceAll(strings.ReplaceAll(php, "\\", "\\\\"), "\n", "\\n")
	config = strings.ReplaceAll(strings.ReplaceAll(config, "\\", "\\\\"), "\n", "\\n")
	all := "@true"
	if capture != "" {
		all = "@printf '%s' '$(MAKEFLAGS)' > " + quote(capture)
	}
	bin := quote("$(INSTALL_ROOT)" + filepath.Join(prefix, "bin"))
	header := quote("$(INSTALL_ROOT)" + filepath.Join(prefix, "include/php/Zend"))
	return fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = --help ]; then echo '--with-pear --with-curl --with-zlib --with-sdk'; exit 0; fi\ncat > Makefile <<'EOF'\nall:\n\t%s\ninstall:\n\tmkdir -p %s %s\n\tprintf '%%b' %s > %s/php\n\tprintf '%%b' %s > %s/php-config\n\tprintf '#!/bin/sh\\nexit 0\\n' > %s/phpize\n\tprintf '#define ZEND_MODULE_API_NO 20230831\\n' > %s/zend_modules.h\n\tchmod 755 %s/php %s/php-config %s/phpize\nEOF\n", all, bin, header, quote(php), bin, quote(config), bin, bin, header, bin, bin, bin)
}
