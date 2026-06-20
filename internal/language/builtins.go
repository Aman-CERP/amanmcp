package language

// BuiltinDefinitions returns AmanMCP's built-in language definitions.
func BuiltinDefinitions() []Definition {
	defs := make([]Definition, len(builtinDefinitions))
	for i, def := range builtinDefinitions {
		defs[i] = cloneDefinition(def)
	}
	return defs
}

func baseBuiltinDefinitions() []Definition {
	return []Definition{
		normalizeDefinition(Definition{
			Name:        "go",
			Extensions:  []string{".go"},
			ContentType: ContentTypeCode,
			Parser:      ParserGo,
			FunctionTypes: []string{
				"function_declaration",
			},
			MethodTypes: []string{
				"method_declaration",
			},
			TypeDefTypes: []string{
				"type_declaration",
			},
			ConstantTypes: []string{
				"const_declaration",
			},
			VariableTypes: []string{
				"var_declaration",
			},
			NameField: "name",
		}, SourceBuiltin),
		normalizeDefinition(Definition{
			Name:        "typescript",
			Extensions:  []string{".ts"},
			ContentType: ContentTypeCode,
			Parser:      ParserTypeScript,
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
				"lexical_declaration",
			},
			VariableTypes: []string{
				"variable_declaration",
			},
			NameField: "name",
		}, SourceBuiltin),
		normalizeDefinition(Definition{
			Name:            "tsx",
			ScannerLanguage: "typescript",
			Extensions:      []string{".tsx"},
			ContentType:     ContentTypeCode,
			Parser:          ParserTSX,
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
				"lexical_declaration",
			},
			VariableTypes: []string{
				"variable_declaration",
			},
			NameField: "name",
		}, SourceBuiltin),
		normalizeDefinition(Definition{
			Name:        "javascript",
			Extensions:  []string{".js", ".mjs"},
			ContentType: ContentTypeCode,
			Parser:      ParserJavaScript,
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
			ConstantTypes: []string{
				"lexical_declaration",
			},
			VariableTypes: []string{
				"variable_declaration",
			},
			NameField: "name",
		}, SourceBuiltin),
		normalizeDefinition(Definition{
			Name:            "jsx",
			ScannerLanguage: "javascript",
			Extensions:      []string{".jsx"},
			ContentType:     ContentTypeCode,
			Parser:          ParserJavaScript,
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
			ConstantTypes: []string{
				"lexical_declaration",
			},
			VariableTypes: []string{
				"variable_declaration",
			},
			NameField: "name",
		}, SourceBuiltin),
		normalizeDefinition(Definition{
			Name:        "python",
			Extensions:  []string{".py", ".pyw", ".pyi"},
			ContentType: ContentTypeCode,
			Parser:      ParserPython,
			FunctionTypes: []string{
				"function_definition",
			},
			ClassTypes: []string{
				"class_definition",
			},
			VariableTypes: []string{
				"assignment",
			},
			NameField: "name",
		}, SourceBuiltin),
		normalizeDefinition(Definition{
			Name:        "markdown",
			Extensions:  []string{".md", ".markdown", ".mdx"},
			ContentType: ContentTypeMarkdown,
			Parser:      ParserLineFallback,
		}, SourceBuiltin),
		normalizeDefinition(Definition{
			Name:        "rst",
			Extensions:  []string{".rst"},
			ContentType: ContentTypeMarkdown,
			Parser:      ParserLineFallback,
		}, SourceBuiltin),
		normalizeDefinition(Definition{
			Name:        "pdf",
			Extensions:  []string{".pdf"},
			ContentType: ContentTypePDF,
			Parser:      ParserLineFallback,
		}, SourceBuiltin),
		normalizeDefinition(Definition{
			Name:        "text",
			Extensions:  []string{".txt"},
			ContentType: ContentTypeText,
			Parser:      ParserLineFallback,
		}, SourceBuiltin),
		normalizeDefinition(Definition{
			Name:        "json",
			Extensions:  []string{".json"},
			ContentType: ContentTypeConfig,
			Parser:      ParserLineFallback,
		}, SourceBuiltin),
		normalizeDefinition(Definition{
			Name:        "yaml",
			Extensions:  []string{".yaml", ".yml"},
			ContentType: ContentTypeConfig,
			Parser:      ParserLineFallback,
		}, SourceBuiltin),
		normalizeDefinition(Definition{
			Name:        "toml",
			Extensions:  []string{".toml"},
			ContentType: ContentTypeConfig,
			Parser:      ParserLineFallback,
		}, SourceBuiltin),
		normalizeDefinition(Definition{
			Name:        "xml",
			Extensions:  []string{".xml"},
			ContentType: ContentTypeConfig,
			Parser:      ParserLineFallback,
		}, SourceBuiltin),
		normalizeDefinition(Definition{
			Name:        "ini",
			Extensions:  []string{".ini"},
			ContentType: ContentTypeConfig,
			Parser:      ParserLineFallback,
		}, SourceBuiltin),
		normalizeDefinition(Definition{
			Name:        "config",
			Extensions:  []string{".conf"},
			ContentType: ContentTypeConfig,
			Parser:      ParserLineFallback,
		}, SourceBuiltin),
		normalizeDefinition(Definition{
			Name:        "properties",
			Extensions:  []string{".properties"},
			ContentType: ContentTypeConfig,
			Parser:      ParserLineFallback,
		}, SourceBuiltin),
		normalizeDefinition(Definition{
			Name:        "dockerfile",
			Filenames:   []string{"Dockerfile"},
			ContentType: ContentTypeConfig,
			Parser:      ParserLineFallback,
		}, SourceBuiltin),
		normalizeDefinition(Definition{
			Name:        "makefile",
			Filenames:   []string{"Makefile", "makefile", "GNUmakefile"},
			ContentType: ContentTypeConfig,
			Parser:      ParserLineFallback,
		}, SourceBuiltin),
	}
}

// BuiltinLineFallbackCodeDefinitions returns detected code languages without
// compiled parser support. They intentionally flow through line fallback.
func BuiltinLineFallbackCodeDefinitions() []Definition {
	namesAndExts := []Definition{
		{Name: "html", Extensions: []string{".html", ".htm"}},
		{Name: "css", Extensions: []string{".css"}},
		{Name: "scss", Extensions: []string{".scss"}},
		{Name: "sass", Extensions: []string{".sass"}},
		{Name: "less", Extensions: []string{".less"}},
		{Name: "shell", Extensions: []string{".sh", ".bash", ".zsh"}},
		{Name: "fish", Extensions: []string{".fish"}},
		{Name: "ruby", Extensions: []string{".rb", ".rake"}},
		{Name: "erb", Extensions: []string{".erb"}},
		{Name: "rust", Extensions: []string{".rs"}},
		{Name: "java", Extensions: []string{".java"}},
		{Name: "kotlin", Extensions: []string{".kt", ".kts"}},
		{Name: "c", Extensions: []string{".c", ".h"}},
		{Name: "cpp", Extensions: []string{".cpp", ".hpp", ".cc", ".cxx"}},
		{Name: "csharp", Extensions: []string{".cs"}},
		{Name: "swift", Extensions: []string{".swift"}},
		{Name: "php", Extensions: []string{".php"}},
		{Name: "scala", Extensions: []string{".scala"}},
		{Name: "elixir", Extensions: []string{".ex", ".exs"}},
		{Name: "erlang", Extensions: []string{".erl"}},
		{Name: "haskell", Extensions: []string{".hs"}},
		{Name: "lua", Extensions: []string{".lua"}},
		{Name: "r", Extensions: []string{".r"}},
		{Name: "sql", Extensions: []string{".sql"}},
		{Name: "vue", Extensions: []string{".vue"}},
		{Name: "svelte", Extensions: []string{".svelte"}},
		{Name: "graphql", Extensions: []string{".graphql", ".gql"}},
		{Name: "protobuf", Extensions: []string{".proto"}},
	}

	defs := make([]Definition, 0, len(namesAndExts))
	for _, def := range namesAndExts {
		def.ContentType = ContentTypeCode
		def.Parser = ParserLineFallback
		defs = append(defs, normalizeDefinition(def, SourceBuiltin))
	}
	return defs
}

func init() {
	// Keep scanner fallback languages in the same table without making the main
	// built-in list harder to scan.
	builtinDefinitions = append(baseBuiltinDefinitions(), BuiltinLineFallbackCodeDefinitions()...)
}

var builtinDefinitions []Definition
