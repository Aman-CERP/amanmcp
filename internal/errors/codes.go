// Package errors provides structured error handling for AmanMCP.
//
// Error codes follow the pattern ERR_XXX_DESCRIPTION where:
//   - 1XX: Configuration errors
//   - 2XX: IO errors (file, disk)
//   - 3XX: Network errors
//   - 4XX: Validation errors
//   - 5XX: Internal errors
package errors

// Category defines error categories for classification.
type Category string

const (
	// CategoryConfig indicates configuration-related errors.
	CategoryConfig Category = "CONFIG"
	// CategoryIO indicates file and disk I/O errors.
	CategoryIO Category = "IO"
	// CategoryNetwork indicates network-related errors.
	CategoryNetwork Category = "NETWORK"
	// CategoryValidation indicates input validation errors.
	CategoryValidation Category = "VALIDATION"
	// CategoryInternal indicates unexpected internal errors.
	CategoryInternal Category = "INTERNAL"
)

// Severity defines error severity levels.
type Severity string

const (
	// SeverityFatal indicates unrecoverable error, must abort.
	SeverityFatal Severity = "FATAL"
	// SeverityError indicates operation failed but can continue.
	SeverityError Severity = "ERROR"
	// SeverityWarning indicates degraded operation, continuing.
	SeverityWarning Severity = "WARNING"
	// SeverityInfo indicates informational only.
	SeverityInfo Severity = "INFO"
)

// Error codes organized by category.
const (
	// Config errors (100-199)
	ErrCodeConfigNotFound   = "ERR_101_CONFIG_NOT_FOUND"
	ErrCodeConfigInvalid    = "ERR_102_CONFIG_INVALID"
	ErrCodeConfigPermission = "ERR_103_CONFIG_PERMISSION"

	// IO errors (200-299)
	ErrCodeFileNotFound   = "ERR_201_FILE_NOT_FOUND"
	ErrCodeFilePermission = "ERR_202_FILE_PERMISSION"
	ErrCodeDiskFull       = "ERR_203_DISK_FULL"
	ErrCodeFileTooLarge   = "ERR_204_FILE_TOO_LARGE"
	ErrCodeCorruptIndex   = "ERR_205_CORRUPT_INDEX"
	ErrCodeFileCorrupt    = "ERR_206_FILE_CORRUPT"

	// Network errors (300-399)
	ErrCodeNetworkTimeout     = "ERR_301_NETWORK_TIMEOUT"
	ErrCodeNetworkUnavailable = "ERR_302_NETWORK_UNAVAILABLE"
	ErrCodeModelDownload      = "ERR_303_MODEL_DOWNLOAD"

	// Validation errors (400-499)
	ErrCodeInvalidInput       = "ERR_401_INVALID_INPUT"
	ErrCodeDimensionMismatch  = "ERR_402_DIMENSION_MISMATCH"
	ErrCodeInvalidQuery       = "ERR_403_INVALID_QUERY"
	ErrCodeQueryEmpty         = "ERR_404_QUERY_EMPTY"
	ErrCodeQueryTooLong       = "ERR_405_QUERY_TOO_LONG"
	ErrCodeInvalidPath        = "ERR_406_INVALID_PATH"

	// Internal errors (500-599)
	ErrCodeInternal        = "ERR_501_INTERNAL"
	ErrCodeEmbeddingFailed = "ERR_502_EMBEDDING_FAILED"
	ErrCodeSearchFailed    = "ERR_503_SEARCH_FAILED"
	ErrCodeChunkingFailed  = "ERR_504_CHUNKING_FAILED"
	ErrCodeIndexFailed     = "ERR_505_INDEX_FAILED"
)

// categoryFromCode extracts category from error code.
func categoryFromCode(code string) Category {
	if len(code) < 7 {
		return CategoryInternal
	}

	// Extract numeric portion (e.g., "101" from "ERR_101_CONFIG_NOT_FOUND")
	numStr := code[4:7]
	if len(numStr) < 1 {
		return CategoryInternal
	}

	switch numStr[0] {
	case '1':
		return CategoryConfig
	case '2':
		return CategoryIO
	case '3':
		return CategoryNetwork
	case '4':
		return CategoryValidation
	default:
		return CategoryInternal
	}
}

// severityFromCode determines severity based on error code.
func severityFromCode(code string) Severity {
	// Fatal errors
	switch code {
	case ErrCodeCorruptIndex, ErrCodeDiskFull:
		return SeverityFatal
	}

	// Retryable network errors get warning severity
	if isRetryableCode(code) {
		return SeverityWarning
	}

	// Default to error severity
	return SeverityError
}

// isRetryableCode checks if an error code represents a retryable error.
func isRetryableCode(code string) bool {
	switch code {
	case ErrCodeNetworkTimeout, ErrCodeNetworkUnavailable, ErrCodeModelDownload:
		return true
	default:
		return false
	}
}
