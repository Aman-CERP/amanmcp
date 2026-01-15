package chunk

import (
	"strings"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// LanguageRegistry manages supported languages and their configurations
type LanguageRegistry struct {
	mu          sync.RWMutex
	configs     map[string]*LanguageConfig // keyed by language name
	extToLang   map[string]string          // extension -> language name
	tsLanguages map[string]*sitter.Language
}

// NewLanguageRegistry creates a new registry with default language configurations
func NewLanguageRegistry() *LanguageRegistry {
	r := &LanguageRegistry{
		configs:     make(map[string]*LanguageConfig),
		extToLang:   make(map[string]string),
		tsLanguages: make(map[string]*sitter.Language),
	}

	// Register default languages
	r.registerGo()
	r.registerTypeScript()
	r.registerJavaScript()
	r.registerPython()

	return r
}

// GetByExtension returns the language configuration for a file extension
func (r *LanguageRegistry) GetByExtension(ext string) (*LanguageConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Normalize extension
	ext = strings.ToLower(ext)
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}

	langName, ok := r.extToLang[ext]
	if !ok {
		return nil, false
	}

	config, ok := r.configs[langName]
	return config, ok
}

// GetByName returns the language configuration by name
func (r *LanguageRegistry) GetByName(name string) (*LanguageConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	config, ok := r.configs[name]
	return config, ok
}

// GetTreeSitterLanguage returns the tree-sitter language for a language name
func (r *LanguageRegistry) GetTreeSitterLanguage(name string) (*sitter.Language, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lang, ok := r.tsLanguages[name]
	return lang, ok
}

// SupportedExtensions returns all supported file extensions
func (r *LanguageRegistry) SupportedExtensions() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	exts := make([]string, 0, len(r.extToLang))
	for ext := range r.extToLang {
		exts = append(exts, ext)
	}
	return exts
}

// registerLanguage adds a language to the registry
func (r *LanguageRegistry) registerLanguage(config *LanguageConfig, tsLang *sitter.Language) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.configs[config.Name] = config
	r.tsLanguages[config.Name] = tsLang

	for _, ext := range config.Extensions {
		r.extToLang[ext] = config.Name
	}
}

func (r *LanguageRegistry) registerGo() {
	config := &LanguageConfig{
		Name:       "go",
		Extensions: []string{".go"},
		FunctionTypes: []string{
			"function_declaration",
		},
		MethodTypes: []string{
			"method_declaration",
		},
		ClassTypes: []string{}, // Go doesn't have classes
		TypeDefTypes: []string{
			"type_declaration",
		},
		InterfaceTypes: []string{}, // Go interfaces are type declarations
		ConstantTypes: []string{
			"const_declaration",
		},
		VariableTypes: []string{
			"var_declaration",
		},
		NameField: "name",
	}

	r.registerLanguage(config, golang.GetLanguage())
}

func (r *LanguageRegistry) registerTypeScript() {
	// TypeScript
	tsConfig := &LanguageConfig{
		Name:       "typescript",
		Extensions: []string{".ts"},
		FunctionTypes: []string{
			"function_declaration",
		},
		MethodTypes: []string{
			"method_definition",
		},
		ClassTypes: []string{
			"class_declaration",
		},
		InterfaceTypes: []string{
			"interface_declaration",
		},
		TypeDefTypes: []string{
			"type_alias_declaration",
		},
		ConstantTypes: []string{
			"lexical_declaration", // const and let
		},
		VariableTypes: []string{
			"variable_declaration", // var
		},
		NameField: "name",
	}
	r.registerLanguage(tsConfig, typescript.GetLanguage())

	// TSX
	tsxConfig := &LanguageConfig{
		Name:           "tsx",
		Extensions:     []string{".tsx"},
		FunctionTypes:  tsConfig.FunctionTypes,
		MethodTypes:    tsConfig.MethodTypes,
		ClassTypes:     tsConfig.ClassTypes,
		InterfaceTypes: tsConfig.InterfaceTypes,
		TypeDefTypes:   tsConfig.TypeDefTypes,
		ConstantTypes:  tsConfig.ConstantTypes,
		VariableTypes:  tsConfig.VariableTypes,
		NameField:      tsConfig.NameField,
	}
	r.registerLanguage(tsxConfig, tsx.GetLanguage())
}

func (r *LanguageRegistry) registerJavaScript() {
	// JavaScript
	jsConfig := &LanguageConfig{
		Name:       "javascript",
		Extensions: []string{".js", ".mjs"},
		FunctionTypes: []string{
			"function_declaration",
			"function",
		},
		MethodTypes: []string{
			"method_definition",
		},
		ClassTypes: []string{
			"class_declaration",
		},
		InterfaceTypes: []string{}, // JS doesn't have interfaces
		TypeDefTypes:   []string{},
		ConstantTypes: []string{
			"lexical_declaration", // const and let
		},
		VariableTypes: []string{
			"variable_declaration", // var
		},
		NameField: "name",
	}
	r.registerLanguage(jsConfig, javascript.GetLanguage())

	// JSX (uses same parser as JS)
	jsxConfig := &LanguageConfig{
		Name:           "jsx",
		Extensions:     []string{".jsx"},
		FunctionTypes:  jsConfig.FunctionTypes,
		MethodTypes:    jsConfig.MethodTypes,
		ClassTypes:     jsConfig.ClassTypes,
		InterfaceTypes: jsConfig.InterfaceTypes,
		TypeDefTypes:   jsConfig.TypeDefTypes,
		ConstantTypes:  jsConfig.ConstantTypes,
		VariableTypes:  jsConfig.VariableTypes,
		NameField:      jsConfig.NameField,
	}
	r.registerLanguage(jsxConfig, javascript.GetLanguage())
}

func (r *LanguageRegistry) registerPython() {
	config := &LanguageConfig{
		Name:       "python",
		Extensions: []string{".py"},
		FunctionTypes: []string{
			"function_definition",
		},
		MethodTypes: []string{}, // In Python, methods are function_definition inside class
		ClassTypes: []string{
			"class_definition",
		},
		InterfaceTypes: []string{}, // Python doesn't have interfaces
		TypeDefTypes:   []string{},
		ConstantTypes:  []string{}, // Python doesn't have const keyword
		VariableTypes: []string{
			"assignment", // Top-level assignments (module-level variables)
		},
		NameField: "name",
	}
	r.registerLanguage(config, python.GetLanguage())
}

// defaultRegistry is the global language registry
var defaultRegistry = NewLanguageRegistry()

// DefaultRegistry returns the global language registry
func DefaultRegistry() *LanguageRegistry {
	return defaultRegistry
}
