package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/models"
	"github.com/JoshuaLM114/workwood/projectdef"
)

// SetupVersion identifies the local initialization steps, independently of the
// software release and YAML schema. Increment it when existing setups need init.
const SetupVersion = 1

type SetupIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

// InitArguments can be passed directly to the project_init MCP tool.
type InitArguments struct {
	Path    string `json:"path"`
	DataDir string `json:"data_dir,omitempty"`
}

// ProjectStatus describes local setup readiness without changing any files.
// Ready means metadata and discovery are current; source repos may need pulling.
type ProjectStatus struct {
	Found                bool           `json:"found"`
	Status               string         `json:"status"`
	ID                   string         `json:"id,omitempty"`
	Name                 string         `json:"name,omitempty"`
	Root                 string         `json:"root,omitempty"`
	DataDir              string         `json:"data_dir,omitempty"`
	DataDirSource        string         `json:"data_dir_source,omitempty"`
	ActiveFeature        string         `json:"active_feature,omitempty"`
	Registered           bool           `json:"registered"`
	SetupVersion         int            `json:"setup_version"`
	RequiredSetupVersion int            `json:"required_setup_version"`
	Issues               []SetupIssue   `json:"issues"`
	InitArguments        *InitArguments `json:"init_arguments,omitempty"`
}

// DetectProject finds an existing project from a path, registered UUID or cwd,
// including definitions that predate project identities. It never registers,
// initializes, clones, or scans beyond the path's ancestors and project metadata.
func DetectProject(project, dataDir string) (report *ProjectStatus, err error) {
	report = &ProjectStatus{Status: "not_found", RequiredSetupVersion: SetupVersion, Issues: []SetupIssue{}}
	defer func() {
		if err != nil {
			report.Status = "blocked"
			report.InitArguments = nil
			report.Issues = append(report.Issues, SetupIssue{Code: "setup_error", Message: err.Error()})
		}
	}()
	registered, err := ListProjects()
	if err != nil {
		return report, err
	}
	expectedID := ""
	for _, p := range registered {
		if project == p.ID {
			project, expectedID = p.Root, p.ID
			break
		}
	}
	if project == "" {
		project = "."
	}
	start, err := filepath.Abs(project)
	if err != nil {
		return report, err
	}
	root, linkedData, feature, err := findProject(start)
	if err != nil {
		return report, err
	}
	if root == "" {
		if expectedID != "" {
			return report, fmt.Errorf("registered project %s is missing at %s", expectedID, start)
		}
		return report, nil
	}
	report.Found, report.Root, report.ActiveFeature = true, root, feature
	pd, err := projectdef.Load(filepath.Join(root, ProjectDefName))
	if err != nil {
		return report, err
	}
	report.ID, report.Name = pd.ID, projectSlug(pd, root)
	if expectedID != "" && pd.ID != expectedID {
		return report, fmt.Errorf("registered project %s has a different identity at %s; register its current location", expectedID, root)
	}
	report.Status = "needs_init"
	report.InitArguments = &InitArguments{Path: root}
	if pd.ID == "" {
		report.Issues = append(report.Issues, SetupIssue{Code: "project_identity_missing", Message: "The project definition needs a shared identity.", Path: filepath.Join(root, ProjectDefName)})
	}
	if pd.Name == "" {
		report.Issues = append(report.Issues, SetupIssue{Code: "project_name_missing", Message: "The project definition needs a name.", Path: filepath.Join(root, ProjectDefName)})
	}
	for _, dir := range []string{ActionsDirName, ManifestsDirName} {
		path := filepath.Join(root, WorkwoodDirName, dir)
		info, e := os.Stat(path)
		if os.IsNotExist(e) {
			report.Issues = append(report.Issues, SetupIssue{Code: "directory_missing", Message: "Initialization needs to create this metadata directory.", Path: path})
		} else if e != nil {
			return report, e
		} else if !info.IsDir() {
			return report, fmt.Errorf("metadata path %s is not a directory", path)
		}
	}
	mans, err := initManifests(filepath.Join(root, WorkwoodDirName, ManifestsDirName), pd.ID)
	if err != nil {
		return report, err
	}
	for _, m := range mans {
		if m.ID == "" || m.Project == "" {
			report.Issues = append(report.Issues, SetupIssue{Code: "feature_identity_missing", Message: "The feature needs its identity or project link filled in.", Path: filepath.Join(root, WorkwoodDirName, ManifestsDirName, m.Feature+".yaml")})
		}
	}
	report.DataDir, report.DataDirSource, err = resolveSetupData(linkedData, dataDir, pd.ID, registered)
	if err != nil {
		return report, err
	}
	if report.DataDir == "" {
		report.Status = "needs_data_dir"
		report.Issues = append(report.Issues, SetupIssue{Code: "data_dir_missing", Message: "Supply the existing project's data directory to check or initialize its local setup."})
		return report, nil
	}
	report.InitArguments.DataDir = report.DataDir
	for _, p := range registered {
		if pd.ID != "" && p.ID == pd.ID && p.Root == root && p.DataDir == report.DataDir {
			report.Registered = true
			break
		}
	}
	if !report.Registered {
		report.Issues = append(report.Issues, SetupIssue{Code: "registration_missing", Message: "Initialization needs to register this project's current root and data directory."})
	}
	cfg, err := Build(root, pd, report.DataDir)
	if err != nil {
		return report, err
	}
	report.Name = cfg.ProjectName
	st, err := LoadState(cfg.StateFile)
	if err != nil {
		return report, err
	}
	report.SetupVersion = st.SetupVersion
	if st.SetupVersion > SetupVersion {
		return report, fmt.Errorf("setup version %d requires a newer workwood (supported: %d)", st.SetupVersion, SetupVersion)
	}
	if st.SetupVersion < SetupVersion {
		report.Issues = append(report.Issues, SetupIssue{Code: "setup_outdated", Message: "Run initialization to complete the current setup generation.", Path: cfg.StateFile})
	}
	if st.Project == "" {
		report.Issues = append(report.Issues, SetupIssue{Code: "state_identity_missing", Message: "Local state needs its project identity filled in.", Path: cfg.StateFile})
	}
	for _, m := range mans {
		if fs, ok := st.Features[m.ID]; m.ID == "" || !ok || fs.Slug != m.Feature {
			report.Issues = append(report.Issues, SetupIssue{Code: "feature_state_missing", Message: "Local state needs to track feature " + m.Feature + ".", Path: cfg.StateFile})
		}
		info, e := os.Stat(cfg.FeatureDir(m.Feature))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return report, e
		}
		if !info.IsDir() {
			return report, fmt.Errorf("feature path %s is not a directory", cfg.FeatureDir(m.Feature))
		}
		if link, e := ReadFeatureLink(cfg.FeatureLinkPath(m.Feature)); e == nil && link.Version > FeatureLinkVersion {
			return report, fmt.Errorf("feature link %s requires a newer workwood", cfg.FeatureLinkPath(m.Feature))
		}
		if !FeatureLinkValid(cfg, m.Feature) {
			report.Issues = append(report.Issues, SetupIssue{Code: "feature_link_outdated", Message: "The existing feature folder needs its local back-link refreshed.", Path: cfg.FeatureLinkPath(m.Feature)})
		}
	}
	if len(report.Issues) == 0 {
		report.Status, report.InitArguments = "ready", nil
	}
	return report, nil
}

func resolveSetupData(linked, explicit, id string, registered []RegisteredProject) (string, string, error) {
	data, source := linked, "feature_link"
	if data == "" {
		data, source = explicit, "argument"
	}
	if data == "" && id != "" {
		for _, p := range registered {
			if p.ID == id {
				data, source = p.DataDir, "registry"
				break
			}
		}
	}
	if data == "" {
		data, source = os.Getenv(EnvData), "environment"
	}
	if data == "" {
		app, _, err := LoadApp()
		if err != nil {
			return "", "", err
		}
		data, source = app.DataDir, "settings"
	}
	if data == "" {
		return "", "", nil
	}
	abs, err := filepath.Abs(data)
	return abs, source, err
}

// initManifests validates the identity and destination of every manifest before
// initialization can rewrite one or create feature back-links from its slug.
func initManifests(dir, projectID string) ([]*models.Manifest, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	mans := []*models.Manifest{}
	ids := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		m, err := manifest.Load(path)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(m.Feature) == "" || m.Feature == "." || m.Feature == ".." || strings.ContainsAny(m.Feature, "/\\\x00\r\n") || entry.Name() != m.Feature+".yaml" {
			return nil, fmt.Errorf("manifest %s must use a feature slug matching its filename", path)
		}
		if m.Project != "" && m.Project != projectID {
			return nil, fmt.Errorf("manifest %s belongs to project %s, not %s", path, m.Project, projectID)
		}
		if m.ID != "" {
			if other, ok := ids[m.ID]; ok {
				return nil, fmt.Errorf("features %s and %s share identity %s", other, m.Feature, m.ID)
			}
			ids[m.ID] = m.Feature
		}
		mans = append(mans, m)
	}
	return mans, nil
}

type ProjectInitialization struct {
	Project           RegisteredProject `json:"project"`
	Initialized       *InitResult       `json:"initialized,omitempty"`
	DefinitionChanged bool              `json:"definition_changed"`
	Status            *ProjectStatus    `json:"status,omitempty"`
}

// InitializeProject upgrades a detected project or scaffolds an explicit path,
// then registers it. Existing project names, identities and repo lists survive.
func InitializeProject(path, dataDir, name string) (*ProjectInitialization, error) {
	status, err := DetectProject(path, dataDir)
	if err != nil {
		return nil, err
	}
	root := status.Root
	pd := &models.ProjectDef{Name: name, Repos: []models.Repo{}}
	changed := true
	if status.Found {
		dataDir = status.DataDir
		pd, err = projectdef.Load(filepath.Join(root, ProjectDefName))
		if err != nil {
			return nil, err
		}
		changed = pd.ID == "" || pd.Name == ""
	} else {
		if strings.TrimSpace(path) == "" {
			return nil, fmt.Errorf("no existing project detected; provide path to initialize a new project")
		}
		root, err = filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		dataDir, _, err = resolveSetupData("", dataDir, "", nil)
		if err != nil {
			return nil, err
		}
	}
	if dataDir == "" {
		return nil, fmt.Errorf("data_dir is required; supply the existing data directory or set WORKWOOD_DATA")
	}
	// Check existing state before generating an identity or writing a definition.
	if _, err := Build(root, pd, dataDir); err != nil {
		return nil, err
	}
	if pd.ID == "" {
		pd.ID = uuid.NewString()
	}
	if pd.Name == "" {
		pd.Name = filepath.Base(root)
	}
	if _, _, _, err := prepareInit(root, pd, dataDir); err != nil {
		return nil, err
	}
	result := &ProjectInitialization{DefinitionChanged: changed}
	if changed {
		if err := os.MkdirAll(root, 0o755); err != nil {
			return result, err
		}
		if err := projectdef.Save(filepath.Join(root, ProjectDefName), pd); err != nil {
			return result, err
		}
	}
	result.Initialized, err = InitProject(root, pd, dataDir)
	if err != nil {
		return result, err
	}
	result.Project, err = RegisterProject(root, dataDir)
	if err != nil {
		return result, fmt.Errorf("project initialized, registration failed: %w", err)
	}
	result.Status, err = DetectProject(root, dataDir)
	return result, err
}
