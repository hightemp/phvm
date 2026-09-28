package doctor

import (
	"reflect"
	"testing"
)

func TestSplitCompilerFlags(t *testing.T) {
	for _, tt := range []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{name: "empty"},
		{name: "escaped path", input: `-I/a\ b/include -L/a\ b/lib -lcurl`, want: []string{"-I/a b/include", "-L/a b/lib", "-lcurl"}},
		{name: "quoted path", input: `cc -I"/a b/include" '-DVALUE=a b'`, want: []string{"cc", "-I/a b/include", "-DVALUE=a b"}},
		{name: "double quote escapes", input: `"a\qb\"c\\d"`, want: []string{`a\qb"c\d`}},
		{name: "single quote literal", input: `'-DVALUE=\$x'`, want: []string{`-DVALUE=\$x`}},
		{name: "empty argument", input: `cc "" -g`, want: []string{"cc", "", "-g"}},
		{name: "line continuation", input: "\\\n -g", want: []string{"-g"}},
		{name: "no shell expansion", input: `"$(some-command)" "$VALUE"`, want: []string{"$(some-command)", "$VALUE"}},
		{name: "unterminated quote", input: `-I"/a b`, wantErr: true},
		{name: "unterminated escape", input: "-g\\", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := splitCompilerFlags(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr = %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("flags = %q, want %q", got, tt.want)
			}
		})
	}
}
