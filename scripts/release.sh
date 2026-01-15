#!/bin/bash
# AmanMCP Release Script
#
# Usage:
#   ./scripts/release.sh v1.0.0
#   ./scripts/release.sh v1.0.0-beta.1
#
# This script:
# 1. Validates version format (semver)
# 2. Updates VERSION file
# 3. Runs CI checks
# 4. Creates git tag
# 5. Pushes to trigger release workflow

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Print colored output
info() { echo -e "${GREEN}[INFO]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# Update README.md version badge
update_readme_badge() {
    local version="$1"
    info "Updating README.md version badge to $version..."

    if [[ ! -f "README.md" ]]; then
        warn "README.md not found, skipping badge update"
        return
    fi

    # Update version badge: version-X.Y.Z -> version-NEW
    sed -i.bak "s/version-[0-9]*\.[0-9]*\.[0-9]*/version-${version}/g" README.md
    rm -f README.md.bak

    info "README.md badge updated"
}

# Create version changelog from unreleased.md
create_version_changelog() {
    local version="$1"
    local major_minor=$(echo "$version" | grep -oE '^[0-9]+\.[0-9]+')
    local changelog_dir="docs/changelog/${major_minor}"
    local changelog_file="${changelog_dir}/v${version}.md"
    local unreleased_file="docs/changelog/unreleased.md"
    local prev_version=""

    info "Creating changelog for v${version}..."

    # Create directory if needed
    mkdir -p "$changelog_dir"

    # Find previous version for "Previous Release" link
    prev_version=$(ls -1 "${changelog_dir}"/v*.md 2>/dev/null | sort -V | tail -1 | xargs basename 2>/dev/null | sed 's/\.md$//' || echo "")

    # Check if unreleased.md has content
    if [[ ! -f "$unreleased_file" ]]; then
        warn "unreleased.md not found, creating minimal changelog"
        cat > "$changelog_file" << EOF
# v${version}

**Release Date:** $(date +%Y-%m-%d)

---

## Changes

See git history for details.

---

## Links

- [Full Changelog](../CHANGELOG.md)
EOF
        return
    fi

    # Extract content sections from unreleased.md (between first --- and Notes section)
    local content=$(awk '/^---$/{p=1; next} /^## Notes/{exit} p' "$unreleased_file" | sed '/^$/N;/^\n$/d')

    # Check if there's actual content (not just empty headers)
    local has_content=$(echo "$content" | grep -E '^- ' || true)

    if [[ -z "$has_content" ]]; then
        warn "No changes in unreleased.md, creating minimal changelog"
        cat > "$changelog_file" << EOF
# v${version}

**Release Date:** $(date +%Y-%m-%d)

---

## Changes

Minor updates and fixes.

---

## Links

- [Full Changelog](../CHANGELOG.md)
EOF
    else
        # Create changelog with content
        cat > "$changelog_file" << EOF
# v${version}

**Release Date:** $(date +%Y-%m-%d)

---

${content}

---

## Links

- [Full Changelog](../CHANGELOG.md)
EOF
        # Add previous release link if exists
        if [[ -n "$prev_version" ]]; then
            echo "- [Previous Release: ${prev_version}](${prev_version}.md)" >> "$changelog_file"
        fi
    fi

    info "Created $changelog_file"
}

# Update main CHANGELOG.md index
update_changelog_index() {
    local version="$1"
    local major_minor=$(echo "$version" | grep -oE '^[0-9]+\.[0-9]+')
    local changelog_index="docs/changelog/CHANGELOG.md"
    local date_str=$(date +%Y-%m-%d)

    info "Updating CHANGELOG.md index..."

    if [[ ! -f "$changelog_index" ]]; then
        warn "CHANGELOG.md not found, skipping index update"
        return
    fi

    # Check if version already in changelog
    if grep -q "v${version}" "$changelog_index"; then
        info "v${version} already in CHANGELOG.md"
        return
    fi

    # Find the line with "## Releases" or first version entry and insert after
    # Insert new version entry after the header section
    local temp_file=$(mktemp)
    local inserted=false

    while IFS= read -r line; do
        echo "$line" >> "$temp_file"
        # Insert after "## Releases" or "## Version History" header
        if [[ "$inserted" == "false" ]] && [[ "$line" =~ ^##.*[Rr]elease|^##.*[Vv]ersion ]]; then
            echo "" >> "$temp_file"
            echo "### [v${version}](${major_minor}/v${version}.md) - ${date_str}" >> "$temp_file"
            inserted=true
        fi
    done < "$changelog_index"

    # If we couldn't find the right place, insert near the top
    if [[ "$inserted" == "false" ]]; then
        # Insert after first blank line after title
        awk -v ver="v${version}" -v mm="${major_minor}" -v dt="${date_str}" '
            NR==1 {print; next}
            /^$/ && !inserted {
                print ""
                print "### [" ver "](" mm "/" ver ".md) - " dt
                inserted=1
            }
            {print}
        ' "$changelog_index" > "$temp_file"
    fi

    mv "$temp_file" "$changelog_index"
    info "CHANGELOG.md updated"
}

# Reset unreleased.md to template
reset_unreleased() {
    local unreleased_file="docs/changelog/unreleased.md"

    info "Resetting unreleased.md..."

    cat > "$unreleased_file" << 'EOF'
# Unreleased Changes

Changes that will be included in the next release.

---

## Added

## Changed

## Fixed

## Removed

## Documentation

---

## Notes

This file is reset after each version release.

Add changes here as you work. Use present tense ("Add feature" not "Added feature").

Categories:

- **Added**: New features
- **Changed**: Changes in existing functionality
- **Deprecated**: Soon-to-be removed features
- **Removed**: Removed features
- **Fixed**: Bug fixes
- **Security**: Security-related changes
- **Documentation**: Documentation-only changes
EOF

    info "unreleased.md reset to template"
}

# Get version from argument
VERSION="${1:-}"

if [ -z "$VERSION" ]; then
    echo "Usage: ./scripts/release.sh <version>"
    echo ""
    echo "Examples:"
    echo "  ./scripts/release.sh v1.0.0"
    echo "  ./scripts/release.sh v1.0.0-beta.1"
    echo "  ./scripts/release.sh v1.0.0-rc.1"
    exit 1
fi

# Validate version format (must start with v, followed by semver)
if ! [[ $VERSION =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$ ]]; then
    error "Invalid version format: $VERSION"
    echo "Expected format: vX.Y.Z or vX.Y.Z-suffix"
    echo "Examples: v1.0.0, v1.0.0-beta.1, v1.0.0-rc.1"
    exit 1
fi

# Strip 'v' prefix for VERSION file
VERSION_NUMBER="${VERSION#v}"

info "Preparing release $VERSION..."

# Check for uncommitted changes
if ! git diff-index --quiet HEAD --; then
    error "Uncommitted changes detected. Please commit or stash them first."
fi

# Check we're on main branch
BRANCH=$(git rev-parse --abbrev-ref HEAD)
if [ "$BRANCH" != "main" ]; then
    warn "You are on branch '$BRANCH', not 'main'."
    read -p "Continue anyway? [y/N] " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

# Check if tag already exists
if git rev-parse "$VERSION" >/dev/null 2>&1; then
    error "Tag $VERSION already exists!"
fi

# Update VERSION file
info "Updating VERSION file to $VERSION_NUMBER..."
echo "$VERSION_NUMBER" > VERSION

# Update all documentation with new version
info "Updating documentation..."
update_readme_badge "$VERSION_NUMBER"
create_version_changelog "$VERSION_NUMBER"
update_changelog_index "$VERSION_NUMBER"
reset_unreleased

# Verify version command works
info "Verifying version command..."
if ! go run ./cmd/amanmcp version | grep -q "$VERSION_NUMBER"; then
    warn "Version command didn't show expected version. Continuing anyway..."
fi

# Run CI checks
info "Running CI checks (this may take a few minutes)..."
if ! make ci-check; then
    error "CI checks failed! Fix issues before releasing."
fi

info "CI checks passed!"

# Commit all release changes
info "Committing release changes..."
git add VERSION README.md docs/changelog/
git commit -m "chore: release $VERSION

- Update VERSION to $VERSION_NUMBER
- Update README.md version badge
- Create docs/changelog/$(echo "$VERSION_NUMBER" | grep -oE '^[0-9]+\.[0-9]+')/v${VERSION_NUMBER}.md
- Update CHANGELOG.md index
- Reset unreleased.md

🤖 Generated with [Claude Code](https://claude.com/claude-code)"

# Create annotated tag
info "Creating tag $VERSION..."
git tag -a "$VERSION" -m "Release $VERSION

See CHANGELOG for details.

🤖 Generated with [Claude Code](https://claude.com/claude-code)"

# Push
info "Pushing to origin..."
git push origin main
git push origin "$VERSION"

echo ""
info "Release $VERSION triggered successfully!"
echo ""
echo "Monitor the release workflow at:"
echo "  https://github.com/Aman-CERP/amanmcp/actions"
echo ""
echo "Once complete, the release will be available at:"
echo "  https://github.com/Aman-CERP/amanmcp/releases/tag/$VERSION"
