package chunk

import (
	"strings"
)

// SymbolExtractor extracts symbols from parsed AST
type SymbolExtractor struct {
	registry *LanguageRegistry
}

// NewSymbolExtractor creates a new symbol extractor
func NewSymbolExtractor() *SymbolExtractor {
	return &SymbolExtractor{
		registry: DefaultRegistry(),
	}
}

// NewSymbolExtractorWithRegistry creates a new symbol extractor with custom registry
func NewSymbolExtractorWithRegistry(registry *LanguageRegistry) *SymbolExtractor {
	return &SymbolExtractor{
		registry: registry,
	}
}

// Extract extracts symbols from the parsed tree
func (e *SymbolExtractor) Extract(tree *Tree, source []byte) []*Symbol {
	// Return empty slice, not nil, for consistent API behavior (DEBT-012)
	if tree == nil || tree.Root == nil {
		return []*Symbol{}
	}

	config, ok := e.registry.GetByName(tree.Language)
	if !ok {
		return []*Symbol{}
	}

	var symbols []*Symbol

	tree.Root.Walk(func(n *Node) bool {
		symbol := e.extractSymbolFromNode(n, source, config, tree.Language)
		if symbol != nil {
			symbols = append(symbols, symbol)
		}
		return true // continue walking
	})

	return symbols
}

// extractSymbolFromNode extracts a symbol from a single node if it matches
func (e *SymbolExtractor) extractSymbolFromNode(n *Node, source []byte, config *LanguageConfig, language string) *Symbol {
	// Check if this is a symbol-defining node
	var symbolType SymbolType
	var found bool

	// Check function types
	for _, ft := range config.FunctionTypes {
		if n.Type == ft {
			symbolType = SymbolTypeFunction
			found = true
			break
		}
	}

	// Check method types
	if !found {
		for _, mt := range config.MethodTypes {
			if n.Type == mt {
				symbolType = SymbolTypeMethod
				found = true
				break
			}
		}
	}

	// Check class types
	if !found {
		for _, ct := range config.ClassTypes {
			if n.Type == ct {
				symbolType = SymbolTypeClass
				found = true
				break
			}
		}
	}

	// Check interface types
	if !found {
		for _, it := range config.InterfaceTypes {
			if n.Type == it {
				symbolType = SymbolTypeInterface
				found = true
				break
			}
		}
	}

	// Check type definition types
	if !found {
		for _, tt := range config.TypeDefTypes {
			if n.Type == tt {
				symbolType = SymbolTypeType
				found = true
				break
			}
		}
	}

	// Check constant types
	if !found {
		for _, ct := range config.ConstantTypes {
			if n.Type == ct {
				symbolType = SymbolTypeConstant
				found = true
				break
			}
		}
	}

	// Check variable types
	if !found {
		for _, vt := range config.VariableTypes {
			if n.Type == vt {
				symbolType = SymbolTypeVariable
				found = true
				break
			}
		}
	}

	if !found {
		// Check for arrow functions and variable declarations with functions
		symbol := e.extractSpecialSymbol(n, source, language)
		if symbol != nil {
			return symbol
		}
		return nil
	}

	// Extract name
	name := e.extractName(n, source, config, language)
	if name == "" {
		return nil
	}

	// Extract doc comment (look at previous sibling)
	docComment := e.extractDocComment(n, source, language)

	// Extract signature (first line of the declaration)
	signature := e.extractSignature(n, source, symbolType, language)

	return &Symbol{
		Name:       name,
		Type:       symbolType,
		StartLine:  int(n.StartPoint.Row) + 1, // Convert to 1-indexed
		EndLine:    int(n.EndPoint.Row) + 1,
		Signature:  signature,
		DocComment: docComment,
	}
}

// extractName extracts the name of a symbol from a node
func (e *SymbolExtractor) extractName(n *Node, source []byte, config *LanguageConfig, language string) string {
	// Look for identifier child
	switch language {
	case "go":
		return e.extractGoName(n, source)
	case "typescript", "tsx":
		return e.extractTypeScriptName(n, source)
	case "javascript", "jsx":
		return e.extractJavaScriptName(n, source)
	case "python":
		return e.extractPythonName(n, source)
	default:
		// Generic fallback: look for first identifier
		for _, child := range n.Children {
			if child.Type == "identifier" {
				return child.GetContent(source)
			}
		}
	}
	return ""
}

func (e *SymbolExtractor) extractGoName(n *Node, source []byte) string {
	switch n.Type {
	case "function_declaration":
		// Function name is in identifier child
		for _, child := range n.Children {
			if child.Type == "identifier" {
				return child.GetContent(source)
			}
		}
	case "method_declaration":
		// Method name is in field_identifier child (not identifier)
		for _, child := range n.Children {
			if child.Type == "field_identifier" {
				return child.GetContent(source)
			}
		}
	case "type_declaration":
		// Look for type_spec
		for _, child := range n.Children {
			if child.Type == "type_spec" {
				for _, grandchild := range child.Children {
					if grandchild.Type == "type_identifier" {
						return grandchild.GetContent(source)
					}
				}
			}
		}
	case "const_declaration":
		// Go const can be: const Name = value OR const ( Name1 = value1; Name2 = value2 )
		// Look for const_spec children, extract first identifier
		for _, child := range n.Children {
			if child.Type == "const_spec" {
				for _, grandchild := range child.Children {
					if grandchild.Type == "identifier" {
						return grandchild.GetContent(source)
					}
				}
			}
		}
	case "var_declaration":
		// Go var can be: var Name Type = value OR var ( Name1 Type1; Name2 Type2 )
		// Look for var_spec children, extract first identifier
		for _, child := range n.Children {
			if child.Type == "var_spec" {
				for _, grandchild := range child.Children {
					if grandchild.Type == "identifier" {
						return grandchild.GetContent(source)
					}
				}
			}
		}
	}
	return ""
}

func (e *SymbolExtractor) extractTypeScriptName(n *Node, source []byte) string {
	// Handle lexical_declaration (const/let) and variable_declaration (var)
	if n.Type == "lexical_declaration" || n.Type == "variable_declaration" {
		// Name is nested inside variable_declarator
		for _, child := range n.Children {
			if child.Type == "variable_declarator" {
				for _, grandchild := range child.Children {
					if grandchild.Type == "identifier" {
						return grandchild.GetContent(source)
					}
				}
			}
		}
	}

	// Look for identifier or type_identifier
	for _, child := range n.Children {
		if child.Type == "identifier" || child.Type == "type_identifier" {
			return child.GetContent(source)
		}
	}
	return ""
}

func (e *SymbolExtractor) extractJavaScriptName(n *Node, source []byte) string {
	// Handle lexical_declaration (const/let) and variable_declaration (var)
	if n.Type == "lexical_declaration" || n.Type == "variable_declaration" {
		// Name is nested inside variable_declarator
		for _, child := range n.Children {
			if child.Type == "variable_declarator" {
				for _, grandchild := range child.Children {
					if grandchild.Type == "identifier" {
						return grandchild.GetContent(source)
					}
				}
			}
		}
	}

	// Look for identifier
	for _, child := range n.Children {
		if child.Type == "identifier" {
			return child.GetContent(source)
		}
	}
	return ""
}

func (e *SymbolExtractor) extractPythonName(n *Node, source []byte) string {
	// Look for identifier
	for _, child := range n.Children {
		if child.Type == "identifier" {
			return child.GetContent(source)
		}
	}
	return ""
}

// extractSpecialSymbol handles special cases like arrow functions and const functions
func (e *SymbolExtractor) extractSpecialSymbol(n *Node, source []byte, language string) *Symbol {
	switch language {
	case "typescript", "tsx", "javascript", "jsx":
		// Handle const arrow = () => {} and const func = function() {}
		if n.Type == "lexical_declaration" || n.Type == "variable_declaration" {
			return e.extractJSVariableFunctionSymbol(n, source)
		}
	}
	return nil
}

// extractJSVariableFunctionSymbol extracts symbols from JS/TS variable declarations
// that contain arrow functions or function expressions
func (e *SymbolExtractor) extractJSVariableFunctionSymbol(n *Node, source []byte) *Symbol {
	// Find variable_declarator children
	for _, child := range n.Children {
		if child.Type == "variable_declarator" {
			var name string
			var hasFunction bool

			for _, grandchild := range child.Children {
				if grandchild.Type == "identifier" {
					name = grandchild.GetContent(source)
				}
				if grandchild.Type == "arrow_function" || grandchild.Type == "function" || grandchild.Type == "function_expression" {
					hasFunction = true
				}
			}

			if name != "" && hasFunction {
				// Extract signature for arrow/const functions
				content := n.GetContent(source)
				signature := e.extractFunctionSignature(content, "javascript")

				return &Symbol{
					Name:      name,
					Type:      SymbolTypeFunction,
					StartLine: int(n.StartPoint.Row) + 1,
					EndLine:   int(n.EndPoint.Row) + 1,
					Signature: signature,
				}
			}
		}
	}
	return nil
}

// extractDocComment extracts the doc comment for a symbol
func (e *SymbolExtractor) extractDocComment(n *Node, source []byte, language string) string {
	// This is a simplified implementation - in a full implementation,
	// we would need to look at the parent node's children to find
	// previous siblings (comments) before this node.
	// For now, we'll return empty string as doc comments require
	// more complex tree traversal.

	// Look at the preceding lines for comments
	// This is handled differently per language
	if n.StartPoint.Row == 0 {
		return ""
	}

	// Find the start of the current line
	lineStart := int(n.StartByte)
	for lineStart > 0 && source[lineStart-1] != '\n' {
		lineStart--
	}

	// Look for comment on previous line
	if lineStart <= 1 {
		return ""
	}

	// Find previous line
	prevLineEnd := lineStart - 1
	prevLineStart := prevLineEnd - 1
	for prevLineStart > 0 && source[prevLineStart-1] != '\n' {
		prevLineStart--
	}

	prevLine := strings.TrimSpace(string(source[prevLineStart:prevLineEnd]))

	switch language {
	case "go":
		if strings.HasPrefix(prevLine, "//") {
			return strings.TrimPrefix(prevLine, "//")
		}
	case "python":
		// Python uses docstrings inside the function/class, not before
		return ""
	case "javascript", "jsx", "typescript", "tsx":
		if strings.HasPrefix(prevLine, "//") {
			return strings.TrimPrefix(prevLine, "//")
		}
	}

	return ""
}

// extractSignature extracts the signature (first line) of a function/method/class declaration.
// This helps embedding models understand the symbol's interface without reading the full body.
func (e *SymbolExtractor) extractSignature(n *Node, source []byte, symbolType SymbolType, language string) string {
	// Get the full content of the node
	content := n.GetContent(source)
	if content == "" {
		return ""
	}

	// For functions/methods, extract up to the opening brace or colon (Python)
	switch symbolType {
	case SymbolTypeFunction, SymbolTypeMethod:
		return e.extractFunctionSignature(content, language)
	case SymbolTypeClass, SymbolTypeInterface, SymbolTypeType:
		return e.extractTypeSignature(content, language)
	}

	return ""
}

// extractFunctionSignature extracts the signature line from a function/method
func (e *SymbolExtractor) extractFunctionSignature(content, language string) string {
	lines := strings.SplitN(content, "\n", 2)
	if len(lines) == 0 {
		return ""
	}

	firstLine := strings.TrimSpace(lines[0])

	switch language {
	case "go":
		// Go: func (r *Receiver) Name(params) ReturnType {
		// Extract up to and including the opening brace
		if idx := strings.Index(firstLine, "{"); idx != -1 {
			return strings.TrimSpace(firstLine[:idx])
		}
		return firstLine

	case "python":
		// Python: def name(params):
		// Keep the full first line including colon
		return firstLine

	case "typescript", "tsx", "javascript", "jsx":
		// JS/TS: function name(params) { or async function name(params): ReturnType {
		// Also: const name = (params) => { or const name = function(params) {
		if idx := strings.Index(firstLine, "{"); idx != -1 {
			return strings.TrimSpace(firstLine[:idx])
		}
		// For arrow functions without braces
		if strings.Contains(firstLine, "=>") && !strings.Contains(firstLine, "{") {
			return firstLine
		}
		return firstLine
	}

	return firstLine
}

// extractTypeSignature extracts the signature from a type/class/interface definition
func (e *SymbolExtractor) extractTypeSignature(content, language string) string {
	lines := strings.SplitN(content, "\n", 2)
	if len(lines) == 0 {
		return ""
	}

	firstLine := strings.TrimSpace(lines[0])

	switch language {
	case "go":
		// Go: type Name struct { or type Name interface {
		if idx := strings.Index(firstLine, "{"); idx != -1 {
			return strings.TrimSpace(firstLine[:idx])
		}
		// For type aliases: type Name = OtherType
		return firstLine

	case "python":
		// Python: class Name(Parent):
		return firstLine

	case "typescript", "tsx":
		// TS: interface Name { or class Name extends Parent {
		if idx := strings.Index(firstLine, "{"); idx != -1 {
			return strings.TrimSpace(firstLine[:idx])
		}
		return firstLine

	case "javascript", "jsx":
		// JS: class Name extends Parent {
		if idx := strings.Index(firstLine, "{"); idx != -1 {
			return strings.TrimSpace(firstLine[:idx])
		}
		return firstLine
	}

	return firstLine
}
