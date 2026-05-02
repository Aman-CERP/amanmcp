package language

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultRegistry_ReusesReadOnlyInstance(t *testing.T) {
	first := DefaultRegistry()
	second := DefaultRegistry()

	assert.Same(t, first, second)
}

func TestDefaultRegistry_RepeatedCallsDoNotAllocate(t *testing.T) {
	_ = DefaultRegistry()

	allocs := testing.AllocsPerRun(100, func() {
		_ = DefaultRegistry()
	})

	assert.Zero(t, allocs)
}

func TestRegistryContentType_UsesDeterministicLookup(t *testing.T) {
	registry := DefaultRegistry()

	for i := 0; i < 100; i++ {
		assert.Equal(t, ContentTypeCode, registry.ContentType("typescript"))
		assert.Equal(t, ContentTypeCode, registry.ContentType("tsx"))
		assert.Equal(t, ContentTypeMarkdown, registry.ContentType("markdown"))
	}
}
