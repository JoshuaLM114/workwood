package menus

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/huh"
	"github.com/stretchr/testify/require"

	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/models"
	"github.com/JoshuaLM114/workwood/superfeature"
)

func TestValidateAddChecksQueuedBranchesPerRepo(t *testing.T) {
	i18n.Init("en")
	cfg := &models.Config{MainDir: t.TempDir()}
	for _, repo := range []string{"api", "web"} {
		require.NoError(t, os.MkdirAll(cfg.BaseRepo(repo), 0o755))
		out, err := exec.Command("git", "-C", cfg.BaseRepo(repo), "init", "-q", "-b", "main").CombinedOutput()
		require.NoError(t, err, string(out))
	}
	e := EditorModel{
		ctx: Ctx{Cfg: cfg}, man: &models.Manifest{Feature: "demo", Shorthand: "d"},
		rows: []editorRow{{kind: rowStagedAdd, add: superfeature.AddSpec{Repo: "api", Sub: "topic"}, addBranch: "d/topic"}},
	}
	for _, tc := range []struct {
		name, repo, sub string
		wantErr         bool
	}{
		{"same repo and branch", "api", "topic", true},
		{"already prefixed", "api", "d/topic", true},
		{"another branch", "api", "other", false},
		{"another repo", "web", "topic", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := e.validateAdd(superfeature.AddSpec{Repo: tc.repo, Sub: tc.sub})
			if tc.wantErr {
				require.ErrorContains(t, err, "already has a worktree")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNewBranchFormValidatesSelectedPlacement(t *testing.T) {
	i18n.Init("en")
	v := addVals{repo: "api", sub: "topic"}
	var checked []bool
	f := newAddForm("d", "main", superfeature.BaseStatus{Base: "main", LocalExists: true, OriginExists: true}, &v, func(sub string, omit bool) error {
		require.Equal(t, "topic", sub)
		checked = append(checked, omit)
		if !omit {
			return i18n.Err("err.branch_exists", "d/topic", "api")
		}
		return nil
	})
	// The name field lets the user reach placement even when the nested name exists.
	f.GetFocusedField().Blur()
	require.NoError(t, f.GetFocusedField().Error())
	f.NextField()
	placement, ok := f.GetFocusedField().(*huh.Select[bool])
	require.True(t, ok)
	placement.Blur()
	require.ErrorContains(t, placement.Error(), "already exists")
	v.omitPrefix = true
	placement.Blur()
	require.NoError(t, placement.Error())
	require.Contains(t, checked, false)
	require.Contains(t, checked, true)
}

func TestNewBranchModeOpensValidatedFormAfterFetch(t *testing.T) {
	i18n.Init("en")
	e := EditorModel{
		ctx:     Ctx{Cfg: &models.Config{MainDir: filepath.Join(t.TempDir(), "main")}},
		man:     &models.Manifest{Feature: "demo", Shorthand: "d"},
		addVals: addVals{repo: "api"}, width: 100,
	}
	_, _ = e.Update(editorBranchesMsg{repo: "api", base: superfeature.BaseStatus{Base: "main", LocalExists: true, OriginExists: true}})
	require.Equal(t, formAdd, e.formMode)
	require.NotNil(t, e.form)
	require.Equal(t, superfeature.BaseSourceOrigin, e.addVals.baseSource)
}

func TestNewBranchFetchFailureStopsBeforeForm(t *testing.T) {
	i18n.Init("en")
	e := EditorModel{addVals: addVals{repo: "api"}, width: 100}
	_, _ = e.Update(editorBranchesMsg{repo: "api", err: os.ErrNotExist})
	require.Equal(t, formNone, e.formMode)
	require.Nil(t, e.form)
	require.Contains(t, e.status, os.ErrNotExist.Error())
}
