package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hightemp/phvm/internal/redact"
	"github.com/hightemp/phvm/internal/toolchain"
)

type libraryProbe struct {
	name, source, libs, prefix string
	compiler                   string
	modules                    []string
	minVersion                 string
	excluded                   []string
}

var libraryProbes = map[string]libraryProbe{
	"openssl":    {source: "#include <openssl/ssl.h>\nint main(void) { return SSL_new(0) == 0; }\n", prefix: "OPENSSL"},
	"libcurl":    {source: "#include <curl/curl.h>\nint main(void) { return curl_easy_init() == 0; }\n", prefix: "CURL"},
	"zlib":       {source: "#include <zlib.h>\nint main(void) { return zlibVersion() == 0; }\n", prefix: "ZLIB"},
	"libxml-2.0": {source: "#include <libxml/parser.h>\nint main(void) { xmlInitParser(); return 0; }\n", prefix: "LIBXML"},
	"oniguruma":  {source: "#include <oniguruma.h>\nint main(void) { return onig_initialize(0, 0); }\n", prefix: "ONIG"},
	"readline":   {source: "#include <readline/readline.h>\nint main(void) { return readline(\"\") == 0; }\n", libs: "-lreadline"},
	"sqlite3":    {source: "#include <sqlite3.h>\nint main(void) { return sqlite3_libversion() == 0; }\n", prefix: "SQLITE"},
	"bzip2":      {source: "#include <bzlib.h>\nint main(void) { return BZ2_bzlibVersion() == 0; }\n", libs: "-lbz2"},
	"gmp":        {source: "#include <gmp.h>\nint main(void) { mpz_t n, root, rem; mpz_init(n); mpz_init(root); mpz_init(rem); mpz_rootrem(root, rem, n, 2); mpz_clear(n); mpz_clear(root); mpz_clear(rem); return 0; }\n", libs: "-lgmp"},
	"iconv":      {source: "#include <iconv.h>\nint main(void) { return iconv_open(\"UTF-8\", \"UTF-8\") == (iconv_t)-1; }\n"},
	"gettext":    {source: "#include <libintl.h>\nint main(void) { return gettext(\"phvm\") == 0; }\n"},
	"icu-uc":     {source: "#include <unicode/uclean.h>\nint main(void) { UErrorCode code=U_ZERO_ERROR; u_init(&code); return 0; }\n", prefix: "ICU", modules: []string{"icu-uc", "icu-io", "icu-i18n"}},
	"libpng":     {source: "#include <png.h>\nint main(void) { return png_get_libpng_ver(0) == 0; }\n", prefix: "PNG"},
	"libjpeg":    {source: "#include <stdio.h>\n#include <jpeglib.h>\nint main(void) { struct jpeg_error_mgr e; return jpeg_std_error(&e) == 0; }\n", prefix: "JPEG"},
	"freetype2":  {source: "#include <ft2build.h>\n#include FT_FREETYPE_H\nint main(void) { FT_Library l; return FT_Init_FreeType(&l); }\n", prefix: "FREETYPE2"},
	"libpq":      {source: "#include <libpq-fe.h>\nint main(void) { return PQlibVersion() == 0; }\n", prefix: "PGSQL"},
	"libsodium":  {source: "#include <sodium.h>\nint main(void) { return sodium_init(); }\n", prefix: "LIBSODIUM"},
	"libxslt":    {source: "#include <libxslt/xslt.h>\n#include <libexslt/exslt.h>\nint main(void) { xsltInit(); exsltRegisterAll(); return 0; }\n", prefix: "XSL", modules: []string{"libxslt", "libexslt"}},
	"libzip":     {source: "#include <zip.h>\nint main(void) { return zip_libzip_version() == 0; }\n", prefix: "LIBZIP"},
}

func splitCompilerFlags(value string) ([]string, error) { return toolchain.SplitArguments(value) }

// checkLibraryLink verifies headers and public symbols without running a probe.
func checkLibraryLink(name string) error {
	return linkLibrary(context.Background(), toolchain.Current(), probeFor(name))
}

func probeFor(name string) libraryProbe {
	p := libraryProbes[name]
	p.name = name
	if len(p.modules) == 0 && p.libs == "" && name != "iconv" && name != "gettext" {
		p.modules = []string{name}
	}
	return p
}

func linkLibrary(ctx context.Context, env toolchain.Environment, probe libraryProbe) error {
	if probe.source == "" {
		return fmt.Errorf("no link probe for %s", probe.name)
	}
	var cflags, libs []string
	cflagsKey := "CFLAGS"
	compilerKey, compilerDefault := "CC", "cc"
	if probe.compiler == "CXX" {
		cflagsKey = "CXXFLAGS"
		compilerKey = "CXX"
		compilerDefault = "c++"
	}
	for _, key := range []string{"CPPFLAGS", cflagsKey} {
		flags, err := splitCompilerFlags(env.Value(key, ""))
		if err != nil {
			return fmt.Errorf("compile/link check: parse %s: %w", key, err)
		}
		cflags = append(cflags, flags...)
	}
	if len(probe.modules) > 0 {
		for _, part := range []struct {
			arg, suffix string
			target      *[]string
		}{{"--cflags", "_CFLAGS", &cflags}, {"--libs", "_LIBS", &libs}} {
			value := env.Value(probe.prefix+part.suffix, "")
			if probe.prefix == "" || value == "" {
				cmd, err := env.Command(ctx, "PKG_CONFIG", "pkg-config", append([]string{part.arg}, probe.modules...)...)
				if err != nil {
					return err
				}
				data, err := cmd.CombinedOutput()
				if err != nil {
					return fmt.Errorf("compile/link check: pkg-config %s: %w: %s", part.arg, err, redact.Text(string(data)))
				}
				value = string(data)
			}
			flags, err := splitCompilerFlags(value)
			if err != nil {
				return fmt.Errorf("compile/link check: parse library flags: %w", err)
			}
			*part.target = append(*part.target, flags...)
		}
	} else {
		libs, _ = splitCompilerFlags(probe.libs)
	}
	dir, err := os.MkdirTemp("", "phvm-doctor-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "check.c")
	if probe.compiler == "CXX" {
		path = filepath.Join(dir, "check.cc")
	}
	if err := os.WriteFile(path, []byte(probe.source), 0600); err != nil {
		return err
	}
	args := append(cflags, path, "-o", filepath.Join(dir, "check"))
	for _, key := range []string{"LDFLAGS", "LIBS"} {
		flags, err := splitCompilerFlags(env.Value(key, ""))
		if err != nil {
			return fmt.Errorf("compile/link check: parse %s: %w", key, err)
		}
		if key == "LDFLAGS" {
			args = append(args, flags...)
		} else {
			libs = append(libs, flags...)
		}
	}
	args = append(args, libs...)
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd, err := env.Command(probeCtx, compilerKey, compilerDefault, args...)
	if err != nil {
		return err
	}
	data, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("compile/link check failed: %w\n%s", redact.Error(err, ""), redact.Text(strings.TrimSpace(string(data))))
	}
	return nil
}
