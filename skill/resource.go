package skill

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
)

const (
	maxResourceFiles = 256
	maxResourceBytes = 8 << 20
)

// Resource describes one bundled file available for explicit, read-only access.
type Resource struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// ErrResourceNotFound reports an exact resource name absent from a skill manifest.
var ErrResourceNotFound = errors.New("skill: resource not found")

// Activate renders one skill, including compatibility and its resource manifest.
func Activate(s Skill) string { return RenderSystemSection([]Skill{s}) }

// ReadResource reads an exact file from the manifest captured when the skill loaded.
func ReadResource(s Skill, name string) ([]byte, error) {
	if !hasResource(s.Resources, name) {
		return nil, fmt.Errorf("%w: %s", ErrResourceNotFound, name)
	}
	root, err := os.OpenRoot(s.SourceDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	currentInfo, err := root.Stat(".")
	if err != nil || s.sourceInfo == nil || !os.SameFile(s.sourceInfo, currentInfo) {
		return nil, fmt.Errorf("skill: unsafe resource %q: source directory changed", name)
	}
	return readVerifiedResource(root, name)
}

func hasResource(resources []Resource, name string) bool {
	for _, resource := range resources {
		if resource.Name == name {
			return true
		}
	}
	return false
}

func readVerifiedResource(root *os.Root, name string) ([]byte, error) {
	declaredInfo, err := lstatResource(root, name)
	if err != nil {
		return nil, fmt.Errorf("skill: unsafe resource %q: %w", name, err)
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("skill: unsafe resource %q: %w", name, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(declaredInfo, info) || info.Size() > maxResourceBytes {
		return nil, fmt.Errorf("skill: unsafe resource %q", name)
	}
	return readBounded(file, maxResourceBytes)
}

func lstatResource(root *os.Root, name string) (os.FileInfo, error) {
	parts := strings.Split(name, "/")
	var resourceInfo os.FileInfo
	for i := range parts {
		info, err := root.Lstat(path.Join(parts[:i+1]...))
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("symbolic links are not allowed")
		}
		if i < len(parts)-1 && !info.IsDir() {
			return nil, errors.New("parent is not a directory")
		}
		if i == len(parts)-1 && !info.Mode().IsRegular() {
			return nil, errors.New("resource is not a regular file")
		}
		resourceInfo = info
	}
	return resourceInfo, nil
}

func loadResourceManifest(s *Skill, root *os.Root) error {
	if s.SourceDir == "" {
		return nil
	}
	resources := make([]Resource, 0)
	err := fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		resource, include, err := manifestResource(path, entry, walkErr)
		if err != nil {
			return err
		}
		if !include {
			return nil
		}
		if len(resources) == maxResourceFiles {
			return fmt.Errorf("skill: resource manifest exceeds %d files", maxResourceFiles)
		}
		resources = append(resources, resource)
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(resources, func(i, j int) bool { return resources[i].Name < resources[j].Name })
	s.Resources = resources
	return nil
}

func manifestResource(path string, entry fs.DirEntry, walkErr error) (Resource, bool, error) {
	if walkErr != nil {
		return Resource{}, false, walkErr
	}
	if path == "." || path == "SKILL.md" || entry.IsDir() {
		return Resource{}, false, nil
	}
	if entry.Type()&os.ModeSymlink != 0 {
		return Resource{}, false, fmt.Errorf("skill: resource %q must not be a symlink", path)
	}
	info, err := entry.Info()
	if err != nil {
		return Resource{}, false, err
	}
	if !info.Mode().IsRegular() {
		return Resource{}, false, fmt.Errorf("skill: resource %q must be a regular file", path)
	}
	if info.Size() > maxResourceBytes {
		return Resource{}, false, fmt.Errorf("skill: resource %q exceeds 8 MiB", path)
	}
	return Resource{Name: path, Size: info.Size()}, true, nil
}

func cloneResources(in []Resource) []Resource {
	if len(in) == 0 {
		return nil
	}
	out := make([]Resource, len(in))
	copy(out, in)
	return out
}
