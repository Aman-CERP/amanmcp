// Package logging provides opt-in file-based logging with rotation for AmanMCP.
// When the --debug flag is set, comprehensive logs are written to ~/.amanmcp/logs/
// for debugging and troubleshooting.
//
// By default (without --debug), logging is minimal and goes to stderr only,
// preserving the "It Just Works" philosophy.
package logging
