package chunk

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Aman-CERP/amanmcp/internal/language"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// LanguageRegistry manages supported code languages and their configurations.
type LanguageRegistry struct {
	mu          sync.RWMutex
	configs     map[string]*LanguageConfig
	extToLang   map[string]string
	tsLanguages map[string]*sitter.Language
}

// NewLanguageRegistry creates a registry with built-in language configurations.
func NewLanguageRegistry() *LanguageRegistry {
	registry, err := NewLanguageRegistryFromDefinitions(nil)
	if err != nil {
		panic(err)
	}
	return registry
}

// NewLanguageRegistryFromDefinitions creates a test-isolated registry from
// built-ins plus user definitions.
func NewLanguageRegistryFromDefinitions(userDefs []language.Definition) (*LanguageRegistry, error) {
	langRegistry, err := language.NewRegistry(userDefs)
	if err != nil {
		return nil, err
	}

	r := &LanguageRegistry{
		configs:     make(map[string]*LanguageConfig),
		extToLang:   make(map[string]string),
		tsLanguages: compiledTreeSitterLanguages(),
	}
	for _, def := range langRegistry.Definitions() {
		if def.ContentType != language.ContentTypeCode {
			continue
		}
		config := languageConfigFromDefinition(def)
		r.registerLanguage(config)
	}
	return r, nil
}

// GetByExtension returns the language configuration for a file extension.
func (r *LanguageRegistry) GetByExtension(ext string) (*LanguageConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	langName, ok := r.extToLang[normalizeChunkExtension(ext)]
	if !ok {
		return nil, false
	}
	config, ok := r.configs[langName]
	return config, ok
}

// GetByName returns the language configuration by name.
func (r *LanguageRegistry) GetByName(name string) (*LanguageConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	config, ok := r.configs[strings.ToLower(strings.TrimSpace(name))]
	return config, ok
}

// ResolveForFile returns the parser config for a file path and scanner language.
func (r *LanguageRegistry) ResolveForFile(path, scannerLanguage string) (*LanguageConfig, bool) {
	ext := filepath.Ext(path)
	if cfg, ok := r.GetByExtension(ext); ok {
		if scannerLanguage == "" || cfg.Name == scannerLanguage || cfg.ScannerLanguage == scannerLanguage {
			return cfg, true
		}
	}
	return r.GetByName(scannerLanguage)
}

// GetTreeSitterLanguage returns the tree-sitter language for a language name.
func (r *LanguageRegistry) GetTreeSitterLanguage(name string) (*sitter.Language, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	config, ok := r.configs[strings.ToLower(strings.TrimSpace(name))]
	if ok {
		lang, langOK := r.tsLanguages[config.Parser]
		return lang, langOK
	}
	lang, langOK := r.tsLanguages[strings.ToLower(strings.TrimSpace(name))]
	return lang, langOK
}

// SupportedExtensions returns all supported code file extensions.
func (r *LanguageRegistry) SupportedExtensions() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	exts := make([]string, 0, len(r.extToLang))
	for ext := range r.extToLang {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	return exts
}

func (r *LanguageRegistry) registerLanguage(config *LanguageConfig) {
	r.configs[config.Name] = config
	for _, ext := range config.Extensions {
		r.extToLang[normalizeChunkExtension(ext)] = config.Name
	}
}

func languageConfigFromDefinition(def language.Definition) *LanguageConfig {
	return &LanguageConfig{
		Name:            def.Name,
		ScannerLanguage: def.ScannerLanguage,
		Extensions:      append([]string(nil), def.Extensions...),
		FunctionTypes:   append([]string(nil), def.FunctionTypes...),
		ClassTypes:      append([]string(nil), def.ClassTypes...),
		InterfaceTypes:  append([]string(nil), def.InterfaceTypes...),
		MethodTypes:     append([]string(nil), def.MethodTypes...),
		TypeDefTypes:    append([]string(nil), def.TypeDefTypes...),
		ConstantTypes:   append([]string(nil), def.ConstantTypes...),
		VariableTypes:   append([]string(nil), def.VariableTypes...),
		NameField:       def.NameField,
		Parser:          def.Parser,
		LineFallback:    def.Parser == language.ParserLineFallback,
		ConfigSource:    def.Source,
	}
}

func compiledTreeSitterLanguages() map[string]*sitter.Language {
	return map[string]*sitter.Language{
		language.ParserGo:         golang.GetLanguage(),
		language.ParserTypeScript: typescript.GetLanguage(),
		language.ParserTSX:        tsx.GetLanguage(),
		language.ParserJavaScript: javascript.GetLanguage(),
		language.ParserPython:     python.GetLanguage(),
	}
}

func normalizeChunkExtension(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return ext
}

// DefaultRegistry returns a fresh built-in registry.
func DefaultRegistry() *LanguageRegistry {
	return NewLanguageRegistry()
}
