// Package gitignore provides gitignore pattern matching functionality.
//
// It implements the gitignore pattern syntax as documented at:
// https://git-scm.com/docs/gitignore
//
// Features:
//   - Basic pattern matching (*.log, temp/)
//   - Wildcard patterns (*, ?, **)
//   - Rooted patterns (/build)
//   - Negation patterns (!important.log)
//   - Directory-only patterns (build/)
//   - Nested gitignore file support
//   - Thread-safe matching
//
// Usage:
//
//	m := gitignore.New()
//	m.AddPattern("*.log")
//	m.AddPattern("!important.log")
//	m.AddPattern("/build/")
//
//	if m.Match("error.log", false) {
//	    // File is ignored
//	}
//
// For nested gitignore files:
//
//	m.AddFromFile("/path/to/project/.gitignore", "")
//	m.AddFromFile("/path/to/project/src/.gitignore", "src")
package gitignore
