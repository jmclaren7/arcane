package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContainerAutoUpdateFilter(t *testing.T) {
	t.Run("exclude mode treats the list as a denylist", func(t *testing.T) {
		filter := NewContainerAutoUpdateFilter("app, db", false)

		assert.False(t, filter.IncludeMode())
		assert.True(t, filter.Excludes([]string{"/db"}))
		assert.False(t, filter.Excludes([]string{"/other"}))
		assert.ElementsMatch(t, []string{"app", "db"}, filter.ListedNames())
	})

	t.Run("include mode treats the list as an allowlist", func(t *testing.T) {
		filter := NewContainerAutoUpdateFilter("app, db", true)

		assert.True(t, filter.IncludeMode())
		assert.False(t, filter.Excludes([]string{"/db"}))
		assert.True(t, filter.Excludes([]string{"/other"}))
		assert.True(t, filter.Lists("app"))
		assert.False(t, filter.Lists("other"))
	})

	t.Run("include mode with an empty list excludes everything", func(t *testing.T) {
		filter := NewContainerAutoUpdateFilter("", true)

		assert.True(t, filter.Excludes([]string{"/app"}))
		assert.Nil(t, filter.ListedNames())
	})

	t.Run("an unset filter excludes nothing", func(t *testing.T) {
		var filter ContainerAutoUpdateFilter

		assert.False(t, filter.Excludes([]string{"/app"}))
	})
}
