// Package configs provides embedded configuration templates for amanmcp.
//
// How Configuration Templates Work:
//
// Templates are embedded at build time using Go's //go:embed directive.
// This ensures they are available in ALL distributions:
//   - Source builds (go install)
//   - Binary releases
//   - Homebrew installations
//
// The templates are used by:
//   - cmd/amanmcp/cmd/init.go → generateAmanmcpYAML() - creates .amanmcp.yaml
//   - cmd/amanmcp/cmd/config.go → creates user config at ~/.config/amanmcp/config.yaml
//
// Template files:
//   - project-config.example.yaml: Project-specific settings (paths, search, submodules)
//   - user-config.example.yaml: Machine-specific settings (thermal, Ollama host, MLX)
//
// Configuration Hierarchy (see internal/config/config.go Load()):
//   1. Hardcoded defaults (internal/config/config.go NewConfig())
//   2. User config (~/.config/amanmcp/config.yaml)
//   3. Project config (.amanmcp.yaml)
//   4. Environment variables (AMANMCP_*)
//
// To modify templates, edit the .yaml files in this directory and rebuild.
// Changes will be embedded in the next build.
package configs

import _ "embed"

// UserConfigTemplate is the template for user/machine-level configuration.
// Created by: `amanmcp config init` at ~/.config/amanmcp/config.yaml
// Contains: Machine-specific settings like thermal management, Ollama host, MLX endpoint.
// Use case: Settings that apply to all projects on this machine.
//
//go:embed user-config.example.yaml
var UserConfigTemplate string

// ProjectConfigTemplate is the template for project-level configuration.
// Created by: `amanmcp init` at .amanmcp.yaml in the project root
// Contains: Project-specific settings like paths.exclude, search weights, submodules.
// Use case: Settings that are version-controlled with the project.
//
// Important: The template includes commented examples showing how to exclude
// project management directories (like .aman-pm/) to prevent search pollution.
// See: configs/project-config.example.yaml for the full template.
//
//go:embed project-config.example.yaml
var ProjectConfigTemplate string
