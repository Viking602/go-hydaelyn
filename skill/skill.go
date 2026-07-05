// Package skill parses and registers Agent Skills-compatible instruction bundles.
package skill

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Skill is a parsed Agent Skills-compatible reusable instruction bundle.
type Skill struct {
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	License       string            `json:"license,omitempty"`
	Compatibility string            `json:"compatibility,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	// AllowedTools is advisory only: it records the tools the skill author
	// intended to surface. Hydaelyn does NOT enforce this list — actual tool
	// availability and policy come from the agent configuration (spec
	// 11-boundaries.md). The field is parsed and preserved for host tooling
	// (e.g. linters, UIs) but has no runtime gate effect.
	AllowedTools []string `json:"allowedTools,omitempty"`
	Body         string   `json:"body,omitempty"`
	SourcePath   string   `json:"sourcePath,omitempty"`
	SourceDir    string   `json:"sourceDir,omitempty"`
}

// ErrRegistryNil is returned when a registry operation requires a Registry.
var ErrRegistryNil = errors.New("skill: registry is nil")

// ValidationError reports an invalid SKILL.md field.
type ValidationError struct {
	Field  string
	Reason string
}

// Error satisfies the error interface.
func (e *ValidationError) Error() string {
	return "skill: invalid SKILL.md: " + e.Field + ": " + e.Reason
}

// DuplicateError is returned by Register when a skill with the same name exists.
type DuplicateError struct {
	Name string
}

// Error satisfies the error interface.
func (e *DuplicateError) Error() string { return "skill: skill already registered: " + e.Name }

// NotFoundError is returned by Resolve when a named skill is absent.
type NotFoundError struct {
	Name string
}

// Error satisfies the error interface.
func (e *NotFoundError) Error() string { return "skill: skill not registered: " + e.Name }

// Registry holds registered skills keyed by Skill.Name.
type Registry struct {
	skills map[string]Skill
}

// Parse parses one SKILL.md file body.
func Parse(path string, content []byte) (Skill, error) {
	frontmatter, body, err := splitFrontmatter(content)
	if err != nil {
		return Skill{}, err
	}
	parsed, err := parseFrontmatter(frontmatter)
	if err != nil {
		return Skill{}, err
	}
	s := Skill{
		Name:          parsed.Name,
		Description:   parsed.Description,
		License:       parsed.License,
		Compatibility: parsed.Compatibility,
		Metadata:      parsed.Metadata,
		AllowedTools:  parsed.AllowedTools,
		Body:          body,
	}
	if path != "" {
		s.SourcePath = filepath.Clean(path)
		s.SourceDir = filepath.Dir(s.SourcePath)
	}
	if err := validateSkill(s, true); err != nil {
		return Skill{}, err
	}
	return s, nil
}

// LoadDir reads SKILL.md directly under dir and parses it.
func LoadDir(dir string) (Skill, error) {
	path := filepath.Join(dir, "SKILL.md")
	content, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	return Parse(path, content)
}

// NewRegistry returns an empty skill registry.
func NewRegistry() *Registry { return &Registry{skills: map[string]Skill{}} }

// Register installs s into r.
func Register(r *Registry, s Skill) error {
	if r == nil {
		return ErrRegistryNil
	}
	if err := validateSkill(s, false); err != nil {
		return err
	}
	if r.skills == nil {
		r.skills = map[string]Skill{}
	}
	if _, dup := r.skills[s.Name]; dup {
		return &DuplicateError{Name: s.Name}
	}
	r.skills[s.Name] = cloneSkill(s)
	return nil
}

// Deregister removes a skill by name. It returns false when absent.
func (r *Registry) Deregister(name string) bool {
	if r == nil || r.skills == nil {
		return false
	}
	if _, ok := r.skills[name]; !ok {
		return false
	}
	delete(r.skills, name)
	return true
}

// Get returns a defensive copy of a skill by name.
func (r *Registry) Get(name string) (Skill, bool) {
	if r == nil || r.skills == nil {
		return Skill{}, false
	}
	s, ok := r.skills[name]
	if !ok {
		return Skill{}, false
	}
	return cloneSkill(s), true
}

// List returns defensive copies of every registered skill sorted by name.
func (r *Registry) List() []Skill {
	if r == nil || len(r.skills) == 0 {
		return nil
	}
	out := make([]Skill, 0, len(r.skills))
	for _, s := range r.skills {
		out = append(out, cloneSkill(s))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Resolve returns skills by name in caller order, de-duplicated by first use.
func (r *Registry) Resolve(names ...string) ([]Skill, error) {
	if r == nil {
		return nil, ErrRegistryNil
	}
	seen := make(map[string]bool, len(names))
	out := make([]Skill, 0, len(names))
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		s, ok := r.Get(name)
		if !ok {
			return nil, &NotFoundError{Name: name}
		}
		out = append(out, s)
	}
	return out, nil
}

// RenderSystemSection renders active skills as one deterministic system section.
func RenderSystemSection(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Active Hydaelyn skills:\n")
	b.WriteString("Use these reusable instructions when they apply to the task. Skills do not grant tool permission; available tools and policy still come from the agent configuration.\n\n")
	for i, s := range skills {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("--- skill: ")
		b.WriteString(s.Name)
		b.WriteString(" ---\n")
		b.WriteString("Description: ")
		b.WriteString(s.Description)
		b.WriteString("\n")
		b.WriteString("Source directory: ")
		b.WriteString(s.SourceDir)
		b.WriteString("\n\n")
		b.WriteString(s.Body)
		if s.Body != "" && !strings.HasSuffix(s.Body, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("--- end skill: ")
		b.WriteString(s.Name)
		b.WriteString(" ---")
	}
	return b.String()
}

type frontmatterFields struct {
	Name          string
	Description   string
	License       string
	Compatibility string
	Metadata      map[string]string
	AllowedTools  []string
}

func splitFrontmatter(content []byte) (string, string, error) {
	normalized := bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	normalized = bytes.ReplaceAll(normalized, []byte("\r"), []byte("\n"))
	text := string(normalized)
	if !strings.HasPrefix(text, "---\n") {
		return "", "", &ValidationError{Field: "frontmatter", Reason: "missing opening delimiter"}
	}
	lines := strings.Split(text[len("---\n"):], "\n")
	for i, line := range lines {
		if line != "---" {
			continue
		}
		frontmatter := strings.Join(lines[:i], "\n")
		body := strings.Join(lines[i+1:], "\n")
		body = strings.TrimPrefix(body, "\n")
		return frontmatter, body, nil
	}
	return "", "", &ValidationError{Field: "frontmatter", Reason: "missing closing delimiter"}
}

func parseFrontmatter(content string) (frontmatterFields, error) {
	var fields frontmatterFields
	if strings.TrimSpace(content) == "" {
		return fields, nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		return frontmatterFields{}, &ValidationError{Field: "frontmatter", Reason: "invalid YAML: " + err.Error()}
	}
	if len(root.Content) == 0 || root.Content[0].Kind == 0 {
		return fields, nil
	}
	node := root.Content[0]
	if node.Kind != yaml.MappingNode {
		return frontmatterFields{}, &ValidationError{Field: "frontmatter", Reason: "must be a mapping"}
	}
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		value := node.Content[i+1]
		switch key.Value {
		case "name":
			text, err := yamlString("name", value)
			if err != nil {
				return frontmatterFields{}, err
			}
			fields.Name = text
		case "description":
			text, err := yamlString("description", value)
			if err != nil {
				return frontmatterFields{}, err
			}
			fields.Description = text
		case "license":
			text, err := yamlString("license", value)
			if err != nil {
				return frontmatterFields{}, err
			}
			fields.License = text
		case "compatibility":
			text, err := yamlString("compatibility", value)
			if err != nil {
				return frontmatterFields{}, err
			}
			fields.Compatibility = text
		case "metadata":
			metadata, err := yamlStringMap(value)
			if err != nil {
				return frontmatterFields{}, err
			}
			fields.Metadata = metadata
		case "allowed-tools":
			tools, err := yamlAllowedTools(value)
			if err != nil {
				return frontmatterFields{}, err
			}
			fields.AllowedTools = tools
		}
	}
	return fields, nil
}

func yamlString(field string, node *yaml.Node) (string, error) {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return "", &ValidationError{Field: field, Reason: "must be a string"}
	}
	return node.Value, nil
}

func yamlStringMap(node *yaml.Node) (map[string]string, error) {
	if node.Kind != yaml.MappingNode {
		return nil, &ValidationError{Field: "metadata", Reason: "must be a string map"}
	}
	metadata := make(map[string]string, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		value := node.Content[i+1]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			return nil, &ValidationError{Field: "metadata", Reason: "keys must be strings"}
		}
		if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
			return nil, &ValidationError{Field: "metadata", Reason: "values must be strings"}
		}
		metadata[key.Value] = value.Value
	}
	return metadata, nil
}

func yamlAllowedTools(node *yaml.Node) ([]string, error) {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag != "!!str" {
			return nil, &ValidationError{Field: "allowed-tools", Reason: "must be a string or string list"}
		}
		return splitAllowedTools(node.Value), nil
	case yaml.SequenceNode:
		tools := make([]string, 0, len(node.Content))
		for _, item := range node.Content {
			if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
				return nil, &ValidationError{Field: "allowed-tools", Reason: "list entries must be strings"}
			}
			name := strings.TrimSpace(item.Value)
			if name != "" {
				tools = append(tools, name)
			}
		}
		return tools, nil
	default:
		return nil, &ValidationError{Field: "allowed-tools", Reason: "must be a string or string list"}
	}
}

func splitAllowedTools(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	tools := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			tools = append(tools, part)
		}
	}
	return tools
}

func validateSkill(s Skill, checkPath bool) error {
	if err := validateName(s.Name); err != nil {
		return err
	}
	if strings.TrimSpace(s.Description) == "" {
		return &ValidationError{Field: "description", Reason: "is required"}
	}
	if len(s.Description) > 1024 {
		return &ValidationError{Field: "description", Reason: "must be at most 1024 characters"}
	}
	if containsXMLTag(s.Description) {
		return &ValidationError{Field: "description", Reason: "must not contain XML tags"}
	}
	if len(s.Compatibility) > 500 {
		return &ValidationError{Field: "compatibility", Reason: "must be at most 500 characters"}
	}
	if checkPath && s.SourcePath != "" {
		dir := filepath.Dir(s.SourcePath)
		base := filepath.Base(dir)
		if base != "" && base != "." && base != string(filepath.Separator) && base != s.Name {
			return &ValidationError{Field: "name", Reason: fmt.Sprintf("must match parent directory %q", base)}
		}
	}
	return nil
}

func validateName(name string) error {
	if name == "" {
		return &ValidationError{Field: "name", Reason: "is required"}
	}
	if len(name) > 64 {
		return &ValidationError{Field: "name", Reason: "must be at most 64 characters"}
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") || strings.Contains(name, "--") {
		return &ValidationError{Field: "name", Reason: "must use single interior hyphens only"}
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			continue
		}
		return &ValidationError{Field: "name", Reason: "must contain only lowercase ASCII letters, digits, and hyphens"}
	}
	if isReservedSkillNameWord(name) {
		return &ValidationError{Field: "name", Reason: "must not contain reserved words \"anthropic\" or \"claude\""}
	}
	return nil
}

// containsXMLTag reports whether s contains an XML-like tag of the form <...>
// where ... contains no whitespace or angle brackets, e.g. </system>, <image>.
// Anthropic's Agent Skills spec forbids XML tags in the description field to
// prevent prompt-injection via frontmatter that gets rendered into the
// system section. The name field is already constrained to [a-z0-9-] by
// validateName, which excludes '<' and '>' and therefore makes this check
// redundant for names — it is only applied to the free-text description. The
// check flags any "<token>" sequence where token is one or more non-whitespace,
// non-"<"/">" characters, covering both opening (<x>) and closing (</x>) forms.
func containsXMLTag(s string) bool {
	for i := range s {
		if s[i] != '<' {
			continue
		}
		j := i + 1
		for j < len(s) && s[j] != '>' && s[j] != '<' && s[j] != ' ' && s[j] != '\t' && s[j] != '\n' && s[j] != '\r' {
			j++
		}
		if j > i+1 && j < len(s) && s[j] == '>' {
			return true
		}
	}
	return false
}

// isReservedSkillNameWord reports whether name is a reserved word that the
// Anthropic Agent Skills spec forbids in skill names. The check is on the
// whole name (case-insensitive): a name equal to "anthropic" or "claude" is
// rejected. Hyphenated compounds are allowed, so "claude-code-review" is fine
// (claude is a prefix joined to a non-reserved segment) while "claude" is not.
func isReservedSkillNameWord(name string) bool {
	lower := strings.ToLower(name)
	for _, word := range []string{"anthropic", "claude"} {
		if lower == word {
			return true
		}
	}
	return false
}

func cloneSkill(s Skill) Skill {
	s.Metadata = cloneStringMap(s.Metadata)
	s.AllowedTools = cloneStrings(s.AllowedTools)
	return s
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}
