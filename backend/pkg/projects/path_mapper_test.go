package projects

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPathMapperForConfiguredDirectory(t *testing.T) {
	ctx := context.Background()

	t.Run("matching paths do not need a mapper", func(t *testing.T) {
		containerDir := t.TempDir()
		pathMapper := NewPathMapperForConfiguredDirectory(ctx, containerDir, containerDir, nil)
		require.Nil(t, pathMapper)
	})

	t.Run("explicit mapping is translated", func(t *testing.T) {
		containerDir := filepath.Join(t.TempDir(), "container")
		hostDir := "/host/path"
		pathMapper := NewPathMapperForConfiguredDirectory(ctx, containerDir+":"+hostDir, containerDir, nil)
		require.NotNil(t, pathMapper)

		source := filepath.Join(containerDir, "0/stack/compose.yaml")
		expected := filepath.Join(hostDir, "0/stack/compose.yaml")
		translated, err := pathMapper.ContainerToHost(source)
		require.NoError(t, err)
		require.Equal(t, filepath.ToSlash(expected), filepath.ToSlash(translated))
	})
}

func TestPathMapper_MatchingMount_NoTranslation(t *testing.T) {
	pm := NewPathMapper("/app/data/projects", "")
	result, err := pm.ContainerToHost("/app/data/projects/test/data")
	require.NoError(t, err)
	assert.Equal(t, "/app/data/projects/test/data", result)
}

func TestPathMapper_WindowsMount_Translation(t *testing.T) {
	pm := NewPathMapper("/app/data/projects", "D:/arcane/projects")
	result, err := pm.ContainerToHost("/app/data/projects/test/data")
	require.NoError(t, err)
	assert.Equal(t, "D:/arcane/projects/test/data", result)
}

func TestPathMapper_PathOutsidePrefix_NoTranslation(t *testing.T) {
	pm := NewPathMapper("/app/data/projects", "D:/arcane/projects")
	result, err := pm.ContainerToHost("/etc/hosts")
	require.NoError(t, err)
	assert.Equal(t, "/etc/hosts", result)
}

func TestPathMapper_PathTraversalPrevention(t *testing.T) {
	pm := NewPathMapper("/app/data/projects", "/host/projects")
	result, err := pm.ContainerToHost("/app/data/projects/../../etc/passwd")
	require.NoError(t, err)
	assert.Equal(t, "/app/etc/passwd", result)
}

func TestPathMapper_FromMounts_NestedIndependentMount(t *testing.T) {
	// Reproduces the nested independent-mount bug: a project directory is bind-mounted
	// into Arcane's projects directory from an unrelated host path.
	pm := NewPathMapperFromMounts([]HostMount{
		{Destination: "/app/data", Source: "/home/user/.arcane/data"},
		{Destination: "/app/data/projects/homeassistant", Source: "/home/user/homeassistant"},
	})
	require.True(t, pm.IsNonMatchingMount())

	// Independently-mounted project resolves to its own host path (longest-prefix wins).
	got, err := pm.ContainerToHost("/app/data/projects/homeassistant/service_postgresql/postgres_data")
	require.NoError(t, err)
	assert.Equal(t, "/home/user/homeassistant/service_postgresql/postgres_data", got)

	// A project without its own mount still re-bases under /app/data.
	got, err = pm.ContainerToHost("/app/data/projects/other/data")
	require.NoError(t, err)
	assert.Equal(t, "/home/user/.arcane/data/projects/other/data", got)
}

func TestPathMapper_FromMounts_NoMatchAndMatchingMounts(t *testing.T) {
	pm := NewPathMapperFromMounts([]HostMount{
		{Destination: "/app/data", Source: "/host/data"},
	})

	// Path outside every mount is returned unchanged.
	got, err := pm.ContainerToHost("/etc/hosts")
	require.NoError(t, err)
	assert.Equal(t, "/etc/hosts", got)

	// A table whose mounts all match (source == destination) needs no translation.
	matching := NewPathMapperFromMounts([]HostMount{
		{Destination: "/var/run/docker.sock", Source: "/var/run/docker.sock"},
	})
	assert.False(t, matching.IsNonMatchingMount())
}

func TestPathMapper_TranslateVolumeSources(t *testing.T) {
	pm := NewPathMapper("/app/data/projects", "C:/User/arcane/projects")

	project := &composetypes.Project{
		Services: composetypes.Services{
			"app": {
				Name: "app",
				Volumes: []composetypes.ServiceVolumeConfig{
					{
						Type:   composetypes.VolumeTypeBind,
						Source: "/app/data/projects/myproj/data",
						Target: "/data",
					},
					{
						Type:   composetypes.VolumeTypeVolume,
						Source: "named-vol",
						Target: "/vol",
					},
				},
			},
		},
		Secrets: composetypes.Secrets{
			"my-secret": {
				File: "/app/data/projects/myproj/secret.txt",
			},
		},
		Configs: composetypes.Configs{
			"my-config": {
				File: "/app/data/projects/myproj/config.yaml",
			},
		},
	}

	err := pm.TranslateVolumeSources(project, true)
	require.NoError(t, err)

	assert.Equal(t, "C:/User/arcane/projects/myproj/data", project.Services["app"].Volumes[0].Source)
	assert.Equal(t, "named-vol", project.Services["app"].Volumes[1].Source)
	assert.Equal(t, "C:/User/arcane/projects/myproj/secret.txt", project.Secrets["my-secret"].File)
	assert.Equal(t, "C:/User/arcane/projects/myproj/config.yaml", project.Configs["my-config"].File)
}

func TestPathMapper_TranslateVolumeSourcesKeepsArcaneReadFiles(t *testing.T) {
	pm := NewPathMapper("/app/data/swarm/sources", "/host/swarm/sources")
	project := &composetypes.Project{
		Services: composetypes.Services{
			"app": {
				Name: "app",
				Volumes: []composetypes.ServiceVolumeConfig{{
					Type:   composetypes.VolumeTypeBind,
					Source: "/app/data/swarm/sources/0/stack/data",
					Target: "/data",
				}},
			},
		},
		Secrets: composetypes.Secrets{
			"secret": {File: "/app/data/swarm/sources/0/stack/secret.txt"},
		},
		Configs: composetypes.Configs{
			"config": {File: "/app/data/swarm/sources/0/stack/config.yaml"},
		},
	}

	require.NoError(t, pm.TranslateVolumeSources(project, false))
	assert.Equal(t, "/host/swarm/sources/0/stack/data", project.Services["app"].Volumes[0].Source)
	assert.Equal(t, "/app/data/swarm/sources/0/stack/secret.txt", project.Secrets["secret"].File)
	assert.Equal(t, "/app/data/swarm/sources/0/stack/config.yaml", project.Configs["config"].File)
}

// A bind-backed named volume's device is opened by the Docker daemon, so it needs
// the same container->host translation as a bind mount source.
func TestPathMapper_TranslateVolumeSourcesDriverOptsDevice(t *testing.T) {
	pm := NewPathMapper("/app/data/projects", "/host/arcane-data/projects")
	project := &composetypes.Project{
		Volumes: composetypes.Volumes{
			"bind-backed": {
				Driver:     "local",
				DriverOpts: composetypes.Options{"type": "none", "o": "bind", "device": "/app/data/projects/myproj/data"},
			},
			"nfs-backed": {
				Driver:     "local",
				DriverOpts: composetypes.Options{"type": "nfs", "o": "addr=10.0.0.1,rw", "device": ":/exported/path"},
			},
			// compose-go only absolutizes a device when `o` is exactly "bind", so this
			// one stays relative and must be left alone rather than failing the load.
			"unresolved-bind": {
				Driver:     "local",
				DriverOpts: composetypes.Options{"type": "none", "o": "bind,ro", "device": "data"},
			},
			"plain": {},
		},
	}

	require.NoError(t, pm.TranslateVolumeSources(project, true))
	assert.Equal(t, "/host/arcane-data/projects/myproj/data", project.Volumes["bind-backed"].DriverOpts["device"])
	assert.Equal(t, ":/exported/path", project.Volumes["nfs-backed"].DriverOpts["device"])
	assert.Equal(t, "data", project.Volumes["unresolved-bind"].DriverOpts["device"])
}

// The mount table must keep winning for relative paths that stay inside a nested
// independently-bind-mounted directory; only paths it cannot resolve fall back to
// being re-resolved against the host project directory.
func TestRemapEscapedRelativeSources_MountTableTakesPrecedence(t *testing.T) {
	pm := NewPathMapperFromMounts([]HostMount{
		{Destination: "/app/data", Source: "/docker/112/arcane/arcane-data"},
		{Destination: "/app/data/projects/goclaw/media", Source: "/mnt/media"},
	})
	require.True(t, pm.IsNonMatchingMount())

	containerWorkingDir := "/app/data/projects/goclaw"
	project := &composetypes.Project{
		Services: composetypes.Services{
			"goclaw": {
				Name: "goclaw",
				Volumes: []composetypes.ServiceVolumeConfig{
					{Type: composetypes.VolumeTypeBind, Source: "/app/data/projects/goclaw/media", Target: "/media"},
					{Type: composetypes.VolumeTypeBind, Source: "/goclaw/data", Target: "/app/data"},
				},
			},
		},
	}
	rawSources := map[string]string{
		VolumeSourceKey("goclaw", "/media"):    "./media",
		VolumeSourceKey("goclaw", "/app/data"): "../../../../goclaw/data",
	}

	require.NoError(t, pm.TranslateVolumeSources(project, true))
	RemapEscapedRelativeSources(context.Background(), pm, project, containerWorkingDir, rawSources, true)

	volumes := project.Services["goclaw"].Volumes
	assert.Equal(t, "/mnt/media", volumes[0].Source, "nested independent mount must win over the host project directory")
	assert.Equal(t, "/docker/112/goclaw/data", volumes[1].Source)
}

func TestPathMapper_IsPathMounted(t *testing.T) {
	fromMounts := NewPathMapperFromMounts([]HostMount{
		{Destination: "/opt/docker", Source: "/opt/docker"},
		{Destination: "/app/data", Source: "/var/lib/docker/volumes/arcane-data/_data"},
	})
	assert.True(t, fromMounts.IsPathMounted("/opt/docker/api-server"), "a matching mount is still a mount")
	assert.True(t, fromMounts.IsPathMounted("/app/data/projects/demo"))
	assert.False(t, fromMounts.IsPathMounted("/srv/elsewhere/demo"))

	prefixed := NewPathMapper("/app/data/projects", "/host/projects")
	assert.True(t, prefixed.IsPathMounted("/app/data/projects/demo"))
	assert.False(t, prefixed.IsPathMounted("/etc/hosts"))
}

// A matching (identity) bind mount such as `-v /opt/docker:/opt/docker` resolves
// every project directory to itself. Arcane's own named-volume mount still makes
// the table non-matching, so the remapper runs — and it must not mistake the
// unchanged path for a project directory that was never mounted.
func TestRemapEscapedRelativeSources_MatchingMountIsNotReportedAsUnmounted(t *testing.T) {
	logBuffer := captureWarningsInternal(t)

	pm := NewPathMapperFromMounts([]HostMount{
		{Destination: "/var/run/docker.sock", Source: "/var/run/docker.sock"},
		{Destination: "/opt/docker", Source: "/opt/docker"},
		{Destination: "/app/data", Source: "/var/lib/docker/volumes/arcane-data/_data"},
	})
	require.True(t, pm.IsNonMatchingMount())

	project := &composetypes.Project{
		Services: composetypes.Services{
			"api": {
				Name: "api",
				Volumes: []composetypes.ServiceVolumeConfig{
					{Type: composetypes.VolumeTypeBind, Source: "/opt/docker/api-server/data", Target: "/data"},
				},
			},
		},
	}
	rawSources := map[string]string{VolumeSourceKey("api", "/data"): "./data"}

	require.NoError(t, pm.TranslateVolumeSources(project, true))
	RemapEscapedRelativeSources(context.Background(), pm, project, "/opt/docker/api-server", rawSources, true)

	assert.Equal(t, "/opt/docker/api-server/data", project.Services["api"].Volumes[0].Source)
	assert.NotContains(t, logBuffer.String(), "not inside a mounted directory")
}

// The warning still has to fire for the case it was written for: a project
// directory that really is outside every mount, where a relative source that
// escapes it cannot be anchored to a host path.
func TestRemapEscapedRelativeSources_UnmountedProjectDirIsReported(t *testing.T) {
	logBuffer := captureWarningsInternal(t)

	pm := NewPathMapperFromMounts([]HostMount{
		{Destination: "/app/data", Source: "/host/arcane-data"},
	})
	require.True(t, pm.IsNonMatchingMount())

	project := &composetypes.Project{
		Services: composetypes.Services{
			"api": {
				Name: "api",
				Volumes: []composetypes.ServiceVolumeConfig{
					{Type: composetypes.VolumeTypeBind, Source: "/srv/projects/shared", Target: "/data"},
				},
			},
		},
	}
	rawSources := map[string]string{VolumeSourceKey("api", "/data"): "../shared"}

	require.NoError(t, pm.TranslateVolumeSources(project, true))
	RemapEscapedRelativeSources(context.Background(), pm, project, "/srv/projects/demo", rawSources, true)

	assert.Equal(t, "/srv/projects/shared", project.Services["api"].Volumes[0].Source)
	assert.Contains(t, logBuffer.String(), "not inside a mounted directory")
}

func captureWarningsInternal(t *testing.T) *bytes.Buffer {
	t.Helper()

	buffer := &bytes.Buffer{}
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buffer, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	return buffer
}
