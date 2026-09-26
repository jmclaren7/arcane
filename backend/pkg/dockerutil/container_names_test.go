package docker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContainerNameFromNames(t *testing.T) {
	tests := []struct {
		name  string
		names []string
		want  string
	}{
		{
			name:  "single name with slash",
			names: []string{"/myapp"},
			want:  "myapp",
		},
		{
			name:  "single name without slash",
			names: []string{"myapp"},
			want:  "myapp",
		},
		{
			name:  "multiple names uses first",
			names: []string{"/myapp", "/myapp-alias"},
			want:  "myapp",
		},
		{
			name:  "no names",
			names: []string{},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			{
				got := ContainerNameFromNames(tt.names)
				assert.Equal(t, tt.want, got,
					"ContainerNameFromNames() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExcludedContainerNames(t *testing.T) {
	assert.Nil(t, ExcludedContainerNameSet(" , "))

	excluded := ExcludedContainerNameSet(" app ,db,,app")
	assert.Equal(t, map[string]bool{"app": true, "db": true}, excluded)
	assert.True(t, ContainerNameExcluded([]string{"/other", "/db"}, excluded))
	assert.False(t, ContainerNameExcluded([]string{"/other"}, excluded))
	assert.False(t, ContainerNameExcluded([]string{"/app"}, nil))
}

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
