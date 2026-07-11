// Package skill parses and registers Agent Skills-compatible instruction bundles.
package skill

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const maxSkillBytes = 1 << 20

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
	AllowedTools []string   `json:"allowedTools,omitempty"`
	Body         string     `json:"body,omitempty"`
	SourcePath   string     `json:"sourcePath,omitempty"`
	SourceDir    string     `json:"sourceDir,omitempty"`
	Resources    []Resource `json:"resources,omitempty"`
	sourceInfo   os.FileInfo
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
	if len(content) > maxSkillBytes {
		return Skill{}, &ValidationError{Field: "file", Reason: "must be at most 1 MiB"}
	}
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
	canonicalDir, root, sourceInfo, err := openSkillRoot(dir)
	if err != nil {
		return Skill{}, err
	}
	defer root.Close()
	content, err := readSkillDefinition(root)
	if err != nil {
		return Skill{}, err
	}
	path := filepath.Join(canonicalDir, "SKILL.md")
	s, err := Parse(path, content)
	if err != nil {
		return Skill{}, err
	}
	s.sourceInfo = sourceInfo
	if err := loadResourceManifest(&s, root); err != nil {
		return Skill{}, err
	}
	return s, nil
}

func openSkillRoot(dir string) (string, *os.Root, os.FileInfo, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", nil, nil, err
	}
	dirInfo, err := os.Lstat(absDir)
	if err != nil {
		return "", nil, nil, err
	}
	if dirInfo.Mode()&os.ModeSymlink != 0 || !dirInfo.IsDir() {
		return "", nil, nil, fmt.Errorf("skill: directory %q must be a real directory", dir)
	}
	canonicalDir, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		return "", nil, nil, err
	}
	root, err := os.OpenRoot(canonicalDir)
	if err != nil {
		return "", nil, nil, err
	}
	sourceInfo, err := root.Stat(".")
	if err != nil {
		root.Close()
		return "", nil, nil, err
	}
	if err := validateSourceIdentity(dir, dirInfo, sourceInfo); err != nil {
		root.Close()
		return "", nil, nil, err
	}
	return canonicalDir, root, sourceInfo, nil
}

func readSkillDefinition(root *os.Root) ([]byte, error) {
	info, err := root.Lstat("SKILL.md")
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("skill: SKILL.md must be a regular file")
	}
	file, err := root.Open("SKILL.md")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("skill: SKILL.md must be a regular file")
	}
	return readBounded(file, maxSkillBytes)
}

func validateSourceIdentity(path string, before, after os.FileInfo) error {
	if !os.SameFile(before, after) {
		return fmt.Errorf("skill: directory %q changed while loading", path)
	}
	return nil
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	content, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > limit {
		return nil, &ValidationError{Field: "file", Reason: fmt.Sprintf("must be at most %d bytes", limit)}
	}
	return content, nil
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
		b.WriteString("\n")
		if s.Compatibility != "" {
			b.WriteString("Compatibility: ")
			b.WriteString(s.Compatibility)
			b.WriteString("\n")
		}
		if len(s.Resources) > 0 {
			b.WriteString("Resources (load only when needed):\n")
			for _, resource := range s.Resources {
				b.WriteString("- ")
				b.WriteString(resource.Name)
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
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
	Name             string
	Description      string
	License          string
	Compatibility    string
	Metadata         map[string]string
	AllowedTools     []string
	compatibilitySet bool
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
	node, err := decodeFrontmatterNode(content)
	if err != nil {
		return frontmatterFields{}, err
	}
	if node == nil {
		return fields, nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if err := applyFrontmatterField(&fields, node.Content[i].Value, node.Content[i+1]); err != nil {
			return frontmatterFields{}, err
		}
	}
	if fields.compatibilitySet && strings.TrimSpace(fields.Compatibility) == "" {
		return frontmatterFields{}, &ValidationError{Field: "compatibility", Reason: "must not be empty when provided"}
	}
	return fields, nil
}

func decodeFrontmatterNode(content string) (*yaml.Node, error) {
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		return nil, &ValidationError{Field: "frontmatter", Reason: "invalid YAML: " + err.Error()}
	}
	if len(root.Content) == 0 || root.Content[0].Kind == 0 {
		return nil, nil
	}
	node := root.Content[0]
	if node.Kind != yaml.MappingNode {
		return nil, &ValidationError{Field: "frontmatter", Reason: "must be a mapping"}
	}
	if err := rejectDuplicateKeys(node); err != nil {
		return nil, err
	}
	return node, nil
}

func applyFrontmatterField(fields *frontmatterFields, key string, value *yaml.Node) error {
	switch key {
	case "name":
		text, err := yamlString("name", value)
		fields.Name = text
		return err
	case "description":
		text, err := yamlString("description", value)
		fields.Description = text
		return err
	case "license":
		text, err := yamlString("license", value)
		fields.License = text
		return err
	case "compatibility":
		text, err := yamlString("compatibility", value)
		fields.Compatibility = text
		fields.compatibilitySet = err == nil
		return err
	case "metadata":
		metadata, err := yamlStringMap(value)
		fields.Metadata = metadata
		return err
	case "allowed-tools":
		tools, err := yamlAllowedTools(value)
		fields.AllowedTools = tools
		return err
	}
	return nil
}

func rejectDuplicateKeys(root *yaml.Node) error {
	pending := []*yaml.Node{root}
	for len(pending) > 0 {
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if node.Kind == yaml.MappingNode {
			if err := rejectMappingDuplicates(node); err != nil {
				return err
			}
		}
		pending = append(pending, node.Content...)
	}
	return nil
}

func rejectMappingDuplicates(node *yaml.Node) error {
	seen := make(map[string]struct{}, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if _, ok := seen[key]; ok {
			return &ValidationError{Field: "frontmatter", Reason: fmt.Sprintf("duplicate key %q", key)}
		}
		seen[key] = struct{}{}
	}
	return nil
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

// splitAllowedTools splits the standard space-separated form while preserving
// whitespace inside parenthesized tool patterns such as "Bash(git diff:*)".
func splitAllowedTools(value string) []string {
	tools := make([]string, 0)
	start, depth := -1, 0
	for i, r := range value {
		if unicode.IsSpace(r) && depth == 0 {
			if start >= 0 {
				tools = append(tools, value[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
		depth = nextToolDepth(depth, r)
	}
	if start >= 0 {
		tools = append(tools, value[start:])
	}
	return tools
}

func nextToolDepth(depth int, r rune) int {
	if r == '(' {
		return depth + 1
	}
	if r == ')' && depth > 0 {
		return depth - 1
	}
	return depth
}

func validateSkill(s Skill, checkPath bool) error {
	if err := validateName(s.Name); err != nil {
		return err
	}
	if strings.TrimSpace(s.Description) == "" {
		return &ValidationError{Field: "description", Reason: "is required"}
	}
	if utf8.RuneCountInString(s.Description) > 1024 {
		return &ValidationError{Field: "description", Reason: "must be at most 1024 characters"}
	}
	if utf8.RuneCountInString(s.Compatibility) > 500 {
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
	if utf8.RuneCountInString(name) > 64 {
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
	return nil
}

func cloneSkill(s Skill) Skill {
	s.Metadata = cloneStringMap(s.Metadata)
	s.AllowedTools = cloneStrings(s.AllowedTools)
	s.Resources = cloneResources(s.Resources)
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
