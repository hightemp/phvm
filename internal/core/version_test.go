package core

import (
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected *Version
		wantErr  bool
	}{
		{"8.3.30", &Version{Major: 8, Minor: 3, Patch: 30, Raw: "8.3.30"}, false},
		{"8.3", &Version{Major: 8, Minor: 3, Patch: -1, Raw: "8.3"}, false},
		{"8", &Version{Major: 8, Minor: -1, Patch: -1, Raw: "8"}, false},
		{"v8.3.30", &Version{Major: 8, Minor: 3, Patch: 30, Raw: "8.3.30"}, false},
		{"php-8.3.30", &Version{Major: 8, Minor: 3, Patch: 30, Raw: "8.3.30"}, false},
		{"", nil, true},
		{"invalid", nil, true},
		{"8.3.30.1", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, err := ParseVersion(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseVersion(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if v.Major != tt.expected.Major || v.Minor != tt.expected.Minor || v.Patch != tt.expected.Patch {
				t.Errorf("ParseVersion(%q) = %+v, want %+v", tt.input, v, tt.expected)
			}
		})
	}
}

func TestVersionString(t *testing.T) {
	tests := []struct {
		version  *Version
		expected string
	}{
		{&Version{Major: 8, Minor: 3, Patch: 30}, "8.3.30"},
		{&Version{Major: 8, Minor: 3, Patch: -1}, "8.3"},
		{&Version{Major: 8, Minor: -1, Patch: -1}, "8"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.version.String(); got != tt.expected {
				t.Errorf("Version.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestVersionIsComplete(t *testing.T) {
	tests := []struct {
		version  *Version
		expected bool
	}{
		{&Version{Major: 8, Minor: 3, Patch: 30}, true},
		{&Version{Major: 8, Minor: 3, Patch: -1}, false},
		{&Version{Major: 8, Minor: -1, Patch: -1}, false},
	}

	for _, tt := range tests {
		t.Run(tt.version.String(), func(t *testing.T) {
			if got := tt.version.IsComplete(); got != tt.expected {
				t.Errorf("Version.IsComplete() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestVersionMatches(t *testing.T) {
	tests := []struct {
		pattern *Version
		other   *Version
		matches bool
	}{
		{&Version{Major: 8, Minor: 3, Patch: -1}, &Version{Major: 8, Minor: 3, Patch: 30}, true},
		{&Version{Major: 8, Minor: -1, Patch: -1}, &Version{Major: 8, Minor: 3, Patch: 30}, true},
		{&Version{Major: 8, Minor: 3, Patch: 30}, &Version{Major: 8, Minor: 3, Patch: 30}, true},
		{&Version{Major: 8, Minor: 3, Patch: -1}, &Version{Major: 8, Minor: 2, Patch: 28}, false},
		{&Version{Major: 7, Minor: -1, Patch: -1}, &Version{Major: 8, Minor: 3, Patch: 30}, false},
	}

	for _, tt := range tests {
		name := tt.pattern.String() + " matches " + tt.other.String()
		t.Run(name, func(t *testing.T) {
			if got := tt.pattern.Matches(tt.other); got != tt.matches {
				t.Errorf("Version.Matches() = %v, want %v", got, tt.matches)
			}
		})
	}
}

func TestVersionCompare(t *testing.T) {
	tests := []struct {
		a, b     *Version
		expected int
	}{
		{&Version{Major: 8, Minor: 3, Patch: 30}, &Version{Major: 8, Minor: 3, Patch: 30}, 0},
		{&Version{Major: 8, Minor: 3, Patch: 30}, &Version{Major: 8, Minor: 3, Patch: 29}, 1},
		{&Version{Major: 8, Minor: 3, Patch: 30}, &Version{Major: 8, Minor: 3, Patch: 31}, -1},
		{&Version{Major: 8, Minor: 3, Patch: 30}, &Version{Major: 8, Minor: 2, Patch: 30}, 1},
		{&Version{Major: 8, Minor: 3, Patch: 30}, &Version{Major: 7, Minor: 4, Patch: 33}, 1},
	}

	for _, tt := range tests {
		name := tt.a.String() + " vs " + tt.b.String()
		t.Run(name, func(t *testing.T) {
			if got := tt.a.Compare(tt.b); got != tt.expected {
				t.Errorf("Compare() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSortVersions(t *testing.T) {
	versions := []string{"8.2.28", "8.3.30", "7.4.33", "8.1.27", "8.3.29"}
	expected := []string{"8.3.30", "8.3.29", "8.2.28", "8.1.27", "7.4.33"}

	SortVersions(versions)

	for i, v := range versions {
		if v != expected[i] {
			t.Errorf("SortVersions()[%d] = %s, want %s", i, v, expected[i])
		}
	}
}

func TestLatestVersion(t *testing.T) {
	versions := []string{"8.2.28", "8.3.30", "7.4.33", "8.1.27"}
	expected := "8.3.30"

	if got := LatestVersion(versions); got != expected {
		t.Errorf("LatestVersion() = %s, want %s", got, expected)
	}
}

func TestFilterVersions(t *testing.T) {
	versions := []string{"8.3.30", "8.3.29", "8.2.28", "8.1.27", "7.4.33"}
	pattern, _ := ParseVersion("8.3")

	result := FilterVersions(versions, pattern)

	if len(result) != 2 {
		t.Errorf("FilterVersions() returned %d versions, want 2", len(result))
	}

	for _, v := range result {
		if v != "8.3.30" && v != "8.3.29" {
			t.Errorf("FilterVersions() included unexpected version: %s", v)
		}
	}
}

func TestIsSpecialAlias(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"latest", true},
		{"stable", true},
		{"lts", true},
		{"LATEST", true},
		{"8.3", false},
		{"default", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := IsSpecialAlias(tt.input); got != tt.expected {
				t.Errorf("IsSpecialAlias(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
