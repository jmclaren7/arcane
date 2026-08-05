package types

import (
	composetypes "github.com/compose-spec/compose-go/v2/types"
)

// VolumeSourcePathMapper translates Compose volume sources for the Docker host.
type VolumeSourcePathMapper interface {
	TranslateVolumeSources(project *composetypes.Project, translateFileResources bool) error
	// ContainerToHost translates a single container-side path to its host-side
	// equivalent, returning the path unchanged when it is outside every mounted
	// directory. Needed to re-resolve relative Compose paths that escape the
	// projects mount, where prefix translation has nothing to match.
	ContainerToHost(containerPath string) (string, error)
	// IsPathMounted reports whether a container-side path lies inside a mounted
	// directory. A matching mount translates a path to itself, so this is the
	// only way to tell "mounted, unchanged" from "outside every mount".
	IsPathMounted(containerPath string) bool
}

// ComposeContentOptions configures loading a Compose project from in-memory content.
type ComposeContentOptions struct {
	ProjectName     string
	ComposeContent  string
	OverrideContent string
	EnvContent      string
	WorkingDir      string
	PathMapper      VolumeSourcePathMapper
}
