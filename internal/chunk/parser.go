package chunk

import (
	"context"
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"
)

// Parser wraps tree-sitter for AST parsing
type Parser struct {
	parser   *sitter.Parser
	registry *LanguageRegistry
}

// NewParser creates a new parser with default language registry
func NewParser() *Parser {
	return &Parser{
		parser:   sitter.NewParser(),
		registry: DefaultRegistry(),
	}
}

// NewParserWithRegistry creates a new parser with a custom language registry
func NewParserWithRegistry(registry *LanguageRegistry) *Parser {
	return &Parser{
		parser:   sitter.NewParser(),
		registry: registry,
	}
}

// Parse parses source code and returns the AST
func (p *Parser) Parse(ctx context.Context, source []byte, language string) (*Tree, error) {
	// Get tree-sitter language
	tsLang, ok := p.registry.GetTreeSitterLanguage(language)
	if !ok {
		return nil, fmt.Errorf("unsupported language: %s", language)
	}

	// Set language (smacker bindings don't return error)
	p.parser.SetLanguage(tsLang)

	// Parse the source (smacker bindings: Parse(oldTree, source))
	tsTree, err := p.parser.ParseCtx(ctx, nil, source)
	if err != nil {
		return nil, fmt.Errorf("failed to parse source: %w", err)
	}
	if tsTree == nil {
		return nil, fmt.Errorf("failed to parse source: nil tree")
	}

	// Convert tree-sitter tree to our tree structure
	root := convertNode(tsTree.RootNode(), source)

	return &Tree{
		Root:     root,
		Source:   source,
		Language: language,
	}, nil
}

// Close releases parser resources
func (p *Parser) Close() {
	if p.parser != nil {
		p.parser.Close()
	}
}

// convertNode converts a tree-sitter node to our Node type
func convertNode(tsNode *sitter.Node, source []byte) *Node {
	if tsNode == nil {
		return nil
	}

	node := &Node{
		Type:      tsNode.Type(),
		StartByte: tsNode.StartByte(),
		EndByte:   tsNode.EndByte(),
		StartPoint: Point{
			Row:    tsNode.StartPoint().Row,
			Column: tsNode.StartPoint().Column,
		},
		EndPoint: Point{
			Row:    tsNode.EndPoint().Row,
			Column: tsNode.EndPoint().Column,
		},
		HasError: tsNode.HasError(),
		Children: make([]*Node, 0, int(tsNode.ChildCount())),
	}

	// Convert children
	for i := uint32(0); i < tsNode.ChildCount(); i++ {
		child := tsNode.Child(int(i))
		if child != nil {
			node.Children = append(node.Children, convertNode(child, source))
		}
	}

	return node
}

// GetContent returns the source content for a node
func (n *Node) GetContent(source []byte) string {
	if n.StartByte >= n.EndByte || int(n.EndByte) > len(source) {
		return ""
	}
	return string(source[n.StartByte:n.EndByte])
}

// FindChildByType finds the first child with the given type
func (n *Node) FindChildByType(nodeType string) *Node {
	for _, child := range n.Children {
		if child.Type == nodeType {
			return child
		}
	}
	return nil
}

// FindChildrenByType finds all children with the given type (non-recursive)
func (n *Node) FindChildrenByType(nodeType string) []*Node {
	var result []*Node
	for _, child := range n.Children {
		if child.Type == nodeType {
			result = append(result, child)
		}
	}
	return result
}

// FindAllByType recursively finds all nodes with the given type
func (n *Node) FindAllByType(nodeType string) []*Node {
	var result []*Node

	if n.Type == nodeType {
		result = append(result, n)
	}

	for _, child := range n.Children {
		result = append(result, child.FindAllByType(nodeType)...)
	}

	return result
}

// Walk traverses the tree depth-first and calls fn for each node
func (n *Node) Walk(fn func(*Node) bool) {
	if !fn(n) {
		return
	}
	for _, child := range n.Children {
		child.Walk(fn)
	}
}
