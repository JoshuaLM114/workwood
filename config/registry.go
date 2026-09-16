package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/JoshuaLM114/workwood/libs/fileio"
)

const RegistryFileName = "projects.yml"

// RegisteredProject links a project's committed identity to its local locations.
type RegisteredProject struct {
	ID      string `yaml:"id" json:"id"`
	Name    string `yaml:"name" json:"name"`
	Root    string `yaml:"root" json:"root"`
	DataDir string `yaml:"data_dir" json:"data_dir"`
}

type projectRegistry struct {
	Version  int                 `yaml:"version"`
	Projects []RegisteredProject `yaml:"projects"`
}

var registryMu sync.Mutex

// ListProjects reads the local registry. Missing registries contain no projects.
func ListProjects() ([]RegisteredProject, error) {
	home, err := Home()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(home, RegistryFileName))
	if os.IsNotExist(err) {
		return []RegisteredProject{}, nil
	}
	if err != nil {
		return nil, err
	}
	var registry projectRegistry
	if err := yaml.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("read project registry: %w", err)
	}
	if registry.Version > 1 {
		return nil, fmt.Errorf("project registry version %d requires a newer workwood", registry.Version)
	}
	if registry.Projects == nil {
		registry.Projects = []RegisteredProject{}
	}
	sort.Slice(registry.Projects, func(i, j int) bool { return registry.Projects[i].Root < registry.Projects[j].Root })
	return registry.Projects, nil
}

// RegisterProject validates and remembers a project without cloning or syncing it.
// Re-registering its UUID updates the local paths, including after a relocation.
func RegisterProject(path, dataDir string) (RegisteredProject, error) {
	loc, err := LocateProject(path)
	if err != nil {
		return RegisteredProject{}, err
	}
	if loc.DataDir != "" {
		dataDir = loc.DataDir
	}
	if dataDir == "" {
		return RegisteredProject{}, fmt.Errorf("data_dir is required to register this project")
	}
	dataDir, err = filepath.Abs(dataDir)
	if err != nil {
		return RegisteredProject{}, err
	}
	cfg, err := Build(loc.Root, loc.PD, dataDir)
	if err != nil {
		return RegisteredProject{}, err
	}
	entry := RegisteredProject{ID: cfg.ProjectID, Name: cfg.ProjectName, Root: cfg.Root, DataDir: cfg.DataDir}
	registryMu.Lock()
	defer registryMu.Unlock()
	unlock, err := lockRegistry(cfg.Home)
	if err != nil {
		return RegisteredProject{}, err
	}
	defer unlock()
	projects, err := ListProjects()
	if err != nil {
		return RegisteredProject{}, err
	}
	kept := projects[:0]
	for _, p := range projects {
		if p.ID != entry.ID && p.Root != entry.Root {
			kept = append(kept, p)
		}
	}
	kept = append(kept, entry)
	if err := os.MkdirAll(cfg.Home, 0o755); err != nil {
		return RegisteredProject{}, err
	}
	err = fileio.WriteYAML(filepath.Join(cfg.Home, RegistryFileName), projectRegistry{Version: 1, Projects: kept})
	return entry, err
}

// UnregisterProject forgets a UUID without touching the project or its data.
func UnregisterProject(id string) error {
	registryMu.Lock()
	defer registryMu.Unlock()
	home, err := Home()
	if err != nil {
		return err
	}
	unlock, err := lockRegistry(home)
	if err != nil {
		return err
	}
	defer unlock()
	projects, err := ListProjects()
	if err != nil {
		return err
	}
	kept := projects[:0]
	found := false
	for _, p := range projects {
		if p.ID == id {
			found = true
		} else {
			kept = append(kept, p)
		}
	}
	if !found {
		return fmt.Errorf("project %q is not registered", id)
	}
	return fileio.WriteYAML(filepath.Join(home, RegistryFileName), projectRegistry{Version: 1, Projects: kept})
}
