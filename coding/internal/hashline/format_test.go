package hashline

import "testing"

func TestFormatHeader(t *testing.T) {
	got := FormatHeader("internal/foo.go", "A1B2")
	want := "¶internal/foo.go#A1B2"
	if got != want {
		t.Errorf("FormatHeader = %q, want %q", got, want)
	}
}

func TestFormatNumberedLine(t *testing.T) {
	got := FormatNumberedLine(3, "func Add(a, b int) int {")
	want := "3:func Add(a, b int) int {"
	if got != want {
		t.Errorf("FormatNumberedLine = %q, want %q", got, want)
	}
	if got := FormatNumberedLine(2, ""); got != "2:" {
		t.Errorf("empty line = %q, want %q", got, "2:")
	}
}

func TestFormatNumberedLines(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		startLine int
		want      string
	}{
		{
			name:      "multi line from 1",
			text:      "package internal\n\nfunc Add(a, b int) int {\n\treturn a + b\n}",
			startLine: 1,
			want:      "1:package internal\n2:\n3:func Add(a, b int) int {\n4:\treturn a + b\n5:}",
		},
		{
			name:      "slice from 3",
			text:      "func Add(a, b int) int {\n\treturn a + b\n}",
			startLine: 3,
			want:      "3:func Add(a, b int) int {\n4:\treturn a + b\n5:}",
		},
		{
			name:      "empty text yields one numbered empty row",
			text:      "",
			startLine: 1,
			want:      "1:",
		},
		{
			name:      "trailing newline becomes final empty row",
			text:      "a\n",
			startLine: 1,
			want:      "1:a\n2:",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatNumberedLines(tt.text, tt.startLine); got != tt.want {
				t.Errorf("FormatNumberedLines = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompactDiff(t *testing.T) {
	tests := []struct {
		name    string
		oldText string
		newText string
		want    string
	}{
		{
			name:    "single line change",
			oldText: "func Add(a, b int) int {\n\treturn a-b\n}",
			newText: "func Add(a, b int) int {\n\treturn a + b\n}",
			want:    "-\treturn a-b\n+\treturn a + b",
		},
		{
			name:    "pure addition",
			oldText: "a\nb",
			newText: "a\nx\nb",
			want:    "+x",
		},
		{
			name:    "pure deletion",
			oldText: "a\nx\nb",
			newText: "a\nb",
			want:    "-x",
		},
		{
			name:    "identical",
			oldText: "a\nb",
			newText: "a\nb",
			want:    "",
		},
		{
			name:    "replace one with many",
			oldText: "a\nb\nc",
			newText: "a\nx\ny\nz\nc",
			want:    "-b\n+x\n+y\n+z",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CompactDiff(tt.oldText, tt.newText); got != tt.want {
				t.Errorf("CompactDiff = %q, want %q", got, tt.want)
			}
		})
	}
}
