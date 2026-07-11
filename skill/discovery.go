package skill

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const (
	maxDiscoveryEntriesPerRoot = 2000
	maxDiscoveredSkills        = 2000
	maxAdditionalDirs          = 64
)

// DiscoveryOptions names every trusted location eligible for scanning.
// ProjectDir is ignored unless TrustProject is true.
type DiscoveryOptions struct {
	UserDir        string
	ProjectDir     string
	TrustProject   bool
	AdditionalDirs []string
}

// Diagnostic records a skipped skill or deterministic collision.
type Diagnostic struct {
	Path    string
	Message string
}

// DiscoveryResult contains the winning skill for each name and non-fatal diagnostics.
type DiscoveryResult struct {
	Skills      []Skill
	Diagnostics []Diagnostic
}

type discoveryRoot struct {
	path     string
	optional bool
}

// Discover scans direct skill children under explicit trusted roots. Later roots win.
func Discover(options DiscoveryOptions) (DiscoveryResult, error) {
	roots, err := discoveryRoots(options)
	if err != nil {
		return DiscoveryResult{}, err
	}

	byName := make(map[string]Skill)
	loadedCandidates := 0
	var diagnostics []Diagnostic
	for _, candidate := range roots {
		canonicalRoot, entries, diagnostic, err := readDiscoveryRoot(candidate)
		if err != nil {
			return DiscoveryResult{}, err
		}
		if diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		discovered, skipped := loadDiscoveryEntries(canonicalRoot, entries)
		diagnostics = append(diagnostics, skipped...)
		for _, s := range discovered {
			loadedCandidates++
			if loadedCandidates > maxDiscoveredSkills {
				return DiscoveryResult{}, fmt.Errorf("skill: discovery exceeds %d skill candidates", maxDiscoveredSkills)
			}
			if previous, ok := byName[s.Name]; ok {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    s.SourcePath,
					Message: fmt.Sprintf("skill %q from %s shadows %s", s.Name, s.SourcePath, previous.SourcePath),
				})
			}
			byName[s.Name] = s
		}
	}

	skills := make([]Skill, 0, len(byName))
	for _, s := range byName {
		skills = append(skills, cloneSkill(s))
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return DiscoveryResult{Skills: skills, Diagnostics: diagnostics}, nil
}

func loadDiscoveryEntries(root string, entries []os.DirEntry) ([]Skill, []Diagnostic) {
	var skills []Skill
	var diagnostics []Diagnostic
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		if _, err := os.Lstat(filepath.Join(dir, "SKILL.md")); errors.Is(err, os.ErrNotExist) {
			continue
		}
		s, err := LoadDir(dir)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: dir, Message: err.Error()})
			continue
		}
		skills = append(skills, s)
	}
	return skills, diagnostics
}

func discoveryRoots(options DiscoveryOptions) ([]discoveryRoot, error) {
	if len(options.AdditionalDirs) > maxAdditionalDirs {
		return nil, fmt.Errorf("skill: discovery exceeds %d additional directories", maxAdditionalDirs)
	}
	var roots []discoveryRoot
	appendConventional := func(base string) {
		if base == "" {
			return
		}
		roots = append(roots,
			discoveryRoot{path: filepath.Join(base, ".agents", "skills"), optional: true},
			discoveryRoot{path: filepath.Join(base, ".hydaelyn", "skills"), optional: true},
		)
	}
	appendConventional(options.UserDir)
	if options.TrustProject {
		appendConventional(options.ProjectDir)
	}
	for _, path := range options.AdditionalDirs {
		if path != "" {
			roots = append(roots, discoveryRoot{path: path})
		}
	}
	return roots, nil
}

func readDiscoveryRoot(candidate discoveryRoot) (string, []os.DirEntry, *Diagnostic, error) {
	absRoot, err := filepath.Abs(candidate.path)
	if err != nil {
		return "", nil, nil, err
	}
	canonicalRoot, err := filepath.EvalSymlinks(absRoot)
	if errors.Is(err, os.ErrNotExist) && candidate.optional {
		return "", nil, nil, nil
	}
	if err != nil {
		return "", nil, &Diagnostic{Path: absRoot, Message: err.Error()}, nil
	}
	directory, err := os.Open(canonicalRoot)
	if err != nil {
		return "", nil, &Diagnostic{Path: canonicalRoot, Message: err.Error()}, nil
	}
	entries, diagnostic := readRootEntries(directory, canonicalRoot)
	if diagnostic != nil {
		return "", nil, diagnostic, nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return canonicalRoot, entries, nil, nil
}

func readRootEntries(directory *os.File, path string) ([]os.DirEntry, *Diagnostic) {
	entries, readErr := directory.ReadDir(maxDiscoveryEntriesPerRoot + 1)
	closeErr := directory.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, &Diagnostic{Path: path, Message: readErr.Error()}
	}
	if closeErr != nil {
		return nil, &Diagnostic{Path: path, Message: closeErr.Error()}
	}
	if len(entries) > maxDiscoveryEntriesPerRoot {
		return nil, &Diagnostic{Path: path, Message: fmt.Sprintf("discovery root exceeds %d entries", maxDiscoveryEntriesPerRoot)}
	}
	return entries, nil
}
