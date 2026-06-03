package hashline

import "testing"

func TestNormalize_BOMAndLineEnding(t *testing.T) {
	bom := "\uFEFF"
	tests := []struct {
		name       string
		raw        string
		wantBOM    string
		wantEnding string
		wantText   string
	}{
		{
			name:       "plain LF",
			raw:        "a\nb\nc",
			wantBOM:    "",
			wantEnding: "\n",
			wantText:   "a\nb\nc",
		},
		{
			name:       "CRLF detected",
			raw:        "a\r\nb\r\nc",
			wantBOM:    "",
			wantEnding: "\r\n",
			wantText:   "a\nb\nc",
		},
		{
			name:       "BOM stripped LF",
			raw:        bom + "a\nb",
			wantBOM:    bom,
			wantEnding: "\n",
			wantText:   "a\nb",
		},
		{
			name:       "BOM + CRLF",
			raw:        bom + "a\r\nb",
			wantBOM:    bom,
			wantEnding: "\r\n",
			wantText:   "a\nb",
		},
		{
			name:       "mixed endings treated as CRLF",
			raw:        "a\r\nb\nc",
			wantBOM:    "",
			wantEnding: "\r\n",
			wantText:   "a\nb\nc",
		},
		{
			name:       "empty",
			raw:        "",
			wantBOM:    "",
			wantEnding: "\n",
			wantText:   "",
		},
		{
			name:       "lone CR preserved as content",
			raw:        "a\rb\nc",
			wantBOM:    "",
			wantEnding: "\n",
			wantText:   "a\rb\nc",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nf := Normalize(tt.raw)
			if nf.Raw != tt.raw {
				t.Errorf("Raw = %q, want %q", nf.Raw, tt.raw)
			}
			if nf.BOM != tt.wantBOM {
				t.Errorf("BOM = %q, want %q", nf.BOM, tt.wantBOM)
			}
			if nf.LineEnding != tt.wantEnding {
				t.Errorf("LineEnding = %q, want %q", nf.LineEnding, tt.wantEnding)
			}
			if nf.Text != tt.wantText {
				t.Errorf("Text = %q, want %q", nf.Text, tt.wantText)
			}
		})
	}
}

func TestNormalizedFile_RestoreRoundTrip(t *testing.T) {
	bom := "\uFEFF"
	raws := []string{
		"a\nb\nc",
		"a\r\nb\r\nc",
		bom + "a\nb\nc",
		bom + "a\r\nb\r\nc\r\n",
		"single",
		"trailing\n",
		"trailing\r\n",
		"",
	}
	for _, raw := range raws {
		nf := Normalize(raw)
		got := nf.Restore(nf.Text)
		if got != raw {
			t.Errorf("round-trip mismatch: raw=%q got=%q", raw, got)
		}
	}
}

func TestNormalizedFile_RestoreNewContent(t *testing.T) {
	// A CRLF file edited to new LF-internal content must come back as CRLF.
	nf := Normalize("a\r\nb\r\n")
	got := nf.Restore("x\ny\nz")
	want := "x\r\ny\r\nz"
	if got != want {
		t.Errorf("Restore = %q, want %q", got, want)
	}

	// BOM is re-applied to new content.
	bom := "\uFEFF"
	nf2 := Normalize(bom + "a\nb")
	got2 := nf2.Restore("x\ny")
	want2 := bom + "x\ny"
	if got2 != want2 {
		t.Errorf("Restore = %q, want %q", got2, want2)
	}
}
