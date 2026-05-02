package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Aman-CERP/amanmcp/internal/pmmutation"
)

type PMMutateInput struct {
	Operation string `json:"operation" jsonschema:"mutation operation: acquire_tokens, capture_learning, add_changelog_fragment, file_item, plan_status_move, move_item, resolve_item, defer_item, open_adr, cut_release_preflight, or cut_release_confirm"`

	Paths      []string               `json:"paths,omitempty" jsonschema:"project-relative paths for acquire_tokens"`
	LockTokens []pmmutation.FileToken `json:"lock_tokens,omitempty" jsonschema:"file-scoped optimistic lock tokens from acquire_tokens"`

	What    string `json:"what,omitempty"`
	Context string `json:"context,omitempty"`
	Action  string `json:"action,omitempty"`

	Section string   `json:"section,omitempty"`
	Summary string   `json:"summary,omitempty"`
	Details []string `json:"details,omitempty"`

	ID                 string   `json:"id,omitempty"`
	ItemType           string   `json:"item_type,omitempty"`
	Status             string   `json:"status,omitempty"`
	Priority           string   `json:"priority,omitempty"`
	Parent             string   `json:"parent,omitempty"`
	Title              string   `json:"title,omitempty"`
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
	Deliverables       []string `json:"deliverables,omitempty"`
	Validation         []string `json:"validation,omitempty"`

	SourcePath string `json:"source_path,omitempty"`
	FromStatus string `json:"from_status,omitempty"`
	ToStatus   string `json:"to_status,omitempty"`

	ADRNumber int    `json:"adr_number,omitempty"`
	Decision  string `json:"decision,omitempty"`

	Version         string                 `json:"version,omitempty"`
	EvidencePaths   []string               `json:"evidence_paths,omitempty"`
	Confirmed       bool                   `json:"confirmed,omitempty"`
	PreflightTokens []pmmutation.FileToken `json:"preflight_tokens,omitempty"`
}

type PMMutateOutput struct {
	Operation  string                     `json:"operation"`
	LockTokens []pmmutation.FileToken     `json:"lock_tokens,omitempty"`
	MovePlan   *pmmutation.StatusMovePlan `json:"move_plan,omitempty"`
	Receipt    *pmmutation.Receipt        `json:"receipt,omitempty"`
}

func (s *Server) handlePMMutateArgs(ctx context.Context, args map[string]any) (PMMutateOutput, error) {
	input := PMMutateInput{
		Operation:          stringArg(args, "operation"),
		Paths:              stringSliceArg(args, "paths"),
		LockTokens:         fileTokenSliceArg(args, "lock_tokens"),
		What:               stringArg(args, "what"),
		Context:            stringArg(args, "context"),
		Action:             stringArg(args, "action"),
		Section:            stringArg(args, "section"),
		Summary:            stringArg(args, "summary"),
		Details:            stringSliceArg(args, "details"),
		ID:                 stringArg(args, "id"),
		ItemType:           stringArg(args, "item_type"),
		Status:             stringArg(args, "status"),
		Priority:           stringArg(args, "priority"),
		Parent:             stringArg(args, "parent"),
		Title:              stringArg(args, "title"),
		AcceptanceCriteria: stringSliceArg(args, "acceptance_criteria"),
		Deliverables:       stringSliceArg(args, "deliverables"),
		Validation:         stringSliceArg(args, "validation"),
		SourcePath:         stringArg(args, "source_path"),
		FromStatus:         stringArg(args, "from_status"),
		ToStatus:           stringArg(args, "to_status"),
		ADRNumber:          intArg(args, "adr_number"),
		Decision:           stringArg(args, "decision"),
		Version:            stringArg(args, "version"),
		EvidencePaths:      stringSliceArg(args, "evidence_paths"),
		Confirmed:          boolArg(args, "confirmed"),
		PreflightTokens:    fileTokenSliceArg(args, "preflight_tokens"),
	}
	return s.handlePMMutateTool(ctx, input)
}

func (s *Server) handlePMMutateTool(ctx context.Context, input PMMutateInput) (PMMutateOutput, error) {
	s.mu.RLock()
	mutator := s.pmMutator
	s.mu.RUnlock()
	if mutator == nil {
		return PMMutateOutput{}, NewInvalidParamsError("pm.mutate is unavailable because no project root is configured")
	}

	output := PMMutateOutput{Operation: input.Operation}
	switch input.Operation {
	case "acquire_tokens":
		tokens, err := mutator.AcquireTokens(ctx, input.Paths...)
		if err != nil {
			return PMMutateOutput{}, NewInvalidParamsError(err.Error())
		}
		output.LockTokens = tokens
		return output, nil
	case "capture_learning":
		receipt, err := mutator.CaptureLearning(ctx, pmmutation.CaptureLearningRequest{
			What:       input.What,
			Context:    input.Context,
			Action:     input.Action,
			LockTokens: input.LockTokens,
		})
		output.Receipt = &receipt
		return output, mapPMMutationError(err)
	case "add_changelog_fragment":
		receipt, err := mutator.AddChangelogFragment(ctx, pmmutation.AddChangelogFragmentRequest{
			Section:    input.Section,
			Summary:    input.Summary,
			Details:    input.Details,
			LockTokens: input.LockTokens,
		})
		output.Receipt = &receipt
		return output, mapPMMutationError(err)
	case "file_item":
		receipt, err := mutator.CreateItem(ctx, pmmutation.CreateItemRequest{
			ID:                 input.ID,
			Type:               input.ItemType,
			Status:             input.Status,
			Priority:           input.Priority,
			Parent:             input.Parent,
			Title:              input.Title,
			Context:            input.Context,
			AcceptanceCriteria: input.AcceptanceCriteria,
			Deliverables:       input.Deliverables,
			Validation:         input.Validation,
			LockTokens:         input.LockTokens,
		})
		output.Receipt = &receipt
		return output, mapPMMutationError(err)
	case "plan_status_move":
		plan, err := mutator.PlanStatusMove(ctx, statusMoveRequest(input))
		if err != nil {
			return PMMutateOutput{}, NewInvalidParamsError(err.Error())
		}
		output.MovePlan = &plan
		return output, nil
	case "move_item":
		receipt, err := mutator.MoveItem(ctx, statusMoveRequest(input))
		output.Receipt = &receipt
		return output, mapPMMutationError(err)
	case "resolve_item":
		receipt, err := mutator.ResolveItem(ctx, statusMoveRequest(input))
		output.Receipt = &receipt
		return output, mapPMMutationError(err)
	case "defer_item":
		receipt, err := mutator.DeferItem(ctx, statusMoveRequest(input))
		output.Receipt = &receipt
		return output, mapPMMutationError(err)
	case "open_adr":
		receipt, err := mutator.CreateADRSkeleton(ctx, pmmutation.CreateADRRequest{
			Number:     input.ADRNumber,
			Title:      input.Title,
			Context:    input.Context,
			Decision:   input.Decision,
			LockTokens: input.LockTokens,
		})
		output.Receipt = &receipt
		return output, mapPMMutationError(err)
	case "cut_release_preflight":
		receipt, err := mutator.PreflightRelease(ctx, pmmutation.ReleasePreflightRequest{
			Version:       input.Version,
			EvidencePaths: input.EvidencePaths,
		})
		output.Receipt = &receipt
		return output, mapPMMutationError(err)
	case "cut_release_confirm":
		receipt, err := mutator.ConfirmRelease(ctx, pmmutation.ReleaseConfirmationRequest{
			Version:         input.Version,
			Confirmed:       input.Confirmed,
			PreflightTokens: input.PreflightTokens,
		})
		output.Receipt = &receipt
		return output, mapPMMutationError(err)
	default:
		return PMMutateOutput{}, NewInvalidParamsError(fmt.Sprintf("unsupported pm.mutate operation %q", input.Operation))
	}
}

func (s *Server) mcpPMMutateHandler(ctx context.Context, _ *mcp.CallToolRequest, input PMMutateInput) (
	*mcp.CallToolResult,
	PMMutateOutput,
	error,
) {
	output, err := s.handlePMMutateTool(ctx, input)
	if err != nil {
		return nil, PMMutateOutput{}, err
	}
	return nil, output, nil
}

func statusMoveRequest(input PMMutateInput) pmmutation.StatusMoveRequest {
	return pmmutation.StatusMoveRequest{
		Type:       input.ItemType,
		SourcePath: input.SourcePath,
		FromStatus: input.FromStatus,
		ToStatus:   input.ToStatus,
		LockTokens: input.LockTokens,
	}
}

func mapPMMutationError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, pmmutation.ErrConflict):
		return &MCPError{
			Code:    ErrCodePMMutationConflict,
			Message: "PM mutation conflict: refresh lock tokens with acquire_tokens and retry. " + err.Error(),
		}
	case errors.Is(err, pmmutation.ErrConfirmationRequired):
		return &MCPError{
			Code:    ErrCodePMMutationConfirmationRequired,
			Message: "PM mutation requires human confirmation in the current request. " + err.Error(),
		}
	case errors.Is(err, pmmutation.ErrNotFound):
		return &MCPError{
			Code:    ErrCodeFileNotFound,
			Message: "PM mutation target is missing. " + err.Error(),
		}
	case errors.Is(err, pmmutation.ErrInvalidInput):
		return NewInvalidParamsError("PM mutation invalid input. " + err.Error())
	}
	return NewInvalidParamsError(err.Error())
}

func boolArg(args map[string]any, key string) bool {
	if args == nil {
		return false
	}
	value, ok := args[key].(bool)
	return ok && value
}

func stringSliceArg(args map[string]any, key string) []string {
	if args == nil {
		return nil
	}
	switch value := args[key].(type) {
	case []string:
		return append([]string(nil), value...)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

func fileTokenSliceArg(args map[string]any, key string) []pmmutation.FileToken {
	if args == nil {
		return nil
	}
	switch value := args[key].(type) {
	case []pmmutation.FileToken:
		return append([]pmmutation.FileToken(nil), value...)
	case []any:
		tokens := make([]pmmutation.FileToken, 0, len(value))
		for _, item := range value {
			tokenMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			tokens = append(tokens, pmmutation.FileToken{
				Path:        stringFromMap(tokenMap, "path"),
				Exists:      boolFromMap(tokenMap, "exists"),
				Size:        int64FromMap(tokenMap, "size"),
				ModTime:     stringFromMap(tokenMap, "mtime"),
				ContentHash: stringFromMap(tokenMap, "content_hash"),
				Token:       stringFromMap(tokenMap, "token"),
			})
		}
		return tokens
	default:
		return nil
	}
}

func stringFromMap(values map[string]any, key string) string {
	value, ok := values[key].(string)
	if !ok {
		return ""
	}
	return value
}

func boolFromMap(values map[string]any, key string) bool {
	value, ok := values[key].(bool)
	return ok && value
}

func int64FromMap(values map[string]any, key string) int64 {
	switch value := values[key].(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		return int64(value)
	default:
		return 0
	}
}
