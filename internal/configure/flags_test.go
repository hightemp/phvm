package configure

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/hightemp/phvm/internal/toolchain"
)

func TestArgumentContractAndIndependentNamespaces(t *testing.T) {
	args, err := Parse(`--with-sdk="/a path" --with-list='a,b' 'CFLAGS=-O0 -g' --with-literal='$HOME'`)
	want := []string{"--with-sdk=/a path", "--with-list=a,b", "CFLAGS=-O0 -g", "--with-literal=$HOME"}
	if err != nil || !reflect.DeepEqual(args, want) {
		t.Fatalf("parse=%q error=%v", args, err)
	}
	merged := Merge([]string{"--enable-foo", "--with-foo=one", "--with-foo=two", "CFLAGS=-O2"}, []string{"--disable-foo", "--without-foo", "CFLAGS=-O0 -g"})
	if want := []string{"--disable-foo", "--without-foo", "CFLAGS=-O0 -g"}; !reflect.DeepEqual(merged, want) {
		t.Errorf("merge=%q", merged)
	}
	for _, invalid := range []string{"", "trailing", "--with-x /path", "--with-x=first\n--with-y", "--with-"} {
		if err := Validate([]string{invalid}); err == nil {
			t.Errorf("invalid argv accepted: %q", invalid)
		}
	}
}

func TestSupportedFlagsUseExactSourceHelp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX configure help")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "configure")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nif [ \"$1\" = --help ]; then echo '--enable-FEATURE --with-PACKAGE --enable-demo --with-sdk'; exit 0; fi\ntouch unexpected-configure\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := CheckSupported(context.Background(), script, dir, toolchain.Current(), []string{"--disable-demo", "--with-sdk=/path with space"}); err != nil {
		t.Fatal(err)
	}
	if err := CheckSupported(context.Background(), script, dir, toolchain.Current(), []string{"--with-missing"}); err == nil {
		t.Error("generic PACKAGE placeholder accepted an unknown option")
	}
	if _, err := os.Stat(filepath.Join(dir, "unexpected-configure")); !os.IsNotExist(err) {
		t.Error("support validation executed configuration")
	}
}
