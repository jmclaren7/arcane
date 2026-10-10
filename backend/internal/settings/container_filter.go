package settings

import (
	docker "go.getarcane.app/docker"
)

// ContainerAutoUpdateFilter is an automation's container list plus the mode
// deciding whether the listed names are excluded from the automation or are the
// only ones included in it.
//
// It lives beside the settings service rather than next to Docker's own
// ExcludedContainerNameSet / ContainerNameExcluded helpers, because those moved
// into the external go.getarcane.app/docker module, which the fork cannot extend.
// Keeping the inversion in exactly one place is what stops a consumer reading
// the list as a denylist and silently inverting the setting.
type ContainerAutoUpdateFilter struct {
	listed      map[string]bool
	includeMode bool
}

// NewContainerAutoUpdateFilter parses an "excluded containers" setting together
// with its include-mode switch. In include mode the names are an allowlist: only
// they are affected and every other container is excluded, so an empty list in
// include mode excludes every container.
func NewContainerAutoUpdateFilter(raw string, includeMode bool) ContainerAutoUpdateFilter {
	return ContainerAutoUpdateFilter{listed: docker.ExcludedContainerNameSet(raw), includeMode: includeMode}
}

// Excludes reports whether Docker's container names put the container outside the
// automation under the configured mode.
func (f ContainerAutoUpdateFilter) Excludes(names []string) bool {
	if f.includeMode {
		return !docker.ContainerNameExcluded(names, f.listed)
	}
	return docker.ContainerNameExcluded(names, f.listed)
}

// IncludeMode reports whether the list is an allowlist rather than a denylist.
func (f ContainerAutoUpdateFilter) IncludeMode() bool {
	return f.includeMode
}

// Lists reports whether the already-normalized name is on the configured list.
func (f ContainerAutoUpdateFilter) Lists(name string) bool {
	return f.listed[name]
}

// ListedNames returns the configured names in no particular order.
func (f ContainerAutoUpdateFilter) ListedNames() []string {
	if len(f.listed) == 0 {
		return nil
	}
	names := make([]string, 0, len(f.listed))
	for name := range f.listed {
		names = append(names, name)
	}
	return names
}
