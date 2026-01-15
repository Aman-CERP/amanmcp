package telemetry

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	require.NoError(t, err)

	// Initialize telemetry schema
	err = InitTelemetrySchema(db)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func TestSQLiteMetricsStore_SaveQueryTypeCounts(t *testing.T) {
	db := setupTestDB(t)
	store, err := NewSQLiteMetricsStore(db)
	require.NoError(t, err)

	counts := map[QueryType]int64{
		QueryTypeSemantic: 10,
		QueryTypeLexical:  5,
		QueryTypeMixed:    3,
	}

	err = store.SaveQueryTypeCounts("2026-01-06", counts)
	require.NoError(t, err)

	// Verify by querying back
	result, err := store.GetQueryTypeCounts("2026-01-06", "2026-01-06")
	require.NoError(t, err)

	assert.Equal(t, int64(10), result[QueryTypeSemantic])
	assert.Equal(t, int64(5), result[QueryTypeLexical])
	assert.Equal(t, int64(3), result[QueryTypeMixed])
}

func TestSQLiteMetricsStore_SaveQueryTypeCounts_Incremental(t *testing.T) {
	db := setupTestDB(t)
	store, err := NewSQLiteMetricsStore(db)
	require.NoError(t, err)

	// First save
	err = store.SaveQueryTypeCounts("2026-01-06", map[QueryType]int64{
		QueryTypeSemantic: 10,
	})
	require.NoError(t, err)

	// Second save should increment
	err = store.SaveQueryTypeCounts("2026-01-06", map[QueryType]int64{
		QueryTypeSemantic: 5,
	})
	require.NoError(t, err)

	result, err := store.GetQueryTypeCounts("2026-01-06", "2026-01-06")
	require.NoError(t, err)

	assert.Equal(t, int64(15), result[QueryTypeSemantic])
}

func TestSQLiteMetricsStore_UpsertTermCounts(t *testing.T) {
	db := setupTestDB(t)
	store, err := NewSQLiteMetricsStore(db)
	require.NoError(t, err)

	terms := map[string]int64{
		"error":   10,
		"handler": 5,
		"search":  3,
	}

	err = store.UpsertTermCounts(terms)
	require.NoError(t, err)

	// Get top terms
	result, err := store.GetTopTerms(10)
	require.NoError(t, err)

	assert.Len(t, result, 3)
	assert.Equal(t, "error", result[0].Term)
	assert.Equal(t, int64(10), result[0].Count)
}

func TestSQLiteMetricsStore_UpsertTermCounts_Incremental(t *testing.T) {
	db := setupTestDB(t)
	store, err := NewSQLiteMetricsStore(db)
	require.NoError(t, err)

	// First upsert
	err = store.UpsertTermCounts(map[string]int64{"error": 10})
	require.NoError(t, err)

	// Second upsert should add
	err = store.UpsertTermCounts(map[string]int64{"error": 5})
	require.NoError(t, err)

	result, err := store.GetTopTerms(1)
	require.NoError(t, err)

	assert.Equal(t, int64(15), result[0].Count)
}

func TestSQLiteMetricsStore_GetTopTerms_Limit(t *testing.T) {
	db := setupTestDB(t)
	store, err := NewSQLiteMetricsStore(db)
	require.NoError(t, err)

	terms := map[string]int64{
		"a": 1, "b": 2, "c": 3, "d": 4, "e": 5,
	}
	err = store.UpsertTermCounts(terms)
	require.NoError(t, err)

	result, err := store.GetTopTerms(3)
	require.NoError(t, err)

	assert.Len(t, result, 3)
	// Should be sorted by count descending
	assert.Equal(t, "e", result[0].Term)
	assert.Equal(t, "d", result[1].Term)
	assert.Equal(t, "c", result[2].Term)
}

func TestSQLiteMetricsStore_ZeroResultQueries(t *testing.T) {
	db := setupTestDB(t)
	store, err := NewSQLiteMetricsStore(db)
	require.NoError(t, err)

	now := time.Now()

	err = store.AddZeroResultQuery("missing function", now)
	require.NoError(t, err)

	err = store.AddZeroResultQuery("nonexistent class", now.Add(time.Minute))
	require.NoError(t, err)

	result, err := store.GetZeroResultQueries(10)
	require.NoError(t, err)

	assert.Len(t, result, 2)
	// Should be most recent first
	assert.Equal(t, "nonexistent class", result[0])
	assert.Equal(t, "missing function", result[1])
}

func TestSQLiteMetricsStore_ZeroResultQueries_CircularBuffer(t *testing.T) {
	db := setupTestDB(t)
	store, err := NewSQLiteMetricsStore(db)
	require.NoError(t, err)

	now := time.Now()

	// Add 105 queries - should trim to 100
	for i := 0; i < 105; i++ {
		err = store.AddZeroResultQuery("query"+string(rune('A'+i%26)), now.Add(time.Duration(i)*time.Second))
		require.NoError(t, err)
	}

	result, err := store.GetZeroResultQueries(200) // Ask for more than exists
	require.NoError(t, err)

	assert.Len(t, result, 100)
}

func TestSQLiteMetricsStore_LatencyCounts(t *testing.T) {
	db := setupTestDB(t)
	store, err := NewSQLiteMetricsStore(db)
	require.NoError(t, err)

	counts := map[LatencyBucket]int64{
		BucketP10:   100,
		BucketP50:   50,
		BucketP100:  25,
		BucketP500:  10,
		BucketP1000: 5,
	}

	err = store.SaveLatencyCounts("2026-01-06", counts)
	require.NoError(t, err)

	result, err := store.GetLatencyCounts("2026-01-06", "2026-01-06")
	require.NoError(t, err)

	assert.Equal(t, int64(100), result[BucketP10])
	assert.Equal(t, int64(50), result[BucketP50])
	assert.Equal(t, int64(25), result[BucketP100])
	assert.Equal(t, int64(10), result[BucketP500])
	assert.Equal(t, int64(5), result[BucketP1000])
}

func TestSQLiteMetricsStore_LatencyCounts_Incremental(t *testing.T) {
	db := setupTestDB(t)
	store, err := NewSQLiteMetricsStore(db)
	require.NoError(t, err)

	err = store.SaveLatencyCounts("2026-01-06", map[LatencyBucket]int64{BucketP10: 10})
	require.NoError(t, err)

	err = store.SaveLatencyCounts("2026-01-06", map[LatencyBucket]int64{BucketP10: 5})
	require.NoError(t, err)

	result, err := store.GetLatencyCounts("2026-01-06", "2026-01-06")
	require.NoError(t, err)

	assert.Equal(t, int64(15), result[BucketP10])
}

func TestSQLiteMetricsStore_DateRange(t *testing.T) {
	db := setupTestDB(t)
	store, err := NewSQLiteMetricsStore(db)
	require.NoError(t, err)

	// Save data for multiple days
	err = store.SaveQueryTypeCounts("2026-01-05", map[QueryType]int64{QueryTypeSemantic: 10})
	require.NoError(t, err)

	err = store.SaveQueryTypeCounts("2026-01-06", map[QueryType]int64{QueryTypeSemantic: 20})
	require.NoError(t, err)

	err = store.SaveQueryTypeCounts("2026-01-07", map[QueryType]int64{QueryTypeSemantic: 30})
	require.NoError(t, err)

	// Query range
	result, err := store.GetQueryTypeCounts("2026-01-05", "2026-01-06")
	require.NoError(t, err)

	assert.Equal(t, int64(30), result[QueryTypeSemantic]) // 10 + 20
}

func TestNewSQLiteMetricsStore_NilDB(t *testing.T) {
	_, err := NewSQLiteMetricsStore(nil)
	assert.Error(t, err)
}

func TestSQLiteMetricsStore_EmptyTerms(t *testing.T) {
	db := setupTestDB(t)
	store, err := NewSQLiteMetricsStore(db)
	require.NoError(t, err)

	// Empty map should be no-op
	err = store.UpsertTermCounts(map[string]int64{})
	require.NoError(t, err)
}
