package hashline

import (
	"regexp"
	"testing"
)

var tagPattern = regexp.MustCompile(`^[0-9A-F]{4}$`)

func TestNormalizeForHash_TrimsTrailingWhitespace(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"trailing spaces", "a   \nb", "a\nb"},
		{"trailing tabs", "a\t\t\nb", "a\nb"},
		{"trailing CR", "a\r\nb\r", "a\nb"},
		{"mixed trailing", "a \t\r\nb", "a\nb"},
		{"final line trimmed", "a\nb   ", "a\nb"},
		{"interior whitespace kept", "a b\tc\nd", "a b\tc\nd"},
		{"empty lines preserved", "a\n\n\nb", "a\n\n\nb"},
		{"leading whitespace kept", "   a\nb", "   a\nb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeForHash(tt.in); got != tt.want {
				t.Errorf("NormalizeForHash(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestComputeFileHash_Format(t *testing.T) {
	for _, in := range []string{"", "a", "package main\n\nfunc main() {}\n", "日本語\n"} {
		tag := ComputeFileHash(in)
		if !tagPattern.MatchString(tag) {
			t.Errorf("ComputeFileHash(%q) = %q, not four uppercase hex digits", in, tag)
		}
	}
}

func TestComputeFileHash_StableForSameContent(t *testing.T) {
	a := ComputeFileHash("package foo\n")
	b := ComputeFileHash("package foo\n")
	if a != b {
		t.Errorf("same content gave different tags: %q vs %q", a, b)
	}
}

func TestComputeFileHash_ChangesWithContent(t *testing.T) {
	a := ComputeFileHash("package foo\n")
	b := ComputeFileHash("package bar\n")
	if a == b {
		t.Errorf("different content gave the same tag %q (allowed but improbable here)", a)
	}
}

func TestComputeFileHash_IgnoresTrailingWhitespace(t *testing.T) {
	a := ComputeFileHash("a\nb\n")
	b := ComputeFileHash("a   \nb\t\n")
	if a != b {
		t.Errorf("trailing whitespace changed the tag: %q vs %q", a, b)
	}
}
