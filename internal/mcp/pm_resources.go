package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Aman-CERP/amanmcp/internal/pmresource"
)

// RegisterPMResources registers read-only AmanPM inspection resources.
func (s *Server) RegisterPMResources() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.rootPath == "" {
		return fmt.Errorf("rootPath must be set before registering PM resources")
	}

	reader := pmresource.NewReader(s.rootPath)
	deprecationMeta := amanpmDeprecationMeta()
	for _, definition := range pmresource.ResourceDefinitions() {
		s.mcp.AddResource(
			&mcp.Resource{
				Name:        definition.Name,
				URI:         definition.URI,
				Description: "DEPRECATED — moving to scripts/amanpm/ per DEBT-031 (AmanPM resources do not belong in the AmanMCP product binary). " + definition.Description,
				MIMEType:    definition.MIMEType,
				Meta:        deprecationMeta,
			},
			makePMResourceHandler(reader, definition.URI),
		)
	}
	return nil
}

func makePMResourceHandler(reader *pmresource.Reader, expectedURI string) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		if req == nil || req.Params == nil {
			return nil, NewInvalidParamsError("read resource request is missing parameters")
		}
		if req.Params.URI != expectedURI {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}

		envelope, err := reader.Read(ctx, req.Params.URI)
		if err != nil {
			if errors.Is(err, pmresource.ErrUnknownResource) {
				return nil, mcp.ResourceNotFoundError(req.Params.URI)
			}
			return nil, MapError(err)
		}

		content, err := json.MarshalIndent(envelope, "", "  ")
		if err != nil {
			return nil, MapError(err)
		}

		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{
					URI:      req.Params.URI,
					MIMEType: "application/json",
					Text:     string(content),
				},
			},
		}, nil
	}
}
