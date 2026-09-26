package docker

import (
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

// ContainerNameFromNames returns Docker's first container name without the
// leading slash Docker stores in container summaries.
func ContainerNameFromNames(names []string) string {
	if len(names) == 0 {
		return ""
	}

	return strings.TrimPrefix(names[0], "/")
}

// ExcludedContainerNameSet parses a comma-separated exclusion setting into a
// name lookup. Returns nil when the setting names no containers.
func ExcludedContainerNameSet(raw string) map[string]bool {
	names := utils.UniqueNonEmptyStrings(strings.Split(raw, ","))
	if len(names) == 0 {
		return nil
	}
	excluded := make(map[string]bool, len(names))
	for _, name := range names {
		excluded[name] = true
	}
	return excluded
}

// ContainerNameExcluded reports whether any of Docker's container names,
// stripped of the leading slash, appears in the exclusion lookup.
func ContainerNameExcluded(names []string, excluded map[string]bool) bool {
	if len(excluded) == 0 {
		return false
	}
	for _, name := range names {
		if excluded[strings.TrimPrefix(name, "/")] {
			return true
		}
	}
	return false
}

// ContainerAutoUpdateFilter is an automation's container list plus the mode
// deciding whether the listed names are excluded from the automation or are the
// only ones included in it.
type ContainerAutoUpdateFilter struct {
	listed      map[string]bool
	includeMode bool
}

// NewContainerAutoUpdateFilter parses an "excluded containers" setting together
// with its include-mode switch. In include mode the names are an allowlist: only
// they are affected and every other container is excluded, so an empty list in
// include mode excludes every container.
func NewContainerAutoUpdateFilter(raw string, includeMode bool) ContainerAutoUpdateFilter {
	return ContainerAutoUpdateFilter{listed: ExcludedContainerNameSet(raw), includeMode: includeMode}
}

// Excludes reports whether Docker's container names put the container outside the
// automation under the configured mode.
func (f ContainerAutoUpdateFilter) Excludes(names []string) bool {
	if f.includeMode {
		return !ContainerNameExcluded(names, f.listed)
	}
	return ContainerNameExcluded(names, f.listed)
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
