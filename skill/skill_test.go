package skill

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParse_ValidRequiredFieldsAndBody(t *testing.T) {
	content := []byte("---\nname: code-review\ndescription: Review code before editing\n---\nFollow the review checklist.\nKeep comments actionable.\n")
	path := filepath.Join("testdata", "code-review", "SKILL.md")

	got, err := Parse(path, content)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	want := Skill{
		Name:        "code-review",
		Description: "Review code before editing",
		Body:        "Follow the review checklist.\nKeep comments actionable.\n",
		SourcePath:  filepath.Clean(path),
		SourceDir:   filepath.Dir(filepath.Clean(path)),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse() = %#v, want %#v", got, want)
	}
}

func TestParse_OptionalFields(t *testing.T) {
	tests := []struct {
		name         string
		frontmatter  string
		wantTools    []string
		wantMetadata map[string]string
	}{
		{
			name: "string allowed-tools",
			frontmatter: strings.Join([]string{
				"name: tool-string",
				"description: Exercise optional string fields",
				"license: MIT",
				"compatibility: Hydaelyn 0.8",
				"allowed-tools: read, write bash",
				"metadata:",
				"  owner: platform",
				"  priority: high",
			}, "\n"),
			wantTools: []string{"read", "write", "bash"},
			wantMetadata: map[string]string{
				"owner":    "platform",
				"priority": "high",
			},
		},
		{
			name: "list allowed-tools",
			frontmatter: strings.Join([]string{
				"name: tool-list",
				"description: Exercise YAML sequence tools",
				"license: Apache-2.0",
				"compatibility: Hydaelyn 0.8",
				"allowed-tools:",
				"  - read",
				"  - bash",
				"metadata:",
				"  owner: qa",
			}, "\n"),
			wantTools: []string{"read", "bash"},
			wantMetadata: map[string]string{
				"owner": "qa",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := []byte("---\n" + tt.frontmatter + "\n---\nBody text.\n")

			got, err := Parse("", content)
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}

			if got.License == "" {
				t.Fatal("License was not parsed")
			}
			if got.Compatibility != "Hydaelyn 0.8" {
				t.Fatalf("Compatibility = %q, want %q", got.Compatibility, "Hydaelyn 0.8")
			}
			if !reflect.DeepEqual(got.AllowedTools, tt.wantTools) {
				t.Fatalf("AllowedTools = %#v, want %#v", got.AllowedTools, tt.wantTools)
			}
			if !reflect.DeepEqual(got.Metadata, tt.wantMetadata) {
				t.Fatalf("Metadata = %#v, want %#v", got.Metadata, tt.wantMetadata)
			}
			if got.Body != "Body text.\n" {
				t.Fatalf("Body = %q, want %q", got.Body, "Body text.\n")
			}
		})
	}
}

func TestParse_InvalidSkillFiles(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		content    string
		wantField  string
		wantReason string
	}{
		{
			name:       "missing frontmatter",
			content:    "name: code-review\ndescription: Review code\n",
			wantField:  "frontmatter",
			wantReason: "missing opening delimiter",
		},
		{
			name:       "missing name",
			content:    "---\ndescription: Review code\n---\nBody\n",
			wantField:  "name",
			wantReason: "is required",
		},
		{
			name:       "missing description",
			path:       filepath.Join("testdata", "code-review", "SKILL.md"),
			content:    "---\nname: code-review\n---\nBody\n",
			wantField:  "description",
			wantReason: "is required",
		},
		{
			name:       "non-string name",
			content:    "---\nname: 123\ndescription: Review code\n---\nBody\n",
			wantField:  "name",
			wantReason: "must be a string",
		},
		{
			name:       "non-string description",
			content:    "---\nname: code-review\ndescription: false\n---\nBody\n",
			wantField:  "description",
			wantReason: "must be a string",
		},
		{
			name:       "non-string optional field",
			content:    "---\nname: code-review\ndescription: Review code\nlicense: true\n---\nBody\n",
			wantField:  "license",
			wantReason: "must be a string",
		},
		{
			name:       "invalid name characters",
			content:    "---\nname: Code Review\ndescription: Review code\n---\nBody\n",
			wantField:  "name",
			wantReason: "must contain only lowercase ASCII letters, digits, and hyphens",
		},
		{
			name:       "name must match parent directory",
			path:       filepath.Join("testdata", "other-skill", "SKILL.md"),
			content:    "---\nname: code-review\ndescription: Review code\n---\nBody\n",
			wantField:  "name",
			wantReason: "must match parent directory \"other-skill\"",
		},
		{
			name:       "overlong description",
			content:    "---\nname: code-review\ndescription: " + strings.Repeat("a", 1025) + "\n---\nBody\n",
			wantField:  "description",
			wantReason: "must be at most 1024 characters",
		},
		{
			name:       "overlong compatibility",
			content:    "---\nname: code-review\ndescription: Review code\ncompatibility: " + strings.Repeat("a", 501) + "\n---\nBody\n",
			wantField:  "compatibility",
			wantReason: "must be at most 500 characters",
		},
		{
			name:       "non-string metadata",
			content:    "---\nname: code-review\ndescription: Review code\nmetadata:\n  attempts: 3\n---\nBody\n",
			wantField:  "metadata",
			wantReason: "values must be strings",
		},
		{
			name:       "reserved word claude in name",
			content:    "---\nname: claude\ndescription: Review code\n---\nBody\n",
			wantField:  "name",
			wantReason: "must not contain reserved words \"anthropic\" or \"claude\"",
		},
		{
			name:       "reserved word anthropic in name",
			content:    "---\nname: anthropic\ndescription: Review code\n---\nBody\n",
			wantField:  "name",
			wantReason: "must not contain reserved words \"anthropic\" or \"claude\"",
		},
		{
			name:       "uppercase reserved word in name",
			content:    "---\nname: Claude\ndescription: Review code\n---\nBody\n",
			wantField:  "name",
			wantReason: "must contain only lowercase ASCII letters, digits, and hyphens",
		},
		{
			name:       "xml tag in description",
			content:    "---\nname: code-review\ndescription: Review <system>secrets</system> here\n---\nBody\n",
			wantField:  "description",
			wantReason: "must not contain XML tags",
		},
		{
			name:       "closing xml tag in description",
			content:    "---\nname: code-review\ndescription: Leak </instructions>\n---\nBody\n",
			wantField:  "description",
			wantReason: "must not contain XML tags",
		},
		{
			name:       "self-closing xml tag in description",
			content:    "---\nname: code-review\ndescription: Use <image/> tag\n---\nBody\n",
			wantField:  "description",
			wantReason: "must not contain XML tags",
		},
		{
			name:       "xml tag with attributes in description",
			content:    "---\nname: code-review\ndescription: Inject <system role=\"override\">secrets</system>\n---\nBody\n",
			wantField:  "description",
			wantReason: "must not contain XML tags",
		},
		{
			name:       "reserved word segment in compound name",
			content:    "---\nname: claude-tools\ndescription: Review code\n---\nBody\n",
			wantField:  "name",
			wantReason: "must not contain reserved words \"anthropic\" or \"claude\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.path, []byte(tt.content))
			assertValidationError(t, err, tt.wantField, tt.wantReason)
		})
	}
}

func TestLoadDir_ReadsSkillMarkdownFromSuppliedDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "load-skill")
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: load-skill\ndescription: Loaded from the requested directory\n---\nLoaded body.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "SKILL.md"), []byte("this nested file must not be parsed"), 0o644); err != nil {
		t.Fatalf("WriteFile nested SKILL.md: %v", err)
	}

	got, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir returned error: %v", err)
	}

	if got.Name != "load-skill" {
		t.Fatalf("Name = %q, want %q", got.Name, "load-skill")
	}
	if got.Description != "Loaded from the requested directory" {
		t.Fatalf("Description = %q", got.Description)
	}
	if got.Body != "Loaded body.\n" {
		t.Fatalf("Body = %q, want %q", got.Body, "Loaded body.\n")
	}
	wantPath := filepath.Join(dir, "SKILL.md")
	if got.SourcePath != filepath.Clean(wantPath) {
		t.Fatalf("SourcePath = %q, want %q", got.SourcePath, filepath.Clean(wantPath))
	}
	if got.SourceDir != filepath.Dir(filepath.Clean(wantPath)) {
		t.Fatalf("SourceDir = %q, want %q", got.SourceDir, filepath.Dir(filepath.Clean(wantPath)))
	}
}

func TestRenderSystemSection_IncludesActiveSkillsAndPermissionWarning(t *testing.T) {
	skills := []Skill{
		{
			Name:        "code-review",
			Description: "Review code before editing",
			SourceDir:   filepath.Join("skills", "code-review"),
			Body:        "Review diffs before changing files.",
		},
		{
			Name:        "incident-response",
			Description: "Handle incidents",
			SourceDir:   filepath.Join("skills", "incident-response"),
			Body:        "Escalate customer-impacting incidents.\n",
		},
	}

	got := RenderSystemSection(skills)
	want := strings.Join([]string{
		"Active Hydaelyn skills:",
		"Use these reusable instructions when they apply to the task. Skills do not grant tool permission; available tools and policy still come from the agent configuration.",
		"",
		"--- skill: code-review ---",
		"Description: Review code before editing",
		"Source directory: " + filepath.Join("skills", "code-review"),
		"",
		"Review diffs before changing files.",
		"--- end skill: code-review ---",
		"",
		"--- skill: incident-response ---",
		"Description: Handle incidents",
		"Source directory: " + filepath.Join("skills", "incident-response"),
		"",
		"Escalate customer-impacting incidents.",
		"--- end skill: incident-response ---",
	}, "\n")
	if got != want {
		t.Fatalf("RenderSystemSection() =\n%s\nwant\n%s", got, want)
	}
}

func TestParse_ReservedWordSegmentsAreRejected(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"claude prefix", "---\nname: claude-code-review\ndescription: Review code\n---\nBody\n"},
		{"anthropic suffix", "---\nname: review-anthropic\ndescription: Review code\n---\nBody\n"},
		{"both joined", "---\nname: anthropic-claude-review\ndescription: Review code\n---\nBody\n"},
		{"claude-tools", "---\nname: claude-tools\ndescription: Review code\n---\nBody\n"},
		{"anthropic-helper", "---\nname: anthropic-helper\ndescription: Review code\n---\nBody\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse("", []byte(tt.content))
			assertValidationError(t, err, "name", "must not contain reserved words \"anthropic\" or \"claude\"")
		})
	}
}

func TestParse_NonReservedCompoundNamesAreAllowed(t *testing.T) {
	content := "---\nname: code-review-helper\ndescription: Review code\n---\nBody\n"
	if _, err := Parse("", []byte(content)); err != nil {
		t.Fatalf("Parse() rejected a non-reserved compound name: %v", err)
	}
}

func TestContainsXMLTag(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"plain text", false},
		{"use a < b comparison", false},
		{"unclosed <tag", false},
		{"<system>", true},
		{"</instructions>", true},
		{"text <image/> more", true},
		{"only << angle", false},
		{"empty <>", false},
		{"a < b > c", true},
		{"<system role=\"override\">", true},
		{"</system role='x'>", true},
		{"< a > with spaces", true},
		{"bare > alone", false},
		{"a <b c d>", true},
	}
	for _, tt := range tests {
		if got := containsXMLTag(tt.in); got != tt.want {
			t.Errorf("containsXMLTag(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func assertValidationError(t *testing.T, err error, field, reason string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	if validation.Field != field || validation.Reason != reason {
		t.Fatalf("ValidationError = {Field:%q Reason:%q}, want {Field:%q Reason:%q}", validation.Field, validation.Reason, field, reason)
	}
}
