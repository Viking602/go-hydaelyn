package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscover_TrustedRootsPrecedenceAndStableOrder(t *testing.T) {
	root := t.TempDir()
	user := filepath.Join(root, "user")
	project := filepath.Join(root, "project")
	additional := filepath.Join(root, "additional")
	writeTestSkill(t, filepath.Join(user, ".agents", "skills", "shared"), "shared", "user")
	writeTestSkill(t, filepath.Join(user, defaultSkillDirectory, "skills", "zeta"), "zeta", "user zeta")
	writeTestSkill(t, filepath.Join(project, ".agents", "skills", "shared"), "shared", "project")
	writeTestSkill(t, filepath.Join(additional, "shared"), "shared", "additional")
	writeTestSkill(t, filepath.Join(additional, "alpha"), "alpha", "additional alpha")

	untrusted, err := Discover(DiscoveryOptions{UserDir: user, ProjectDir: project})
	if err != nil {
		t.Fatalf("Discover untrusted: %v", err)
	}
	if got := findSkill(t, untrusted.Skills, "shared").Body; !strings.Contains(got, "user") {
		t.Fatalf("untrusted project winner body = %q", got)
	}

	result, err := Discover(DiscoveryOptions{
		UserDir:        user,
		ProjectDir:     project,
		TrustProject:   true,
		AdditionalDirs: []string{additional},
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got, want := []string{result.Skills[0].Name, result.Skills[1].Name, result.Skills[2].Name}, []string{"alpha", "shared", "zeta"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("skill order = %v, want %v", got, want)
	}
	winner := findSkill(t, result.Skills, "shared")
	if !strings.Contains(winner.Body, "additional") {
		t.Fatalf("shared winner body = %q", winner.Body)
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("collision diagnostics = %#v, want 2", result.Diagnostics)
	}
	last := result.Diagnostics[len(result.Diagnostics)-1].Message
	if !strings.Contains(last, winner.SourcePath) || !strings.Contains(last, "shadows") {
		t.Fatalf("winner diagnostic = %q", last)
	}
}

func TestDiscover_ScansDirectChildrenAndBoundsRoots(t *testing.T) {
	root := t.TempDir()
	writeTestSkill(t, filepath.Join(root, "direct"), "direct", "direct")
	writeTestSkill(t, filepath.Join(root, "group", "nested"), "nested", "nested")
	result, err := Discover(DiscoveryOptions{AdditionalDirs: []string{root}})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(result.Skills) != 1 || result.Skills[0].Name != "direct" {
		t.Fatalf("skills = %#v, want direct only", result.Skills)
	}

	bounded := t.TempDir()
	for i := 0; i <= maxDiscoveryEntriesPerRoot; i++ {
		if err := os.WriteFile(filepath.Join(bounded, fmt.Sprintf("entry-%04d", i)), nil, 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	result, err = Discover(DiscoveryOptions{AdditionalDirs: []string{bounded}})
	if err != nil {
		t.Fatalf("Discover bounded root: %v", err)
	}
	if len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Message, "exceeds") {
		t.Fatalf("bounded diagnostics = %#v", result.Diagnostics)
	}
}

func TestDiscover_BoundsAdditionalDirsAndLoadedCandidates(t *testing.T) {
	t.Run("additional directories", func(t *testing.T) {
		_, err := Discover(DiscoveryOptions{AdditionalDirs: make([]string, maxAdditionalDirs+1)})
		if err == nil || !strings.Contains(err.Error(), "additional directories") {
			t.Fatalf("Discover additional directory limit error = %v", err)
		}
	})

	t.Run("candidates count before deduplication", func(t *testing.T) {
		roots := []string{t.TempDir(), t.TempDir()}
		for _, root := range roots {
			for i := 0; i <= maxDiscoveredSkills/2; i++ {
				name := fmt.Sprintf("skill-%04d", i)
				writeTestSkill(t, filepath.Join(root, name), name, "body")
			}
		}
		_, err := Discover(DiscoveryOptions{AdditionalDirs: roots})
		if err == nil || !strings.Contains(err.Error(), "skill candidates") {
			t.Fatalf("Discover candidate limit error = %v", err)
		}
	})
}

func TestDiscover_ReportsIgnoredLegacyRoot(t *testing.T) {
	user := t.TempDir()
	legacyRoot := filepath.Join(user, legacySkillDirectory, "skills")
	writeTestSkill(t, filepath.Join(legacyRoot, "legacy"), "legacy", "legacy")

	result, err := Discover(DiscoveryOptions{UserDir: user})
	if err != nil {
		t.Fatalf("Discover legacy root: %v", err)
	}
	if len(result.Skills) != 0 {
		t.Fatalf("legacy skills loaded = %#v, want none", result.Skills)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("legacy diagnostics = %#v, want one", result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	replacement := filepath.Join(user, defaultSkillDirectory, "skills")
	if diagnostic.Path != legacyRoot || !strings.Contains(diagnostic.Message, "ignored") || !strings.Contains(diagnostic.Message, replacement) {
		t.Fatalf("legacy diagnostic = %#v, want path %q and replacement %q", diagnostic, legacyRoot, replacement)
	}

	writeTestSkill(t, filepath.Join(replacement, "current"), "current", "current")
	result, err = Discover(DiscoveryOptions{UserDir: user})
	if err != nil {
		t.Fatalf("Discover migrated root: %v", err)
	}
	if len(result.Skills) != 1 || result.Skills[0].Name != "current" {
		t.Fatalf("migrated skills = %#v, want current only", result.Skills)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Path != legacyRoot {
		t.Fatalf("migrated diagnostics = %#v, want ignored legacy root", result.Diagnostics)
	}
}

func writeTestSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: Test " + name + "\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile SKILL.md: %v", err)
	}
}

func findSkill(t *testing.T, skills []Skill, name string) Skill {
	t.Helper()
	for _, s := range skills {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("skill %q not found", name)
	return Skill{}
}
