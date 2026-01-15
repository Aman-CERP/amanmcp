#!/usr/bin/env bash
#
# CI Parity Check Script for AmanMCP
#
# Purpose: Ensures local environment matches GitHub Actions CI exactly.
# Usage:
#   ./scripts/ci-parity-check.sh --full    # Full CI validation (parallel)
#   ./scripts/ci-parity-check.sh --quick   # Quick validation (tests + lint)
#   ./scripts/ci-parity-check.sh --commit  # Fast commit check (< 10s target)
#
# Exit Codes:
#   0 - All checks passed
#   1 - Tests failed
#   2 - Lint failed
#   3 - Coverage below threshold
#   4 - Build failed
#   5 - Security issues found

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration (match Makefile - Single Source of Truth)
GO_MIN_VERSION="1.24"
GO_EXACT_VERSION="1.25.5"
GOLANGCI_LINT_VERSION="v2.1.6"
GOVULNCHECK_VERSION="v1.1.4"  # Updated for Go 1.25 compatibility
COVERAGE_THRESHOLD=25

# Parse arguments
MODE="${1:---full}"

# Helper functions
log_step() {
    echo -e "${YELLOW}▶ $1${NC}"
}

log_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

log_error() {
    echo -e "${RED}✗ $1${NC}"
}

log_info() {
    echo -e "  $1"
}

log_parallel() {
    echo -e "${BLUE}⚡ $1${NC}"
}

# Check Go version
check_go_version() {
    log_step "Checking Go version..."

    GO_VERSION=$(go version | grep -oE 'go[0-9]+\.[0-9]+(\.[0-9]+)?' | sed 's/go//')

    # Check minimum version
    if [[ "$(printf '%s\n' "$GO_MIN_VERSION" "$GO_VERSION" | sort -V | head -n1)" != "$GO_MIN_VERSION" ]]; then
        log_error "Go version $GO_VERSION is below minimum $GO_MIN_VERSION"
        exit 1
    fi

    log_success "Go version check passed ($GO_VERSION >= $GO_MIN_VERSION)"
}

# Download dependencies
download_deps() {
    log_step "Downloading dependencies..."

    if ! go mod download; then
        log_error "Failed to download dependencies"
        exit 1
    fi

    log_success "Dependencies downloaded"
}

# Run unit tests
run_tests() {
    log_step "Running unit tests with race detector..."

    # Exclude validation package - those are integration tests requiring a proper index
    # Run validation separately with: make validate or make validate-all
    PACKAGES=$(go list ./... | grep -v '/validation$')

    if ! go test -race -coverprofile=coverage.out -covermode=atomic $PACKAGES; then
        log_error "Unit tests failed"
        exit 1
    fi

    log_success "Unit tests passed"
}

# Run unit tests without race detector (fast mode)
run_tests_fast() {
    log_step "Running quick compile check..."

    # Just verify it compiles - don't run full tests
    if ! go build -o /dev/null ./cmd/amanmcp; then
        log_error "Build failed"
        exit 4
    fi

    log_success "Build check passed"
}

# Check coverage threshold
check_coverage() {
    log_step "Checking coverage threshold..."

    if [[ ! -f coverage.out ]]; then
        log_info "No coverage file found, skipping coverage check"
        return
    fi

    COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')

    if [[ -z "$COVERAGE" ]]; then
        log_info "No coverage data available"
        return
    fi

    # Use bc for float comparison
    if command -v bc &> /dev/null; then
        if (( $(echo "$COVERAGE < $COVERAGE_THRESHOLD" | bc -l) )); then
            log_error "Coverage $COVERAGE% is below threshold of $COVERAGE_THRESHOLD%"
            exit 3
        fi
    else
        # Fallback: integer comparison
        COVERAGE_INT=${COVERAGE%.*}
        if [[ $COVERAGE_INT -lt $COVERAGE_THRESHOLD ]]; then
            log_error "Coverage $COVERAGE% is below threshold of $COVERAGE_THRESHOLD%"
            exit 3
        fi
    fi

    log_success "Coverage: $COVERAGE% (threshold: $COVERAGE_THRESHOLD%)"
}

# Run linting (full)
run_lint() {
    log_step "Running golangci-lint..."

    # Use go run for portability - reduced timeout from 5m to 2m
    if ! go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION} run --timeout=2m; then
        log_error "Lint failed"
        exit 2
    fi

    log_success "Lint passed"
}

# Run linting (fast - only staged files)
run_lint_fast() {
    log_step "Running lint on staged files..."

    # Get staged Go files
    STAGED_FILES=$(git diff --cached --name-only --diff-filter=ACMR -- '*.go' 2>/dev/null || true)

    if [[ -z "$STAGED_FILES" ]]; then
        log_success "No Go files staged, skipping lint"
        return
    fi

    # Lint only changed files relative to HEAD
    if ! go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION} run --timeout=30s --new-from-rev=HEAD~1 2>/dev/null; then
        # Fallback: lint staged files directly if new-from-rev fails
        if ! go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION} run --timeout=30s $STAGED_FILES; then
            log_error "Lint failed on staged files"
            exit 2
        fi
    fi

    log_success "Lint passed (staged files)"
}

# Run security scan
run_security() {
    log_step "Running security scan (govulncheck)..."

    # Pinned version for consistent performance
    if ! go run golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION} ./...; then
        log_error "Security vulnerabilities found"
        exit 5
    fi

    log_success "Security scan clean"
}

# Build verification
run_build() {
    log_step "Verifying build..."

    if ! go build -o /dev/null ./cmd/amanmcp 2>/dev/null || ! go build -o /dev/null ./... 2>/dev/null; then
        # If cmd/amanmcp doesn't exist yet, just verify all packages compile
        if ! go build ./... 2>/dev/null; then
            log_error "Build failed"
            exit 4
        fi
    fi

    log_success "Build succeeded"
}

# Run parallel checks (tests, lint, security)
run_parallel_checks() {
    log_parallel "Running tests, lint, and security scan IN PARALLEL..."
    echo ""

    # Exclude validation package - those are integration tests requiring a proper index
    # Run validation separately with: make validate or make validate-all
    PACKAGES=$(go list ./... | grep -v '/validation$')

    # Create temp directory for outputs
    TEMP_DIR=$(mktemp -d)
    trap "rm -rf $TEMP_DIR" EXIT

    # Run all checks in parallel
    (
        log_step "[PARALLEL] Running unit tests with race detector..."
        if go test -race -coverprofile=coverage.out -covermode=atomic $PACKAGES > "$TEMP_DIR/test.log" 2>&1; then
            echo "0" > "$TEMP_DIR/test.exit"
            log_success "[PARALLEL] Unit tests passed"
        else
            echo "1" > "$TEMP_DIR/test.exit"
            log_error "[PARALLEL] Unit tests failed"
        fi
    ) &
    PID_TEST=$!

    (
        log_step "[PARALLEL] Running golangci-lint..."
        if go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION} run --timeout=2m > "$TEMP_DIR/lint.log" 2>&1; then
            echo "0" > "$TEMP_DIR/lint.exit"
            log_success "[PARALLEL] Lint passed"
        else
            echo "2" > "$TEMP_DIR/lint.exit"
            log_error "[PARALLEL] Lint failed"
        fi
    ) &
    PID_LINT=$!

    (
        log_step "[PARALLEL] Running security scan..."
        if go run golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION} ./... > "$TEMP_DIR/sec.log" 2>&1; then
            echo "0" > "$TEMP_DIR/sec.exit"
            log_success "[PARALLEL] Security scan clean"
        else
            echo "5" > "$TEMP_DIR/sec.exit"
            log_error "[PARALLEL] Security issues found"
        fi
    ) &
    PID_SEC=$!

    # Wait for all parallel jobs
    wait $PID_TEST $PID_LINT $PID_SEC || true

    echo ""
    log_parallel "Parallel checks completed. Checking results..."
    echo ""

    # Check results and show failures
    FAILED=0

    if [[ "$(cat $TEMP_DIR/test.exit 2>/dev/null || echo 1)" != "0" ]]; then
        log_error "Tests failed. Output:"
        cat "$TEMP_DIR/test.log" 2>/dev/null || true
        FAILED=1
    fi

    if [[ "$(cat $TEMP_DIR/lint.exit 2>/dev/null || echo 2)" != "0" ]]; then
        log_error "Lint failed. Output:"
        cat "$TEMP_DIR/lint.log" 2>/dev/null || true
        FAILED=2
    fi

    if [[ "$(cat $TEMP_DIR/sec.exit 2>/dev/null || echo 5)" != "0" ]]; then
        log_error "Security scan failed. Output:"
        cat "$TEMP_DIR/sec.log" 2>/dev/null || true
        FAILED=5
    fi

    if [[ $FAILED -ne 0 ]]; then
        exit $FAILED
    fi

    log_success "All parallel checks passed"
}

# Fast commit check (target: < 10 seconds)
run_commit_check() {
    echo ""
    echo "========================================"
    echo "  AmanMCP Fast Commit Check"
    echo "  Target: < 10 seconds"
    echo "========================================"
    echo ""

    START_TIME=$(date +%s)

    check_go_version
    run_tests_fast
    run_lint_fast

    END_TIME=$(date +%s)
    DURATION=$((END_TIME - START_TIME))

    echo ""
    echo "========================================"
    echo -e "${GREEN}✓ FAST COMMIT CHECK PASSED${NC}"
    echo -e "  Duration: ${DURATION}s (target: <10s)"
    echo "========================================"
    echo ""
}

# Main execution
main() {
    case "$MODE" in
        --commit)
            run_commit_check
            ;;
        --quick)
            echo ""
            echo "========================================"
            echo "  AmanMCP CI Parity Check"
            echo "  Mode: Quick (tests + lint)"
            echo "========================================"
            echo ""

            check_go_version
            run_tests
            run_lint

            echo ""
            echo "========================================"
            echo -e "${GREEN}✓ CI PARITY CHECK PASSED${NC}"
            echo "========================================"
            echo ""

            # Cleanup
            rm -f coverage.out
            ;;
        --full|*)
            echo ""
            echo "========================================"
            echo "  AmanMCP CI Parity Check"
            echo "  Mode: Full (PARALLEL execution)"
            echo "========================================"
            echo ""

            check_go_version
            download_deps
            run_parallel_checks
            check_coverage
            run_build

            echo ""
            echo "========================================"
            echo -e "${GREEN}✓ CI PARITY CHECK PASSED${NC}"
            echo "========================================"
            echo ""

            # Cleanup
            rm -f coverage.out
            ;;
    esac
}

main "$@"
