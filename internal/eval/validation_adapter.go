package eval

import (
	"context"
	"fmt"

	"github.com/Aman-CERP/amanmcp/internal/validation"
)

type ValidationSearcher struct {
	validator *validation.Validator
}

func NewValidationSearcher(ctx context.Context, projectRoot string) (*ValidationSearcher, error) {
	validator, err := validation.NewValidator(ctx, projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize search eval validator: %w", err)
	}
	return &ValidationSearcher{validator: validator}, nil
}

func (s *ValidationSearcher) Close() error {
	if s.validator == nil {
		return nil
	}
	return s.validator.Close()
}

func (s *ValidationSearcher) Search(ctx context.Context, query Query) (SearchResponse, error) {
	result, err := s.validator.RunStructuredQuery(ctx, validation.QuerySpec{
		ID:       query.ID,
		Name:     query.Name,
		Query:    query.Query,
		Tool:     query.Tool,
		Profile:  query.Profile,
		Scope:    append([]string(nil), query.Scope...),
		Mode:     query.Mode,
		Expected: expectedPaths(query.ExpectedResults),
		Notes:    query.Notes,
		Tier:     validationTier(query),
	})
	if err != nil {
		if query.Class == "negative_adversarial" {
			return SearchResponse{}, nil
		}
		return SearchResponse{}, fmt.Errorf("structured search query failed: %w", err)
	}
	results := make([]SearchResult, 0, len(result.Results))
	for _, item := range result.Results {
		results = append(results, SearchResult{
			Path:        item.FilePath,
			Symbol:      item.Symbol,
			Text:        item.Content,
			ResultID:    item.ResultID,
			ContentType: item.ContentType,
			PageNumber:  item.PageNumber,
			PageStart:   item.PageStart,
			PageEnd:     item.PageEnd,
		})
	}
	return SearchResponse{Results: results, ResponseBytes: result.ResponseBytes}, nil
}

func validationTier(query Query) int {
	if query.Class == "negative_adversarial" || query.Tier == "negative" {
		return 0
	}
	if query.Tier == "2" {
		return 2
	}
	return 1
}

func expectedPaths(expected []ExpectedResult) []string {
	paths := make([]string, 0, len(expected))
	for _, result := range expected {
		paths = append(paths, result.Path)
	}
	return paths
}
