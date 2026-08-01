package project

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/conner-s/nfu/internal/config"
)

// indexFile is the mutable, human-readable manifest sitting beside the projects
// directory: the shared host catalog `add` writes to, plus an optional display
// order. It never gates which projects exist (the directory is the source of
// truth), so it can never drift.
const indexFile = "projects.toml"

const (
	projectHeader = "# nfu project definition — `nfu project edit <name>` to change it.\n\n"
	indexHeader   = "# nfu projects index — shared deploy hosts + display order.\n" +
		"# Managed by `nfu project add`; the projects/ directory is the source of truth.\n\n"
)

// FilePath returns the on-disk path of the named project's definition.
func FilePath(dir, name string) string { return filepath.Join(dir, name+".toml") }

// IndexPath returns the path of the mutable index beside the config.
func IndexPath(configDir string) string { return filepath.Join(configDir, indexFile) }

// Stored is one on-disk project definition and where it came from.
type Stored struct {
	Name     string
	Project  config.Project
	ReadOnly bool // a home-manager-generated file (a /nix/store symlink)
	Path     string
}

// Index is the mutable manifest file (projects.toml).
type Index struct {
	// Order lists project names in preferred display order; names absent from it
	// sort alphabetically after those present.
	Order []string `toml:"order,omitempty"`
	// Hosts is the shared deploy-host catalog written by `add`.
	Hosts map[string]config.Host `toml:"hosts,omitempty"`
}

// Scan reads every `*.toml` in dir as a project (name = filename stem). A
// missing directory is not an error — it just means no per-file projects yet.
// Results are sorted by name; ordering for display is applied later by the
// catalog using the index.
func Scan(dir string) ([]Stored, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Stored
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".toml") {
			continue
		}
		stem := strings.TrimSuffix(name, ".toml")
		path := filepath.Join(dir, name)
		proj, err := loadProjectFile(path)
		if err != nil {
			return nil, err
		}
		out = append(out, Stored{Name: stem, Project: proj, ReadOnly: isReadOnly(path), Path: path})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Save writes project p to dir/<name>.toml, creating dir as needed. It refuses
// to clobber a read-only (home-manager-declared) file, pointing the user at the
// declarative source instead.
func Save(dir, name string, p config.Project) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	path := FilePath(dir, name)
	if fileExists(path) && isReadOnly(path) {
		return fmt.Errorf("project %q is declared read-only (home-manager); edit programs.nfu.projects.%s, or use --print", name, name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := MarshalProject(p)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Remove deletes dir/<name>.toml. It refuses to remove a read-only
// (home-manager-declared) file.
func Remove(dir, name string) error {
	path := FilePath(dir, name)
	if !fileExists(path) {
		return fmt.Errorf("no project file for %q at %s", name, path)
	}
	if isReadOnly(path) {
		return fmt.Errorf("project %q is declared read-only (home-manager); remove it from programs.nfu.projects", name)
	}
	return os.Remove(path)
}

// LoadIndex reads the projects index beside the config. A missing index is an
// empty one, not an error.
func LoadIndex(configDir string) (*Index, error) {
	data, err := os.ReadFile(IndexPath(configDir))
	if err != nil {
		if os.IsNotExist(err) {
			return &Index{}, nil
		}
		return nil, err
	}
	var ix Index
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ix); err != nil {
		return nil, fmt.Errorf("parse %s: %w", indexFile, err)
	}
	return &ix, nil
}

// SaveIndex writes the projects index beside the config, creating the directory
// as needed. An empty index still writes (so an explicit `order = []` clears).
func SaveIndex(configDir string, ix *Index) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(ix)
	if err != nil {
		return err
	}
	return os.WriteFile(IndexPath(configDir), append([]byte(indexHeader), data...), 0o644)
}

// MarshalProject renders a project as the top-level TOML written to a per-file
// definition (name lives in the filename, so there is no wrapper table).
func MarshalProject(p config.Project) ([]byte, error) {
	body, err := toml.Marshal(p)
	if err != nil {
		return nil, err
	}
	return append([]byte(projectHeader), body...), nil
}

// ValidateName rejects names that would not be safe, unambiguous filenames.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("project name is empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("invalid project name %q", name)
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return fmt.Errorf("invalid project name %q: use letters, digits, '-', '_', '.'", name)
		}
	}
	return nil
}

// loadProjectFile parses one per-file project definition.
func loadProjectFile(path string) (config.Project, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return config.Project{}, err
	}
	var p config.Project
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return config.Project{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return p, nil
}

// isReadOnly reports whether path resolves into the Nix store — the signal that
// a project file was generated by the home-manager module and must not be
// rewritten or removed by nfu.
func isReadOnly(path string) bool {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	return strings.Contains(resolved, "/nix/store/")
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
