package skill

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResource_ManifestActivationAndExactRead(t *testing.T) {
	s := loadManifestTestSkill(t)
	assertManifestResourceNames(t, s)
	activated := Activate(s)
	if !strings.Contains(activated, "references/z.md") || strings.Contains(activated, "secret reference") {
		t.Fatalf("Activate() did not disclose manifest progressively:\n%s", activated)
	}
	content, err := ReadResource(s, "references/z.md")
	if err != nil || string(content) != "secret reference" {
		t.Fatalf("ReadResource() = %q, %v", content, err)
	}
	for _, name := range []string{"../SKILL.md", "references/../SKILL.md", "SKILL.md", "other/missing.txt"} {
		if _, err := ReadResource(s, name); !errors.Is(err, ErrResourceNotFound) {
			t.Fatalf("ReadResource(%q) error = %v, want ErrResourceNotFound", name, err)
		}
	}
}

func loadManifestTestSkill(t *testing.T) Skill {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "resource-skill")
	writeTestSkill(t, dir, "resource-skill", "Read references only when needed.")
	writeResource := func(name, content string) {
		t.Helper()
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	writeResource("references/z.md", "secret reference")
	writeResource("scripts/run.sh", "echo safe")
	writeResource("assets/nested/a.txt", "asset")
	writeResource("other/custom.txt", "custom")
	writeResource("LICENSE.txt", "MIT")

	s, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	return s
}

func assertManifestResourceNames(t *testing.T, s Skill) {
	t.Helper()
	gotNames := make([]string, len(s.Resources))
	for i, resource := range s.Resources {
		gotNames[i] = resource.Name
	}
	wantNames := []string{"LICENSE.txt", "assets/nested/a.txt", "other/custom.txt", "references/z.md", "scripts/run.sh"}
	if strings.Join(gotNames, ",") != strings.Join(wantNames, ",") {
		t.Fatalf("resources = %v, want %v", gotNames, wantNames)
	}
}

func TestResource_RejectsSymlinkOversizeAndManifestReplacement(t *testing.T) {
	t.Run("symlink during load", testResourceSymlinkDuringLoad)
	t.Run("oversized resource", testResourceOversized)
	t.Run("file replaced by symlink", testResourceFileReplacedBySymlink)
	t.Run("file replaced by in-root symlink", testResourceFileReplacedByInRootSymlink)
	t.Run("parent replaced by symlink", testResourceParentReplacedBySymlink)
	t.Run("skill directory replaced by symlink", testResourceDirectoryReplacedBySymlink)
}

func testResourceSymlinkDuringLoad(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "links")
	writeTestSkill(t, dir, "links", "Body")
	if err := os.MkdirAll(filepath.Join(dir, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "references", "secret")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDir(dir); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("LoadDir symlink error = %v", err)
	}
}

func testResourceOversized(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "large")
	writeTestSkill(t, dir, "large", "Body")
	path := filepath.Join(dir, "assets", "large.bin")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxResourceBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDir(dir); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("LoadDir oversized error = %v", err)
	}
}

func testResourceFileReplacedBySymlink(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "replace")
	writeTestSkill(t, dir, "replace", "Body")
	path := filepath.Join(dir, "references", "data.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadResource(s, "references/data.txt"); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("ReadResource replaced symlink error = %v", err)
	}
}

func testResourceFileReplacedByInRootSymlink(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "replace-in-root")
	writeTestSkill(t, dir, "replace-in-root", "Body")
	resource := filepath.Join(dir, "references", "data.txt")
	if err := os.MkdirAll(filepath.Dir(resource), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resource, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(resource); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../SKILL.md", resource); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadResource(s, "references/data.txt"); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("ReadResource in-root symlink error = %v", err)
	}
}

func testResourceParentReplacedBySymlink(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "parent-replace")
	writeTestSkill(t, dir, "parent-replace", "Body")
	resourceDir := filepath.Join(dir, "references")
	if err := os.MkdirAll(resourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resourceDir, "data.txt"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(resourceDir, resourceDir+"-old"); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "data.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, resourceDir); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadResource(s, "references/data.txt"); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("ReadResource replaced parent error = %v", err)
	}
}

func testResourceDirectoryReplacedBySymlink(t *testing.T) {
	_, _, s := loadReplaceableResource(t, "root-replace")
	moved := s.SourceDir + "-old"
	if err := os.Rename(s.SourceDir, moved); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	if err := os.MkdirAll(filepath.Join(external, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, "references", "data.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, s.SourceDir); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadResource(s, "references/data.txt"); err == nil || !strings.Contains(err.Error(), "source directory changed") {
		t.Fatalf("ReadResource replaced skill root error = %v", err)
	}
}

func loadReplaceableResource(t *testing.T, name string) (string, string, Skill) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	writeTestSkill(t, dir, name, "Body")
	resource := filepath.Join(dir, "references", "data.txt")
	if err := os.MkdirAll(filepath.Dir(resource), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resource, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, resource, s
}

func TestResource_RegistryCopiesManifest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "copy")
	writeTestSkill(t, dir, "copy", "Body")
	path := filepath.Join(dir, "references", "data.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRegistry()
	if err := Register(r, s); err != nil {
		t.Fatal(err)
	}
	s.Resources[0].Name = "mutated"
	got, _ := r.Get("copy")
	if got.Resources[0].Name != "references/data.txt" {
		t.Fatalf("registered manifest mutated: %#v", got.Resources)
	}
	if content, err := ReadResource(got, "references/data.txt"); err != nil || string(content) != "data" {
		t.Fatalf("ReadResource copied manifest = %q, %v", content, err)
	}
}
