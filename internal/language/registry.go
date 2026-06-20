// Package language owns AmanMCP's data-driven language registration contract.
package language

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	ContentTypeCode     = "code"
	ContentTypeMarkdown = "markdown"
	ContentTypePDF      = "pdf"
	ContentTypeText     = "text"
	ContentTypeConfig   = "config"

	ParserGo           = "go"
	ParserTypeScript   = "typescript"
	ParserTSX          = "tsx"
	ParserJavaScript   = "javascript"
	ParserPython       = "python"
	ParserLineFallback = "line_fallback"

	SourceBuiltin = "builtin"
	SourceConfig  = "config"
)

// Definition describes how a language is detected and, for code, parsed.
type Definition struct {
	Name            string   `yaml:"name" json:"name"`
	Extensions      []string `yaml:"extensions" json:"extensions"`
	Filenames       []string `yaml:"filenames,omitempty" json:"filenames,omitempty"`
	ContentType     string   `yaml:"content_type" json:"content_type"`
	Parser          string   `yaml:"parser" json:"parser"`
	ScannerLanguage string   `yaml:"-" json:"-"`
	Source          string   `yaml:"-" json:"-"`

	FunctionTypes  []string `yaml:"function_types,omitempty" json:"function_types,omitempty"`
	ClassTypes     []string `yaml:"class_types,omitempty" json:"class_types,omitempty"`
	InterfaceTypes []string `yaml:"interface_types,omitempty" json:"interface_types,omitempty"`
	MethodTypes    []string `yaml:"method_types,omitempty" json:"method_types,omitempty"`
	TypeDefTypes   []string `yaml:"type_def_types,omitempty" json:"type_def_types,omitempty"`
	ConstantTypes  []string `yaml:"constant_types,omitempty" json:"constant_types,omitempty"`
	VariableTypes  []string `yaml:"variable_types,omitempty" json:"variable_types,omitempty"`
	NameField      string   `yaml:"name_field,omitempty" json:"name_field,omitempty"`
}

// Registry resolves file paths and language names against validated definitions.
type Registry struct {
	defsByName        map[string]Definition
	extToName         map[string]string
	fileToName        map[string]string
	contentTypeByName map[string]string
}

var (
	defaultRegistry     *Registry
	defaultRegistryOnce sync.Once
	defaultRegistryErr  error
)

// DefaultRegistry returns a registry with AmanMCP's built-in language contract.
func DefaultRegistry() *Registry {
	defaultRegistryOnce.Do(func() {
		defaultRegistry, defaultRegistryErr = NewRegistry(nil)
	})
	if defaultRegistryErr != nil {
		panic(defaultRegistryErr)
	}
	return defaultRegistry
}

// NewRegistry builds a validated registry from built-ins plus user definitions.
func NewRegistry(userDefs []Definition) (*Registry, error) {
	defs := BuiltinDefinitions()
	for i := range userDefs {
		def := normalizeDefinition(userDefs[i], SourceConfig)
		defs = append(defs, def)
	}
	if err := validateDefinitions(defs); err != nil {
		return nil, err
	}

	registry := &Registry{
		defsByName:        make(map[string]Definition, len(defs)),
		extToName:         make(map[string]string),
		fileToName:        make(map[string]string),
		contentTypeByName: make(map[string]string, len(defs)),
	}
	for _, def := range defs {
		registry.defsByName[def.Name] = cloneDefinition(def)
		registry.contentTypeByName[def.Name] = def.ContentType
		if def.ScannerLanguage != "" {
			if existing, ok := registry.contentTypeByName[def.ScannerLanguage]; ok && existing != def.ContentType {
				return nil, fmt.Errorf("scanner language %q maps to conflicting content types %q and %q", def.ScannerLanguage, existing, def.ContentType)
			}
			registry.contentTypeByName[def.ScannerLanguage] = def.ContentType
		}
		for _, ext := range def.Extensions {
			registry.extToName[ext] = def.Name
		}
		for _, filename := range def.Filenames {
			registry.fileToName[filename] = def.Name
		}
	}
	return registry, nil
}

// ValidateUserDefinitions validates user language config against built-ins.
func ValidateUserDefinitions(userDefs []Definition) error {
	_, err := NewRegistry(userDefs)
	return err
}

// NormalizeUserDefinitions returns normalized user definitions after validation.
func NormalizeUserDefinitions(userDefs []Definition) ([]Definition, error) {
	registry, err := NewRegistry(userDefs)
	if err != nil {
		return nil, err
	}
	out := make([]Definition, 0, len(userDefs))
	for _, userDef := range userDefs {
		def, ok := registry.GetByName(userDef.Name)
		if ok && def.Source == SourceConfig {
			out = append(out, def)
		}
	}
	return out, nil
}

// Definitions returns a deterministic copy of all registered definitions.
func (r *Registry) Definitions() []Definition {
	names := make([]string, 0, len(r.defsByName))
	for name := range r.defsByName {
		names = append(names, name)
	}
	sort.Strings(names)

	defs := make([]Definition, 0, len(names))
	for _, name := range names {
		defs = append(defs, cloneDefinition(r.defsByName[name]))
	}
	return defs
}

// GetByName returns a language definition by registered name.
func (r *Registry) GetByName(name string) (Definition, bool) {
	def, ok := r.defsByName[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return Definition{}, false
	}
	return cloneDefinition(def), true
}

// GetByExtension returns a language definition by extension.
func (r *Registry) GetByExtension(ext string) (Definition, bool) {
	name, ok := r.extToName[normalizeExtension(ext)]
	if !ok {
		return Definition{}, false
	}
	return r.GetByName(name)
}

// Detect returns the scanner-visible language for a file path.
func (r *Registry) Detect(path string) string {
	base := filepath.Base(path)
	if name, ok := r.fileToName[base]; ok {
		return scannerName(r.defsByName[name])
	}
	ext := normalizeExtension(filepath.Ext(path))
	if name, ok := r.extToName[ext]; ok {
		return scannerName(r.defsByName[name])
	}
	return ""
}

// ContentType returns the content type for a scanner-visible language.
func (r *Registry) ContentType(language string) string {
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "" {
		return ContentTypeText
	}
	if contentType, ok := r.contentTypeByName[language]; ok {
		return contentType
	}
	return ContentTypeText
}

func scannerName(def Definition) string {
	if def.ScannerLanguage != "" {
		return def.ScannerLanguage
	}
	return def.Name
}

func validateDefinitions(defs []Definition) error {
	seenNames := make(map[string]string, len(defs))
	seenExts := make(map[string]string)
	seenFiles := make(map[string]string)

	for _, def := range defs {
		if def.Name == "" {
			return fmt.Errorf("language name is required")
		}
		if previous, exists := seenNames[def.Name]; exists {
			return fmt.Errorf("duplicate language name %q from %s conflicts with %s", def.Name, def.Source, previous)
		}
		seenNames[def.Name] = def.Source

		if !isValidContentType(def.ContentType) {
			return fmt.Errorf("language %q has unknown content_type %q", def.Name, def.ContentType)
		}
		if !isKnownParser(def.Parser) {
			return fmt.Errorf("language %q references unknown parser %q", def.Name, def.Parser)
		}
		if err := validateSymbolNodeKinds(def); err != nil {
			return err
		}

		for _, ext := range def.Extensions {
			if owner, exists := seenExts[ext]; exists {
				return fmt.Errorf("extension %s for language %q conflicts with language %q", ext, def.Name, owner)
			}
			seenExts[ext] = def.Name
		}
		for _, filename := range def.Filenames {
			if owner, exists := seenFiles[filename]; exists {
				return fmt.Errorf("filename %s for language %q conflicts with language %q", filename, def.Name, owner)
			}
			seenFiles[filename] = def.Name
		}
	}
	return nil
}

func validateSymbolNodeKinds(def Definition) error {
	if def.Parser == ParserLineFallback {
		if len(allSymbolTypes(def)) > 0 {
			return fmt.Errorf("language %q uses line_fallback but declares symbol node kinds", def.Name)
		}
		return nil
	}

	allowed := knownNodeTypesByParser()[def.Parser]
	for _, nodeType := range allSymbolTypes(def) {
		if !allowed[nodeType] {
			return fmt.Errorf("language %q has unknown symbol node kind %q for parser %q", def.Name, nodeType, def.Parser)
		}
	}
	return nil
}

func allSymbolTypes(def Definition) []string {
	total := len(def.FunctionTypes) + len(def.ClassTypes) + len(def.InterfaceTypes) +
		len(def.MethodTypes) + len(def.TypeDefTypes) + len(def.ConstantTypes) + len(def.VariableTypes)
	all := make([]string, 0, total)
	all = append(all, def.FunctionTypes...)
	all = append(all, def.ClassTypes...)
	all = append(all, def.InterfaceTypes...)
	all = append(all, def.MethodTypes...)
	all = append(all, def.TypeDefTypes...)
	all = append(all, def.ConstantTypes...)
	all = append(all, def.VariableTypes...)
	return all
}

func knownNodeTypesByParser() map[string]map[string]bool {
	known := make(map[string]map[string]bool)
	for _, def := range BuiltinDefinitions() {
		if def.Parser == ParserLineFallback {
			continue
		}
		if known[def.Parser] == nil {
			known[def.Parser] = make(map[string]bool)
		}
		for _, nodeType := range allSymbolTypes(def) {
			known[def.Parser][nodeType] = true
		}
	}
	return known
}

func isKnownParser(parser string) bool {
	switch parser {
	case ParserGo, ParserTypeScript, ParserTSX, ParserJavaScript, ParserPython, ParserLineFallback:
		return true
	default:
		return false
	}
}

func isValidContentType(contentType string) bool {
	switch contentType {
	case ContentTypeCode, ContentTypeMarkdown, ContentTypePDF, ContentTypeText, ContentTypeConfig:
		return true
	default:
		return false
	}
}

func normalizeDefinition(def Definition, source string) Definition {
	def.Name = strings.ToLower(strings.TrimSpace(def.Name))
	def.ScannerLanguage = strings.ToLower(strings.TrimSpace(def.ScannerLanguage))
	def.ContentType = strings.ToLower(strings.TrimSpace(def.ContentType))
	def.Parser = strings.ToLower(strings.TrimSpace(def.Parser))
	def.Source = source
	if def.NameField == "" {
		def.NameField = "name"
	}

	def.Extensions = normalizeExtensions(def.Extensions)
	def.Filenames = normalizeFilenames(def.Filenames)
	def.FunctionTypes = normalizeNodeTypes(def.FunctionTypes)
	def.ClassTypes = normalizeNodeTypes(def.ClassTypes)
	def.InterfaceTypes = normalizeNodeTypes(def.InterfaceTypes)
	def.MethodTypes = normalizeNodeTypes(def.MethodTypes)
	def.TypeDefTypes = normalizeNodeTypes(def.TypeDefTypes)
	def.ConstantTypes = normalizeNodeTypes(def.ConstantTypes)
	def.VariableTypes = normalizeNodeTypes(def.VariableTypes)
	return def
}

func normalizeExtensions(exts []string) []string {
	out := make([]string, 0, len(exts))
	for _, ext := range exts {
		ext = normalizeExtension(ext)
		if ext != "." {
			out = append(out, ext)
		}
	}
	return out
}

func normalizeExtension(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return ext
}

func normalizeFilenames(filenames []string) []string {
	out := make([]string, 0, len(filenames))
	for _, filename := range filenames {
		filename = strings.TrimSpace(filename)
		if filename != "" {
			out = append(out, filename)
		}
	}
	return out
}

func normalizeNodeTypes(types []string) []string {
	out := make([]string, 0, len(types))
	for _, nodeType := range types {
		nodeType = strings.TrimSpace(nodeType)
		if nodeType != "" {
			out = append(out, nodeType)
		}
	}
	return out
}

func cloneDefinition(def Definition) Definition {
	def.Extensions = append([]string(nil), def.Extensions...)
	def.Filenames = append([]string(nil), def.Filenames...)
	def.FunctionTypes = append([]string(nil), def.FunctionTypes...)
	def.ClassTypes = append([]string(nil), def.ClassTypes...)
	def.InterfaceTypes = append([]string(nil), def.InterfaceTypes...)
	def.MethodTypes = append([]string(nil), def.MethodTypes...)
	def.TypeDefTypes = append([]string(nil), def.TypeDefTypes...)
	def.ConstantTypes = append([]string(nil), def.ConstantTypes...)
	def.VariableTypes = append([]string(nil), def.VariableTypes...)
	return def
}
