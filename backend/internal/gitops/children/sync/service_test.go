package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/types/v2/gitops"
	"github.com/getarcaneapp/arcane/types/v2/swarm"
	"github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitrepo"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	projectpkg "github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/gitutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
)

func setupSyncTestServiceInternal(t *testing.T) (*Service, *database.DB, string) {
	t.Helper()

	ctx := t.Context()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&projectpkg.Project{}, &projectpkg.GitOpsSync{}, &settings.SettingVariable{}, &imageupdate.ImageUpdateRecord{}, &event.Event{}))
	arcaneDB := &database.DB{DB: db}

	settingsService, err := settings.NewSettingsService(ctx, arcaneDB)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })

	projectsDir := t.TempDir()
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	eventService := event.NewEventService(arcaneDB, config.Load(), nil)
	projectService := projectpkg.NewProjectService(arcaneDB, settingsService, eventService, nil, nil, nil, nil, nil, config.Load(), nil, nil)

	limits := func(_ context.Context, sync *projectpkg.GitOpsSync) (int, int64, int64) {
		return sync.MaxSyncFiles, sync.MaxSyncTotalSize, sync.MaxSyncBinarySize
	}
	logError := func(context.Context, *projectpkg.GitOpsSync, user.Actor, string) {}
	unregisterJob := func(context.Context, string) {}
	return New(arcaneDB, nil, projectService, nil, eventService, limits, logError, unregisterJob), arcaneDB, projectsDir
}

func writeFileInternal(t *testing.T, rootDir, relativePath string, content []byte) {
	t.Helper()

	targetPath := filepath.Join(rootDir, relativePath)
	require.NoError(t, os.MkdirAll(filepath.Dir(targetPath), 0o755))
	require.NoError(t, os.WriteFile(targetPath, content, 0o644))
}

// TestGitOpsSyncService_SyncProjectDirectory_RefusesDuplicateOnNameCollision verifies a
// directory sync refuses to create a "-N" sibling when its target name is already taken
// by a non-adoptable directory; instead it errors as a broken binding and disables auto-sync.
func TestGitOpsSyncService_SyncProjectDirectory_RefusesDuplicateOnNameCollision(t *testing.T) {
	ctx := t.Context()
	svc, db, projectsDir := setupSyncTestServiceInternal(t)

	// Occupy the target name with a dir that is NOT an adoptable GitOps project
	// (it has no matching compose file).
	require.NoError(t, os.MkdirAll(filepath.Join(projectsDir, "Dozzle"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectsDir, "Dozzle", "unrelated.txt"), []byte("x"), 0o644))

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-dup-refuse",
		Name:          "Dozzle",
		EnvironmentID: "0",
		ComposePath:   "Dozzle/docker-compose.yaml",
		ProjectName:   "Dozzle",
		SyncDirectory: true,
		AutoSync:      true,
		SyncInterval:  60,
	}
	require.NoError(t, db.Create(sync).Error)

	syncFiles := []projects.SyncFile{
		{RelativePath: "docker-compose.yaml", Content: []byte("services:\n  app:\n    image: nginx:alpine\n")},
	}

	_, _, _, _, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, "", user.Actor{})
	require.ErrorIs(t, err, common.ErrGitOpsSyncProjectBindingBroken)

	_, statErr := os.Stat(filepath.Join(projectsDir, "Dozzle-1"))
	require.ErrorIs(t, statErr, os.ErrNotExist, "must not mint a -N duplicate")

	var got projectpkg.GitOpsSync
	require.NoError(t, db.Where("id = ?", sync.ID).First(&got).Error)
	assert.False(t, got.AutoSync, "auto-sync should be disabled on broken binding")
}

func TestGitOpsSyncService_SyncProjectDirectory_CreatesProjectPreservingRepoLayout(t *testing.T) {
	ctx := t.Context()
	svc, db, _ := setupSyncTestServiceInternal(t)

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-directory-create",
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project",
		SyncDirectory: true,
	}
	require.NoError(t, db.Create(sync).Error)

	gitEnvContent := "# keep git formatting\r\nZ_LAST=last\r\nCLOUDFLARE_CLIENT_SECRET=$$pbkdf2-sha512$$310000$$XXX\r\nQUOTED_SECRET='$pbkdf2-sha512$310000$XXX'\r\nA_FIRST=first"
	syncFiles := []projects.SyncFile{
		{
			RelativePath: "docker-compose.yaml",
			Content: []byte(`include:
  - meta.yaml
services:
  app:
    image: nginx:alpine
    env_file:
      - .env
`),
		},
		{
			RelativePath: "meta.yaml",
			Content: []byte(`services:
  helper:
    image: busybox:latest
`),
		},
		{
			RelativePath: ".env",
			Content:      []byte(gitEnvContent),
		},
	}

	project, syncedFiles, created, changed, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, "", user.Actor{})
	require.NoError(t, err)
	require.NotNil(t, project)
	require.True(t, created)
	require.True(t, changed)
	// .env is a reserved root env file: it is routed through the override
	// merge rather than tracked as a raw synced file.
	require.ElementsMatch(t, []string{"docker-compose.yaml", "meta.yaml"}, syncedFiles)

	composePath, detectErr := projects.DetectComposeFile(t.Context(), "", project.Path)
	require.NoError(t, detectErr)
	assert.Equal(t, filepath.Join(project.Path, "docker-compose.yaml"), composePath)

	composeBytes, err := os.ReadFile(filepath.Join(project.Path, "docker-compose.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(composeBytes), "include:")

	metaBytes, err := os.ReadFile(filepath.Join(project.Path, "meta.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(metaBytes), "helper:")

	envBytes, err := os.ReadFile(filepath.Join(project.Path, ".env"))
	require.NoError(t, err)
	assert.Equal(t, gitEnvContent, string(envBytes))

	gitEnvBytes, err := os.ReadFile(filepath.Join(project.Path, ".env.git"))
	require.NoError(t, err)
	assert.Equal(t, gitEnvContent, string(gitEnvBytes))

	parsedEnv, err := projects.ParseProjectEnvFile(filepath.Join(project.Path, ".env"), nil)
	require.NoError(t, err)
	assert.Equal(t, "$pbkdf2-sha512$310000$XXX", parsedEnv["CLOUDFLARE_CLIENT_SECRET"])
	assert.Equal(t, "$pbkdf2-sha512$310000$XXX", parsedEnv["QUOTED_SECRET"])

	_, statErr := os.Stat(filepath.Join(project.Path, "compose.yaml"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestGitOpsSyncService_SyncProjectDirectory_UpdatesProjectAndCleansOldSyncedFiles(t *testing.T) {
	ctx := t.Context()
	svc, db, projectsDir := setupSyncTestServiceInternal(t)

	projectPath := filepath.Join(projectsDir, "demo-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectPath, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "docker-compose.yaml"), []byte(`include:
  - meta.yaml
services:
  app:
    image: nginx:1.26-alpine
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "meta.yaml"), []byte(`services:
  helper:
    image: busybox:1.36
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "old.txt"), []byte("remove me\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "keep.txt"), []byte("keep me\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services: {}\n"), 0o644))

	project := &projectpkg.Project{
		ID:      "proj-directory-update",
		Name:    "demo-project",
		DirName: new("demo-project"),
		Path:    projectPath,
		Status:  projectpkg.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	oldSyncedFilesJSON, err := json.Marshal([]string{"docker-compose.yaml", "meta.yaml", "old.txt"})
	require.NoError(t, err)

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-directory-update",
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project",
		ProjectID:     &project.ID,
		SyncDirectory: true,
		SyncedFiles:   new(string(oldSyncedFilesJSON)),
	}
	require.NoError(t, db.Create(sync).Error)

	syncFiles := []projects.SyncFile{
		{
			RelativePath: "docker-compose.yaml",
			Content: []byte(`include:
  - nested/feature.yaml
services:
  app:
    image: nginx:1.27-alpine
`),
		},
		{
			RelativePath: "nested/feature.yaml",
			Content: []byte(`services:
  worker:
    image: busybox:latest
`),
		},
	}

	keepBefore, err := os.Stat(filepath.Join(projectPath, "keep.txt"))
	require.NoError(t, err)

	updatedProject, syncedFiles, created, changed, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, "", user.Actor{})
	require.NoError(t, err)
	require.NotNil(t, updatedProject)
	require.False(t, created)
	require.True(t, changed)
	require.ElementsMatch(t, []string{"docker-compose.yaml", "nested/feature.yaml"}, syncedFiles)

	composePath, detectErr := projects.DetectComposeFile(t.Context(), "", updatedProject.Path)
	require.NoError(t, detectErr)
	assert.Equal(t, filepath.Join(updatedProject.Path, "docker-compose.yaml"), composePath)

	_, statErr := os.Stat(filepath.Join(updatedProject.Path, "old.txt"))
	require.ErrorIs(t, statErr, os.ErrNotExist)

	_, statErr = os.Stat(filepath.Join(updatedProject.Path, "compose.yaml"))
	require.ErrorIs(t, statErr, os.ErrNotExist)

	// An untracked sibling is outside the sync's scope: never copied, rewritten or pruned.
	keepAfter, err := os.Stat(filepath.Join(updatedProject.Path, "keep.txt"))
	require.NoError(t, err)
	assert.True(t, os.SameFile(keepBefore, keepAfter))
	assert.Equal(t, keepBefore.ModTime(), keepAfter.ModTime())
	keepBytes, err := os.ReadFile(filepath.Join(updatedProject.Path, "keep.txt"))
	require.NoError(t, err)
	assert.Equal(t, "keep me\n", string(keepBytes))

	featureBytes, err := os.ReadFile(filepath.Join(updatedProject.Path, "nested", "feature.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(featureBytes), "worker:")
}

// TestGitOpsSyncService_SyncProjectDirectory_PreservesEnvOverrideAndAddsNewGitKey
// verifies directory sync routes the project-root .env through the same
// three-file override merge single-file git sync uses: an edit made in Arcane
// (recorded as project.env) survives the sync, while a new key introduced by
// git still flows into the effective .env. This is the core regression test
// for https://github.com/getarcaneapp/arcane/issues/2476.
func TestGitOpsSyncService_SyncProjectDirectory_PreservesEnvOverrideAndAddsNewGitKey(t *testing.T) {
	ctx := t.Context()
	svc, db, projectsDir := setupSyncTestServiceInternal(t)

	projectPath := filepath.Join(projectsDir, "demo-project")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "docker-compose.yaml"), []byte(`services:
  app:
    image: nginx:1.26-alpine
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env.git"), []byte("FOO=git\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "project.env"), []byte("FOO=useredit\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("FOO=useredit\n"), 0o644))

	project := &projectpkg.Project{
		ID:      "proj-directory-env-preserve",
		Name:    "demo-project",
		DirName: new("demo-project"),
		Path:    projectPath,
		Status:  projectpkg.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	oldSyncedFilesJSON, err := json.Marshal([]string{"docker-compose.yaml"})
	require.NoError(t, err)

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-directory-env-preserve",
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project",
		ProjectID:     &project.ID,
		SyncDirectory: true,
		SyncedFiles:   new(string(oldSyncedFilesJSON)),
	}
	require.NoError(t, db.Create(sync).Error)

	newGitEnvContent := "FOO=gitnew\nBAR=new\n"
	syncFiles := []projects.SyncFile{
		{
			RelativePath: "docker-compose.yaml",
			Content: []byte(`services:
  app:
    image: nginx:1.27-alpine
`),
		},
		{
			RelativePath: ".env",
			Content:      []byte(newGitEnvContent),
		},
	}

	updatedProject, syncedFiles, created, changed, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, "", user.Actor{})
	require.NoError(t, err)
	require.NotNil(t, updatedProject)
	require.False(t, created)
	require.True(t, changed)
	require.ElementsMatch(t, []string{"docker-compose.yaml"}, syncedFiles)

	effectiveBytes, err := os.ReadFile(filepath.Join(updatedProject.Path, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "FOO=useredit\nBAR=new\n", string(effectiveBytes))

	gitBytes, err := os.ReadFile(filepath.Join(updatedProject.Path, ".env.git"))
	require.NoError(t, err)
	assert.Equal(t, newGitEnvContent, string(gitBytes))

	overrideBytes, err := os.ReadFile(filepath.Join(updatedProject.Path, "project.env"))
	require.NoError(t, err)
	assert.Equal(t, "FOO=useredit\n", string(overrideBytes))

	effectiveEnv, err := projects.ParseProjectEnvFile(filepath.Join(updatedProject.Path, ".env"), nil)
	require.NoError(t, err)
	assert.Equal(t, projects.EnvMap{"FOO": "useredit", "BAR": "new"}, effectiveEnv)

	gitEnv, err := projects.ParseProjectEnvFile(filepath.Join(updatedProject.Path, ".env.git"), nil)
	require.NoError(t, err)
	assert.Equal(t, projects.EnvMap{"FOO": "gitnew", "BAR": "new"}, gitEnv)

	overrideEnv, err := projects.ParseProjectEnvFile(filepath.Join(updatedProject.Path, "project.env"), nil)
	require.NoError(t, err)
	assert.Equal(t, projects.EnvMap{"FOO": "useredit"}, overrideEnv)
}

// TestGitOpsSyncService_SyncProjectDirectory_MigratesLegacyTrackedEnvOnFirstSyncAfterUpgrade
// verifies the first directory sync after upgrading from a pre-override-merge
// Arcane version: sync.SyncedFiles still lists ".env" from the old raw-write
// path, and the project has only a direct .env (no .env.git/project.env yet).
// CleanupRemovedFiles must not delete the live-copied stage .env before the
// merge step reads it, or a local-only key would be silently dropped instead
// of migrated into project.env.
func TestGitOpsSyncService_SyncProjectDirectory_MigratesLegacyTrackedEnvOnFirstSyncAfterUpgrade(t *testing.T) {
	ctx := t.Context()
	svc, db, projectsDir := setupSyncTestServiceInternal(t)

	projectPath := filepath.Join(projectsDir, "demo-project")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "docker-compose.yaml"), []byte(`services:
  app:
    image: nginx:1.26-alpine
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("LOCAL_ONLY=1\n"), 0o644))

	project := &projectpkg.Project{
		ID:      "proj-directory-legacy-env-migrate",
		Name:    "demo-project",
		DirName: new("demo-project"),
		Path:    projectPath,
		Status:  projectpkg.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	// A pre-fix sync recorded .env as a plain tracked file.
	oldSyncedFilesJSON, err := json.Marshal([]string{"docker-compose.yaml", ".env"})
	require.NoError(t, err)

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-directory-legacy-env-migrate",
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project",
		ProjectID:     &project.ID,
		SyncDirectory: true,
		SyncedFiles:   new(string(oldSyncedFilesJSON)),
	}
	require.NoError(t, db.Create(sync).Error)

	syncFiles := []projects.SyncFile{
		{
			RelativePath: "docker-compose.yaml",
			Content: []byte(`services:
  app:
    image: nginx:1.27-alpine
`),
		},
		{
			RelativePath: ".env",
			Content:      []byte("BASE=git\n"),
		},
	}

	updatedProject, _, created, _, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, "", user.Actor{})
	require.NoError(t, err)
	require.NotNil(t, updatedProject)
	require.False(t, created)

	effectiveEnv, err := projects.ParseProjectEnvFile(filepath.Join(updatedProject.Path, ".env"), nil)
	require.NoError(t, err)
	assert.Equal(t, projects.EnvMap{"BASE": "git", "LOCAL_ONLY": "1"}, effectiveEnv)

	overrideEnv, err := projects.ParseProjectEnvFile(filepath.Join(updatedProject.Path, "project.env"), nil)
	require.NoError(t, err)
	assert.Equal(t, projects.EnvMap{"LOCAL_ONLY": "1"}, overrideEnv)
}

// TestGitOpsSyncService_SyncProjectDirectory_IgnoresCommittedReservedEnvFiles verifies
// that a repo committing files at the reserved bookkeeping paths (.env.git,
// project.env) cannot clobber Arcane's own override-merge bookkeeping: those paths
// are dropped from the raw sync write, never tracked in syncedFiles, and the actual
// .env.git/project.env on disk are still produced solely by the merge.
func TestGitOpsSyncService_SyncProjectDirectory_IgnoresCommittedReservedEnvFiles(t *testing.T) {
	ctx := t.Context()
	svc, db, _ := setupSyncTestServiceInternal(t)

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-directory-reserved-env",
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project-reserved",
		SyncDirectory: true,
	}
	require.NoError(t, db.Create(sync).Error)

	syncFiles := []projects.SyncFile{
		{
			RelativePath: "docker-compose.yaml",
			Content: []byte(`services:
  app:
    image: nginx:alpine
`),
		},
		{
			RelativePath: ".env",
			Content:      []byte("FOO=fromgit\n"),
		},
		{
			RelativePath: ".env.git",
			Content:      []byte("poison\n"),
		},
		{
			RelativePath: "project.env",
			Content:      []byte("poison\n"),
		},
	}

	project, syncedFiles, created, _, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, "", user.Actor{})
	require.NoError(t, err)
	require.NotNil(t, project)
	require.True(t, created)
	require.ElementsMatch(t, []string{"docker-compose.yaml"}, syncedFiles)

	effectiveEnv, err := projects.ParseProjectEnvFile(filepath.Join(project.Path, ".env"), nil)
	require.NoError(t, err)
	assert.Equal(t, projects.EnvMap{"FOO": "fromgit"}, effectiveEnv)

	gitBytes, err := os.ReadFile(filepath.Join(project.Path, ".env.git"))
	require.NoError(t, err)
	assert.NotContains(t, string(gitBytes), "poison")
	gitEnv, err := projects.ParseProjectEnvFile(filepath.Join(project.Path, ".env.git"), nil)
	require.NoError(t, err)
	assert.Equal(t, projects.EnvMap{"FOO": "fromgit"}, gitEnv)

	_, statErr := os.Stat(filepath.Join(project.Path, "project.env"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestGitOpsSyncService_DirectorySync_RealWalkWithNestedConfig(t *testing.T) {
	ctx := t.Context()
	svc, db, _ := setupSyncTestServiceInternal(t)
	svc.repoService = &gitrepo.GitRepositoryService{Client: git.NewClient("")}

	repoPath := t.TempDir()
	writeFileInternal(t, repoPath, "traefik (nl10)/docker-compose.yml", []byte(`services:
  traefik:
    image: traefik:v3.4
    volumes:
      - ./letsencrypt:/letsencrypt
      - ./logs:/var/log/traefik
      - ./config/dynamic_config.yml:/etc/traefik/dynamic_config.yml:ro
`))
	writeFileInternal(t, repoPath, "traefik (nl10)/config/dynamic_config.yml", []byte("http:\n  routers:\n    dashboard:\n      rule: Host(`traefik.example.com`)\n"))

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-directory-real-walk",
		Name:          "traefik-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "traefik (nl10)/docker-compose.yml",
		ProjectName:   "traefik (nl10)",
		SyncDirectory: true,
	}
	require.NoError(t, db.Create(sync).Error)

	syncFiles, err := svc.walkAndParseSyncDirectory(ctx, sync, repoPath)
	require.NoError(t, err)
	var composeContent string
	for _, f := range syncFiles {
		if f.RelativePath == "docker-compose.yml" {
			composeContent = string(f.Content)
		}
	}
	assert.Contains(t, composeContent, "./config/dynamic_config.yml")

	project, syncedFiles, created, changed, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, "", user.Actor{})
	require.NoError(t, err)
	require.NotNil(t, project)
	require.True(t, created)
	require.True(t, changed)
	require.ElementsMatch(t, []string{"docker-compose.yml", "config/dynamic_config.yml"}, syncedFiles)

	composePath, detectErr := projects.DetectComposeFile(t.Context(), "", project.Path)
	require.NoError(t, detectErr)
	assert.Equal(t, filepath.Join(project.Path, "docker-compose.yml"), composePath)

	composeInfo, err := os.Stat(filepath.Join(project.Path, "docker-compose.yml"))
	require.NoError(t, err)
	assert.False(t, composeInfo.IsDir())

	configPath := filepath.Join(project.Path, "config", "dynamic_config.yml")
	configInfo, err := os.Stat(configPath)
	require.NoError(t, err)
	assert.False(t, configInfo.IsDir())

	configBytes, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(configBytes), "dashboard:")
}

func TestGitOpsSyncService_DirectorySync_OverwritesExistingDirectoryAtFilePath(t *testing.T) {
	ctx := t.Context()
	svc, db, projectsDir := setupSyncTestServiceInternal(t)
	svc.repoService = &gitrepo.GitRepositoryService{Client: git.NewClient("")}

	repoPath := t.TempDir()
	writeFileInternal(t, repoPath, "traefik (nl10)/docker-compose.yml", []byte(`services:
  traefik:
    image: traefik:v3.4
    volumes:
      - ./letsencrypt:/letsencrypt
      - ./logs:/var/log/traefik
      - ./config/dynamic_config.yml:/etc/traefik/dynamic_config.yml:ro
`))
	writeFileInternal(t, repoPath, "traefik (nl10)/config/dynamic_config.yml", []byte("http:\n  routers:\n    dashboard:\n      rule: Host(`traefik.example.com`)\n"))

	projectPath := filepath.Join(projectsDir, "traefik-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectPath, "config", "dynamic_config.yml"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(projectPath, "letsencrypt"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(projectPath, "logs"), 0o755))

	dirName := "traefik-project"
	project := &projectpkg.Project{
		ID:      "proj-directory-docker-dir-conflict",
		Name:    "traefik-project",
		DirName: &dirName,
		Path:    projectPath,
		Status:  projectpkg.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-directory-docker-dir-conflict",
		Name:          "traefik-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "traefik (nl10)/docker-compose.yml",
		ProjectName:   "traefik-project",
		ProjectID:     &project.ID,
		SyncDirectory: true,
	}
	require.NoError(t, db.Create(sync).Error)

	syncFiles, err := svc.walkAndParseSyncDirectory(ctx, sync, repoPath)
	require.NoError(t, err)

	updatedProject, syncedFiles, created, changed, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, "", user.Actor{})
	require.NoError(t, err)
	require.NotNil(t, updatedProject)
	require.False(t, created)
	require.True(t, changed)
	require.ElementsMatch(t, []string{"docker-compose.yml", "config/dynamic_config.yml"}, syncedFiles)

	configPath := filepath.Join(updatedProject.Path, "config", "dynamic_config.yml")
	configInfo, err := os.Stat(configPath)
	require.NoError(t, err)
	assert.False(t, configInfo.IsDir())

	configBytes, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(configBytes), "dashboard:")

	composeInfo, err := os.Stat(filepath.Join(updatedProject.Path, "docker-compose.yml"))
	require.NoError(t, err)
	assert.False(t, composeInfo.IsDir())

	dockerArtifactInfo, err := os.Stat(filepath.Join(updatedProject.Path, "letsencrypt"))
	require.NoError(t, err)
	assert.True(t, dockerArtifactInfo.IsDir())

	dockerArtifactInfo, err = os.Stat(filepath.Join(updatedProject.Path, "logs"))
	require.NoError(t, err)
	assert.True(t, dockerArtifactInfo.IsDir())
}

func TestGitOpsSyncService_CreateDirectorySyncProjectInternal_RollsBackProjectOnUpdateFailure(t *testing.T) {
	ctx := t.Context()
	svc, db, projectsDir := setupSyncTestServiceInternal(t)

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-directory-tx-rollback",
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project",
		SyncDirectory: true,
	}
	require.NoError(t, db.Create(sync).Error)

	stagePath := filepath.Join(projectsDir, ".gitops-sync-stage-test")
	require.NoError(t, os.MkdirAll(stagePath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stagePath, "docker-compose.yaml"), []byte("services: {}\n"), 0o644))

	stage := &stagedDirectorySync{
		stagePath:       stagePath,
		stageLogical:    "/.gitops-sync-stage-test",
		projectsDir:     projectsDir,
		composeFileName: "docker-compose.yaml",
		serviceCount:    1,
	}

	callbackName := "test:fail_project_gitops_update"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "projects" {
			_ = tx.AddError(errors.New("forced project update failure"))
		}
	}))
	defer func() {
		_ = db.Callback().Update().Remove(callbackName)
	}()

	project, err := svc.createDirectorySyncProjectInternal(ctx, sync, stage, user.Actor{})
	require.Error(t, err)
	require.Nil(t, project)
	assert.Contains(t, err.Error(), "failed to mark project as GitOps-managed")

	var projectCount int64
	require.NoError(t, db.Model(&projectpkg.Project{}).Count(&projectCount).Error)
	assert.Zero(t, projectCount)

	var storedSync projectpkg.GitOpsSync
	require.NoError(t, db.First(&storedSync, "id = ?", sync.ID).Error)
	assert.Nil(t, storedSync.ProjectID)

	_, statErr := os.Stat(filepath.Join(projectsDir, "demo-project"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

// TestGitOpsSyncService_UpdateDirectorySyncProjectInternal_RollsBackScopedChangesOnUpdateFailure
// forces the project metadata update to fail after the synced files were written
// to the live project, and verifies the scoped rollback restores exactly what the
// sync touched (in place, keeping inodes) while unrelated files are never copied,
// rewritten or pruned.
func TestGitOpsSyncService_UpdateDirectorySyncProjectInternal_RollsBackScopedChangesOnUpdateFailure(t *testing.T) {
	ctx := t.Context()
	svc, db, projectsDir := setupSyncTestServiceInternal(t)

	projectPath := filepath.Join(projectsDir, "demo-project")
	writeFileInternal(t, projectPath, "docker-compose.yaml", []byte("services:\n  app:\n    image: nginx:1.26-alpine\n"))
	writeFileInternal(t, projectPath, "old.txt", []byte("remove me\n"))
	writeFileInternal(t, projectPath, "keep.txt", []byte("keep me\n"))
	writeFileInternal(t, projectPath, ".env", []byte("A=1\n"))
	writeFileInternal(t, projectPath, "data/blob.bin", []byte("bind-mount data\n"))
	// A directory sits where git now ships a file.
	require.NoError(t, os.MkdirAll(filepath.Join(projectPath, "config", "dynamic.yml"), 0o755))

	// Pre-linked, so the only projects-table update during the sync is the
	// metadata write that follows promotion.
	const syncID = "sync-directory-update-rollback"
	project := &projectpkg.Project{
		ID:              "proj-directory-update-rollback",
		Name:            "demo-project",
		DirName:         new("demo-project"),
		Path:            projectPath,
		Status:          projectpkg.ProjectStatusStopped,
		GitOpsManagedBy: new(syncID),
	}
	require.NoError(t, db.Create(project).Error)

	oldSyncedFilesJSON, err := json.Marshal([]string{"docker-compose.yaml", "old.txt"})
	require.NoError(t, err)
	sync := &projectpkg.GitOpsSync{
		ID:            syncID,
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project",
		ProjectID:     &project.ID,
		SyncDirectory: true,
		SyncedFiles:   new(string(oldSyncedFilesJSON)),
	}
	require.NoError(t, db.Create(sync).Error)

	syncFiles := []projects.SyncFile{
		{RelativePath: "docker-compose.yaml", Content: []byte("services:\n  app:\n    image: nginx:1.27-alpine\n")},
		{RelativePath: "config/dynamic.yml", Content: []byte("http: {}\n")},
		{RelativePath: ".env", Content: []byte("A=1\nB=2\n")},
	}

	composeBefore, err := os.Stat(filepath.Join(projectPath, "docker-compose.yaml"))
	require.NoError(t, err)
	keepBefore, err := os.Stat(filepath.Join(projectPath, "keep.txt"))
	require.NoError(t, err)
	blobBefore, err := os.Stat(filepath.Join(projectPath, "data", "blob.bin"))
	require.NoError(t, err)

	// Fail after promotion wrote the live files. While the failure fires, the
	// scoped backup still exists: record the live compose content and what the
	// backup captured.
	var composeAtFailure string
	var backupContents []string
	callbackName := "test:fail_project_gitops_directory_update"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Table != "projects" {
			return
		}
		liveCompose, readErr := os.ReadFile(filepath.Join(projectPath, "docker-compose.yaml"))
		require.NoError(t, readErr)
		composeAtFailure = string(liveCompose)
		entries, readErr := os.ReadDir(projectsDir)
		require.NoError(t, readErr)
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), ".gitops-backup-") {
				continue
			}
			backupDir := filepath.Join(projectsDir, entry.Name())
			_ = filepath.WalkDir(backupDir, func(p string, _ os.DirEntry, _ error) error {
				rel, _ := filepath.Rel(backupDir, p)
				backupContents = append(backupContents, filepath.ToSlash(rel))
				return nil
			})
		}
		_ = tx.AddError(errors.New("forced project update failure"))
	}))
	defer func() {
		_ = db.Callback().Update().Remove(callbackName)
	}()

	updatedProject, _, _, _, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, "", user.Actor{})
	require.Error(t, err)
	require.Nil(t, updatedProject)
	assert.Contains(t, err.Error(), "forced project update failure")
	assert.NotContains(t, err.Error(), "rollback project directory")

	// Promotion had already applied the new files when the failure fired, and
	// the backup held only the paths the sync touches, never unrelated data.
	assert.Contains(t, composeAtFailure, "nginx:1.27-alpine")
	assert.ElementsMatch(t, []string{".", ".env", "config", "config/dynamic.yml", "docker-compose.yaml", "old.txt"}, backupContents)

	// Changed paths are back exactly as they were, in place.
	composeAfter, err := os.Stat(filepath.Join(projectPath, "docker-compose.yaml"))
	require.NoError(t, err)
	assert.True(t, os.SameFile(composeBefore, composeAfter))
	composeBytes, err := os.ReadFile(filepath.Join(projectPath, "docker-compose.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(composeBytes), "nginx:1.26-alpine")

	oldBytes, err := os.ReadFile(filepath.Join(projectPath, "old.txt"))
	require.NoError(t, err)
	assert.Equal(t, "remove me\n", string(oldBytes))

	configInfo, err := os.Stat(filepath.Join(projectPath, "config", "dynamic.yml"))
	require.NoError(t, err)
	assert.True(t, configInfo.IsDir())

	envBytes, err := os.ReadFile(filepath.Join(projectPath, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "A=1\n", string(envBytes))
	_, statErr := os.Lstat(filepath.Join(projectPath, projects.GitSourceEnvFileName))
	require.ErrorIs(t, statErr, os.ErrNotExist)

	// Unrelated files were left alone.
	keepAfter, err := os.Stat(filepath.Join(projectPath, "keep.txt"))
	require.NoError(t, err)
	assert.True(t, os.SameFile(keepBefore, keepAfter))
	assert.Equal(t, keepBefore.ModTime(), keepAfter.ModTime())
	blobAfter, err := os.Stat(filepath.Join(projectPath, "data", "blob.bin"))
	require.NoError(t, err)
	assert.True(t, os.SameFile(blobBefore, blobAfter))
	assert.Equal(t, blobBefore.ModTime(), blobAfter.ModTime())

	// Both scratch directories were removed.
	entries, err := os.ReadDir(projectsDir)
	require.NoError(t, err)
	for _, entry := range entries {
		assert.False(t, projects.IsGitOpsScratchDirName(entry.Name()), "leaked scratch directory %s", entry.Name())
	}
}

func TestGitOpsSyncService_GetDirectorySyncProjectInternal_RelinksManagedProjectWhenProjectIDStale(t *testing.T) {
	ctx := t.Context()
	svc, db, projectsDir := setupSyncTestServiceInternal(t)

	projectPath := filepath.Join(projectsDir, "Radarr-3")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "radarr.yaml"), []byte("services:\n  app:\n    image: lscr.io/linuxserver/radarr:latest\n"), 0o644))

	missingProjectID := "missing-project"
	sync := &projectpkg.GitOpsSync{
		ID:            "sync-directory-relink",
		Name:          "radarr-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/media/radarr.yaml",
		ProjectName:   "Radarr",
		ProjectID:     &missingProjectID,
		SyncDirectory: true,
	}
	require.NoError(t, db.Create(sync).Error)

	dirName := "Radarr-3"
	project := &projectpkg.Project{
		ID:              "proj-directory-relink",
		Name:            "Radarr",
		DirName:         &dirName,
		Path:            projectPath,
		Status:          projectpkg.ProjectStatusStopped,
		GitOpsManagedBy: &sync.ID,
	}
	require.NoError(t, db.Create(project).Error)

	recovered, err := svc.getDirectorySyncProjectInternal(ctx, sync)
	require.NoError(t, err)
	require.NotNil(t, recovered)
	assert.Equal(t, project.ID, recovered.ID)

	var storedSync projectpkg.GitOpsSync
	require.NoError(t, db.First(&storedSync, "id = ?", sync.ID).Error)
	require.NotNil(t, storedSync.ProjectID)
	assert.Equal(t, project.ID, *storedSync.ProjectID)
}

func TestGitOpsSyncService_GetDirectorySyncProjectInternal_RecoversUniqueDirectoryCandidate(t *testing.T) {
	ctx := t.Context()
	svc, db, projectsDir := setupSyncTestServiceInternal(t)

	projectPath := filepath.Join(projectsDir, "Radarr-3")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "radarr.yaml"), []byte("services:\n  app:\n    image: lscr.io/linuxserver/radarr:latest\n"), 0o644))

	missingProjectID := "missing-project"
	sync := &projectpkg.GitOpsSync{
		ID:            "sync-directory-disk-recovery",
		Name:          "radarr-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/media/radarr.yaml",
		ProjectName:   "Radarr",
		ProjectID:     &missingProjectID,
		SyncDirectory: true,
	}
	require.NoError(t, db.Create(sync).Error)

	recovered, err := svc.getDirectorySyncProjectInternal(ctx, sync)
	require.NoError(t, err)
	require.NotNil(t, recovered)
	assert.Equal(t, projectPath, recovered.Path)
	require.NotNil(t, recovered.GitOpsManagedBy)
	assert.Equal(t, sync.ID, *recovered.GitOpsManagedBy)

	var storedSync projectpkg.GitOpsSync
	require.NoError(t, db.First(&storedSync, "id = ?", sync.ID).Error)
	require.NotNil(t, storedSync.ProjectID)
	assert.Equal(t, recovered.ID, *storedSync.ProjectID)

	var storedProject projectpkg.Project
	require.NoError(t, db.First(&storedProject, "id = ?", recovered.ID).Error)
	assert.Equal(t, projectPath, storedProject.Path)
	assert.Equal(t, 1, storedProject.ServiceCount)
}

// TestBuildSwarmStackDeployRequestInternal guards the Git Sync swarm deploy request.
// WithRegistryAuth must always be set: swarm tasks pull with only the auth embedded in
// the service spec, so a request without it makes every private image fail to pull.
func TestBuildSwarmStackDeployRequestInternal(t *testing.T) {
	t.Parallel()

	files := []swarm.SyncFile{{RelativePath: "configs/app.conf", Content: []byte("k=v")}}

	cases := []struct {
		name            string
		sync            *projectpkg.GitOpsSync
		source          *preparedSyncSource
		overrideContent string
		envContent      string
		files           []swarm.SyncFile
		want            swarm.StackDeployRequest
	}{
		{
			name:            "populates all fields and enables registry auth",
			sync:            &projectpkg.GitOpsSync{ProjectName: "descent", ComposePath: "deploy/swarm/compose.yaml"},
			source:          &preparedSyncSource{repoPath: "/tmp/repo", composeContent: "services: {}\n"},
			overrideContent: "override",
			envContent:      "A=1",
			files:           files,
			want: swarm.StackDeployRequest{
				Name:             "descent",
				ComposeContent:   "services: {}\n",
				OverrideContent:  "override",
				EnvContent:       "A=1",
				Files:            files,
				Prune:            true,
				WithRegistryAuth: true,
				WorkingDir:       filepath.Join(string(filepath.Separator), "tmp", "repo", "deploy", "swarm"),
			},
		},
		{
			name:   "enables registry auth with empty optional content and a root-level compose path",
			sync:   &projectpkg.GitOpsSync{ProjectName: "ptest", ComposePath: "compose.yaml"},
			source: &preparedSyncSource{repoPath: "/tmp/repo", composeContent: "services: {}\n"},
			want: swarm.StackDeployRequest{
				Name:             "ptest",
				ComposeContent:   "services: {}\n",
				Prune:            true,
				WithRegistryAuth: true,
				WorkingDir:       "/tmp/repo",
			},
		},
		{
			name:   "passes an empty file list through unchanged",
			sync:   &projectpkg.GitOpsSync{ProjectName: "ptest", ComposePath: "stacks/compose.yaml"},
			source: &preparedSyncSource{repoPath: "/tmp/repo", composeContent: "services: {}\n"},
			// performSwarmStackSyncInternal always passes a non-nil slice; the builder must not reshape it.
			files: []swarm.SyncFile{},
			want: swarm.StackDeployRequest{
				Name:             "ptest",
				ComposeContent:   "services: {}\n",
				Files:            []swarm.SyncFile{},
				Prune:            true,
				WithRegistryAuth: true,
				WorkingDir:       filepath.Join(string(filepath.Separator), "tmp", "repo", "stacks"),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildSwarmStackDeployRequestInternal(tc.sync, tc.source, tc.overrideContent, tc.envContent, tc.files)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestMarkSyncRedeployFailedInternal_PersistsErrorOnSyncRow(t *testing.T) {
	ctx := t.Context()
	svc, db, _ := setupSyncTestServiceInternal(t)
	// Event logging requires a real event.EventService; the shared setup leaves it
	// nil since most tests don't exercise the event path.
	require.NoError(t, db.AutoMigrate(&event.Event{}))
	svc.eventService = event.NewEventService(db, config.Load(), nil)

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-1",
		Name:          "redeploy-fail",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		Branch:        "main",
		ComposePath:   "compose.yml",
		TargetType:    "project",
	}
	require.NoError(t, db.WithContext(ctx).Select("*").Omit("Environment", "Repository", "Project").Create(sync).Error)

	result := &gitops.SyncResult{Success: true}
	syncedFiles := []string{"compose.yml", "scripts/pre-deploy.sh"}
	hookErr := common.Classify(common.ErrRedeployAfterSyncFailed, fmt.Errorf("redeploy failed: %w", errors.New("pre-deploy hook failed: exit 1")))

	svc.markSyncRedeployFailedInternal(ctx, sync, sync.ID, "abc123", syncedFiles, hookErr, user.Actor{ID: "user", Username: "tester"}, result)

	require.False(t, result.Success)
	require.NotNil(t, result.Error)
	require.Contains(t, *result.Error, "pre-deploy hook failed")

	var stored projectpkg.GitOpsSync
	require.NoError(t, db.WithContext(ctx).First(&stored, "id = ?", sync.ID).Error)
	require.NotNil(t, stored.LastSyncStatus)
	require.Equal(t, "failed", *stored.LastSyncStatus)
	require.NotNil(t, stored.LastSyncError)
	require.Contains(t, *stored.LastSyncError, "pre-deploy hook failed")
	// The synced-files list should still be populated so operators can see
	// what reached disk before the redeploy died.
	require.NotNil(t, stored.SyncedFiles)
	require.Contains(t, *stored.SyncedFiles, "compose.yml")
	require.Contains(t, *stored.SyncedFiles, "scripts/pre-deploy.sh")
}

// TestGitOpsSyncService_SyncProjectDirectory_PreservesUnreadableBindMountData verifies
// that re-syncing an existing GitOps project tolerates a foreign-owned unreadable file
// inside the project directory (e.g. data a container wrote through a relative bind
// mount, owned by another UID with restrictive perms). The file is outside the sync's
// scope, so staging, backup and promotion must never read, copy or prune it.
func TestGitOpsSyncService_SyncProjectDirectory_PreservesUnreadableBindMountData(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not enforced on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("permission bits are ignored when running as root")
	}

	ctx := t.Context()
	svc, db, projectsDir := setupSyncTestServiceInternal(t)

	projectPath := filepath.Join(projectsDir, "demo-project")
	writeFileInternal(t, projectPath, "docker-compose.yaml", []byte(`services:
  app:
    image: nginx:1.26-alpine
`))

	// A container wrote a foreign-owned, unreadable file into a relative bind-mount
	// directory inside the project. Arcane's PUID cannot read it.
	secretPath := filepath.Join(projectPath, "data", "secret.bin")
	require.NoError(t, os.MkdirAll(filepath.Dir(secretPath), 0o755))
	require.NoError(t, os.WriteFile(secretPath, []byte("supersecret"), 0o644))
	require.NoError(t, os.Chmod(secretPath, 0o000))
	t.Cleanup(func() { _ = os.Chmod(secretPath, 0o644) })

	project := &projectpkg.Project{
		ID:      "proj-unreadable-bindmount",
		Name:    "demo-project",
		DirName: new("demo-project"),
		Path:    projectPath,
		Status:  projectpkg.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	oldSyncedFilesJSON, err := json.Marshal([]string{"docker-compose.yaml"})
	require.NoError(t, err)

	sync := &projectpkg.GitOpsSync{
		ID:            "sync-unreadable-bindmount",
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/demo/docker-compose.yaml",
		ProjectName:   "demo-project",
		ProjectID:     &project.ID,
		SyncDirectory: true,
		SyncedFiles:   new(string(oldSyncedFilesJSON)),
	}
	require.NoError(t, db.Create(sync).Error)

	syncFiles := []projects.SyncFile{
		{
			RelativePath: "docker-compose.yaml",
			Content: []byte(`services:
  app:
    image: nginx:1.27-alpine
`),
		},
	}

	updatedProject, _, created, _, err := svc.syncProjectDirectoryInternal(ctx, sync, syncFiles, "", user.Actor{})
	require.NoError(t, err)
	require.NotNil(t, updatedProject)
	require.False(t, created)

	// The unreadable foreign file survived promotion untouched (preserved, not pruned).
	require.NoError(t, os.Chmod(secretPath, 0o644))
	secretBytes, err := os.ReadFile(secretPath)
	require.NoError(t, err)
	assert.Equal(t, "supersecret", string(secretBytes))

	// The managed compose file was still updated by the sync.
	composeBytes, err := os.ReadFile(filepath.Join(updatedProject.Path, "docker-compose.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(composeBytes), "nginx:1.27-alpine")
}

func TestSyncService_SyncProjectDirectory_InjectsCommitEnvWhenEnabledInternal(t *testing.T) {
	ctx := t.Context()
	svc, db, _ := setupSyncTestServiceInternal(t)

	const commitHash = "9f2c1ab3d4e5f60718293a4b5c6d7e8f90a1b2c3"

	syncRecord := &projectpkg.GitOpsSync{
		ID:              "sync-directory-commit-env",
		Name:            "demo-sync",
		EnvironmentID:   "0",
		RepositoryID:    "repo-1",
		Branch:          "main",
		ComposePath:     "apps/demo/docker-compose.yaml",
		ProjectName:     "demo-project",
		SyncDirectory:   true,
		InjectCommitEnv: true,
	}
	require.NoError(t, db.Create(syncRecord).Error)

	syncFiles := []projects.SyncFile{
		{
			RelativePath: "docker-compose.yaml",
			Content: []byte(`services:
  app:
    image: nginx:alpine
    environment:
      COMMIT: ${ARCANE_GIT_COMMIT}
`),
		},
	}

	project, _, created, _, err := svc.syncProjectDirectoryInternal(ctx, syncRecord, syncFiles, commitHash, user.Actor{})
	require.NoError(t, err)
	require.NotNil(t, project)
	require.True(t, created)

	effectiveEnv, err := projects.ParseProjectEnvFile(filepath.Join(project.Path, ".env"), nil)
	require.NoError(t, err)
	assert.Equal(t, commitHash, effectiveEnv[projects.GitCommitEnvKey])
	assert.Equal(t, "9f2c1ab", effectiveEnv[projects.GitCommitShortEnvKey])
	assert.Equal(t, "main", effectiveEnv[projects.GitBranchEnvKey])

	gitEnv, err := projects.ParseProjectEnvFile(filepath.Join(project.Path, ".env.git"), nil)
	require.NoError(t, err)
	assert.Equal(t, commitHash, gitEnv[projects.GitCommitEnvKey])

	// The metadata is Git-sourced, so it must not have been copied into the
	// user-editable override.
	_, statErr := os.Stat(filepath.Join(project.Path, "project.env"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestSyncService_SyncProjectDirectory_CommitOnlyChangeDoesNotRedeployInternal(t *testing.T) {
	ctx := t.Context()
	svc, db, projectsDir := setupSyncTestServiceInternal(t)

	const (
		previousCommit = "1111111111111111111111111111111111111111"
		currentCommit  = "2222222222222222222222222222222222222222"
	)
	composeContent := `services:
  app:
    image: nginx:1.27-alpine
`
	repoEnvContent := "FOO=git\n"
	syncedEnvContent := projects.BuildGitMetadataEnvContent(repoEnvContent, previousCommit, "main")

	projectPath := filepath.Join(projectsDir, "demo-project")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "docker-compose.yaml"), []byte(composeContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env.git"), []byte(syncedEnvContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte(syncedEnvContent), 0o644))

	project := &projectpkg.Project{
		ID:      "proj-directory-commit-env",
		Name:    "demo-project",
		DirName: new("demo-project"),
		Path:    projectPath,
		Status:  projectpkg.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	oldSyncedFilesJSON, err := json.Marshal([]string{"docker-compose.yaml"})
	require.NoError(t, err)

	syncRecord := &projectpkg.GitOpsSync{
		ID:              "sync-directory-commit-env-update",
		Name:            "demo-sync",
		EnvironmentID:   "0",
		RepositoryID:    "repo-1",
		Branch:          "main",
		ComposePath:     "apps/demo/docker-compose.yaml",
		ProjectName:     "demo-project",
		ProjectID:       &project.ID,
		SyncDirectory:   true,
		InjectCommitEnv: true,
		SyncedFiles:     new(string(oldSyncedFilesJSON)),
	}
	require.NoError(t, db.Create(syncRecord).Error)

	syncFiles := []projects.SyncFile{
		{RelativePath: "docker-compose.yaml", Content: []byte(composeContent)},
		{RelativePath: ".env", Content: []byte(repoEnvContent)},
	}

	updatedProject, _, created, changed, err := svc.syncProjectDirectoryInternal(ctx, syncRecord, syncFiles, currentCommit, user.Actor{})
	require.NoError(t, err)
	require.NotNil(t, updatedProject)
	require.False(t, created)
	assert.False(t, changed, "a commit that leaves the synced files untouched must not trigger a redeploy")

	effectiveEnv, err := projects.ParseProjectEnvFile(filepath.Join(updatedProject.Path, ".env"), nil)
	require.NoError(t, err)
	assert.Equal(t, currentCommit, effectiveEnv[projects.GitCommitEnvKey], "the new commit is still written to disk")
	assert.Equal(t, "git", effectiveEnv["FOO"])
}

// TestSyncService_SyncProjectDirectory_EnablingInjectionMarksContentsChangedInternal
// covers the first sync after the flag is switched on: the project gains the
// metadata keys, so an already-running project must be redeployed for its
// containers to receive them at all.
func TestSyncService_SyncProjectDirectory_EnablingInjectionMarksContentsChangedInternal(t *testing.T) {
	ctx := t.Context()
	svc, db, projectsDir := setupSyncTestServiceInternal(t)

	composeContent := `services:
  app:
    image: nginx:1.27-alpine
`
	repoEnvContent := "FOO=git\n"

	projectPath := filepath.Join(projectsDir, "demo-project")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "docker-compose.yaml"), []byte(composeContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env.git"), []byte(repoEnvContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte(repoEnvContent), 0o644))

	project := &projectpkg.Project{
		ID:      "proj-directory-enable-injection",
		Name:    "demo-project",
		DirName: new("demo-project"),
		Path:    projectPath,
		Status:  projectpkg.ProjectStatusRunning,
	}
	require.NoError(t, db.Create(project).Error)

	oldSyncedFilesJSON, err := json.Marshal([]string{"docker-compose.yaml"})
	require.NoError(t, err)

	syncRecord := &projectpkg.GitOpsSync{
		ID:              "sync-directory-enable-injection",
		Name:            "demo-sync",
		EnvironmentID:   "0",
		RepositoryID:    "repo-1",
		Branch:          "main",
		ComposePath:     "apps/demo/docker-compose.yaml",
		ProjectName:     "demo-project",
		ProjectID:       &project.ID,
		SyncDirectory:   true,
		InjectCommitEnv: true,
		SyncedFiles:     new(string(oldSyncedFilesJSON)),
	}
	require.NoError(t, db.Create(syncRecord).Error)

	syncFiles := []projects.SyncFile{
		{RelativePath: "docker-compose.yaml", Content: []byte(composeContent)},
		{RelativePath: ".env", Content: []byte(repoEnvContent)},
	}

	const commitHash = "9f2c1ab3d4e5f60718293a4b5c6d7e8f90a1b2c3"
	updatedProject, _, created, changed, err := svc.syncProjectDirectoryInternal(ctx, syncRecord, syncFiles, commitHash, user.Actor{})
	require.NoError(t, err)
	require.NotNil(t, updatedProject)
	require.False(t, created)
	assert.True(t, changed, "newly injected variables must reach running containers")

	effectiveEnv, err := projects.ParseProjectEnvFile(filepath.Join(updatedProject.Path, ".env"), nil)
	require.NoError(t, err)
	assert.Equal(t, commitHash, effectiveEnv[projects.GitCommitEnvKey])
}

func TestGitMetadataEnvContentInternal(t *testing.T) {
	const commitHash = "9f2c1ab3d4e5f60718293a4b5c6d7e8f90a1b2c3"
	repoEnv := "FOO=git\n"

	t.Run("returns nothing when the sync did not opt in", func(t *testing.T) {
		_, ok := gitMetadataEnvContentInternal(&projectpkg.GitOpsSync{Branch: "main"}, &repoEnv, commitHash)
		assert.False(t, ok)
	})

	t.Run("returns nothing when the commit could not be resolved", func(t *testing.T) {
		syncRecord := &projectpkg.GitOpsSync{Branch: "main", InjectCommitEnv: true}
		_, ok := gitMetadataEnvContentInternal(syncRecord, &repoEnv, "")
		assert.False(t, ok)
	})

	t.Run("appends to the repository env and stands alone when the repo has none", func(t *testing.T) {
		syncRecord := &projectpkg.GitOpsSync{Branch: "main", InjectCommitEnv: true}

		withRepoEnv, ok := gitMetadataEnvContentInternal(syncRecord, &repoEnv, commitHash)
		require.True(t, ok)
		parsed, err := projects.ParseProjectEnvContent(withRepoEnv, nil)
		require.NoError(t, err)
		assert.Equal(t, "git", parsed["FOO"])
		assert.Equal(t, commitHash, parsed[projects.GitCommitEnvKey])

		withoutRepoEnv, ok := gitMetadataEnvContentInternal(syncRecord, nil, commitHash)
		require.True(t, ok)
		parsed, err = projects.ParseProjectEnvContent(withoutRepoEnv, nil)
		require.NoError(t, err)
		assert.Equal(t, projects.EnvMap{
			projects.GitCommitEnvKey:      commitHash,
			projects.GitCommitShortEnvKey: "9f2c1ab",
			projects.GitBranchEnvKey:      "main",
		}, parsed)
	})
}
