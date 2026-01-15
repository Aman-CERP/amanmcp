package watcher

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperation_Constants(t *testing.T) {
	// Given: Operation constants
	// Then: they are distinct values
	assert.NotEqual(t, OpCreate, OpModify)
	assert.NotEqual(t, OpCreate, OpDelete)
	assert.NotEqual(t, OpCreate, OpRename)
	assert.NotEqual(t, OpModify, OpDelete)
	assert.NotEqual(t, OpModify, OpRename)
	assert.NotEqual(t, OpDelete, OpRename)
}

func TestOperation_String(t *testing.T) {
	tests := []struct {
		name string
		op   Operation
		want string
	}{
		{"create", OpCreate, "CREATE"},
		{"modify", OpModify, "MODIFY"},
		{"delete", OpDelete, "DELETE"},
		{"rename", OpRename, "RENAME"},
		{"unknown", Operation(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.op.String())
		})
	}
}

func TestFileEvent_Fields(t *testing.T) {
	// Given: a file event with all fields set
	now := time.Now()
	event := FileEvent{
		Path:      "src/main.go",
		OldPath:   "src/old.go",
		Operation: OpRename,
		IsDir:     false,
		Timestamp: now,
	}

	// Then: all fields are accessible
	assert.Equal(t, "src/main.go", event.Path)
	assert.Equal(t, "src/old.go", event.OldPath)
	assert.Equal(t, OpRename, event.Operation)
	assert.False(t, event.IsDir)
	assert.Equal(t, now, event.Timestamp)
}

func TestDefaultOptions(t *testing.T) {
	// When: getting default options
	opts := DefaultOptions()

	// Then: defaults are sensible
	assert.Equal(t, 200*time.Millisecond, opts.DebounceWindow)
	assert.Equal(t, 5*time.Second, opts.PollInterval)
	assert.Equal(t, 1000, opts.EventBufferSize)
	assert.Nil(t, opts.IgnorePatterns)
}

func TestOptions_Validate(t *testing.T) {
	// Given: default options
	opts := DefaultOptions()

	// When: validating
	err := opts.Validate()

	// Then: no error
	require.NoError(t, err)
}

func TestOptions_WithDefaults(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want Options
	}{
		{
			name: "empty options get defaults",
			opts: Options{},
			want: DefaultOptions(),
		},
		{
			name: "partial options keep custom values",
			opts: Options{
				DebounceWindow: 500 * time.Millisecond,
			},
			want: Options{
				DebounceWindow:  500 * time.Millisecond,
				PollInterval:    5 * time.Second,
				EventBufferSize: 1000,
			},
		},
		{
			name: "all custom values preserved",
			opts: Options{
				DebounceWindow:  100 * time.Millisecond,
				PollInterval:    10 * time.Second,
				EventBufferSize: 500,
				IgnorePatterns:  []string{"*.tmp"},
			},
			want: Options{
				DebounceWindow:  100 * time.Millisecond,
				PollInterval:    10 * time.Second,
				EventBufferSize: 500,
				IgnorePatterns:  []string{"*.tmp"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.opts.WithDefaults()
			assert.Equal(t, tt.want.DebounceWindow, got.DebounceWindow)
			assert.Equal(t, tt.want.PollInterval, got.PollInterval)
			assert.Equal(t, tt.want.EventBufferSize, got.EventBufferSize)
			assert.Equal(t, tt.want.IgnorePatterns, got.IgnorePatterns)
		})
	}
}
