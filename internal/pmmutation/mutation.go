// Package pmmutation contains the core AmanPM source-file mutation primitives.
package pmmutation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Aman-CERP/amanmcp/internal/secrets"
	"github.com/gofrs/flock"
	"gopkg.in/yaml.v3"
)

const (
	SchemaVersion = "pm-mutation.v1"

	LearningPath  = ".aman-pm/knowledge/learnings.md"
	ChangelogPath = ".aman-pm/changelog/unreleased.md"

	authoredByLine = "Authored-By: Niraj Kumar <nirajkvinit@gmail.com>"
)

var (
	ErrConflict             = errors.New("pm mutation conflict")
	ErrInvalidInput         = errors.New("pm mutation invalid input")
	ErrNotFound             = errors.New("pm mutation target missing")
	ErrConfirmationRequired = errors.New("pm mutation confirmation required")
)

type ValidationStatus string

const (
	ValidationOK          ValidationStatus = "ok"
	ValidationConflict    ValidationStatus = "conflict"
	ValidationInvalid     ValidationStatus = "invalid"
	ValidationUnavailable ValidationStatus = "unavailable"
	ValidationBlocked     ValidationStatus = "blocked"
)

type Mutator struct {
	root  string
	clock func() time.Time
}

type Option func(*Mutator)

func WithClock(clock func() time.Time) Option {
	return func(m *Mutator) {
		if clock != nil {
			m.clock = clock
		}
	}
}

func New(root string, opts ...Option) *Mutator {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = root
	}
	m := &Mutator{
		root:  absRoot,
		clock: time.Now,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

type FileToken struct {
	Path        string `json:"path"`
	Exists      bool   `json:"exists"`
	Size        int64  `json:"size"`
	ModTime     string `json:"mtime"`
	ContentHash string `json:"content_hash"`
	Token       string `json:"token"`
}

type Diagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Path     string `json:"path,omitempty"`
}

type ChangedFile struct {
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	Token     FileToken `json:"token"`
	Operation string    `json:"operation"`
}

type ValidationResult struct {
	Status          ValidationStatus `json:"status"`
	CheckedAt       string           `json:"checked_at"`
	PreWriteTokens  []FileToken      `json:"pre_write_tokens,omitempty"`
	PostWriteTokens []FileToken      `json:"post_write_tokens,omitempty"`
}

type Receipt struct {
	SchemaVersion          string           `json:"schema_version"`
	Operation              string           `json:"operation"`
	ChangedFiles           []ChangedFile    `json:"changed_files"`
	GeneratedIDs           []string         `json:"generated_ids"`
	Validation             ValidationResult `json:"validation"`
	Diagnostics            []Diagnostic     `json:"diagnostics"`
	SuggestedCommitMessage string           `json:"suggested_commit_message"`
	Release                *ReleasePlan     `json:"release,omitempty"`
}

type Preview struct {
	TargetPath  string       `json:"target_path"`
	LockTokens  []FileToken  `json:"lock_tokens"`
	GeneratedID string       `json:"generated_id,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

type CaptureLearningRequest struct {
	What       string
	Context    string
	Action     string
	LockTokens []FileToken
}

type AddChangelogFragmentRequest struct {
	Section    string
	Summary    string
	Details    []string
	LockTokens []FileToken
}

type CreateItemRequest struct {
	ID                 string
	Type               string
	Status             string
	Priority           string
	Parent             string
	Title              string
	Context            string
	AcceptanceCriteria []string
	Deliverables       []string
	Validation         []string
	CreatedBy          string
	Created            time.Time
	LockTokens         []FileToken
}

type CreateADRRequest struct {
	Number     int
	Title      string
	Context    string
	Decision   string
	Date       time.Time
	LockTokens []FileToken
}

type StatusMoveRequest struct {
	Type       string
	SourcePath string
	FromStatus string
	ToStatus   string
	LockTokens []FileToken
}

type StatusMovePlan struct {
	SourcePath string `json:"source_path"`
	TargetPath string `json:"target_path"`
	FromStatus string `json:"from_status"`
	ToStatus   string `json:"to_status"`
}

type ReleasePreflightRequest struct {
	Version       string
	EvidencePaths []string
}

type ReleaseConfirmationRequest struct {
	Version         string
	Confirmed       bool
	PreflightTokens []FileToken
}

type ReleasePlan struct {
	Version          string   `json:"version"`
	Performed        bool     `json:"performed"`
	Confirmed        bool     `json:"confirmed"`
	WouldTag         bool     `json:"would_tag"`
	WouldPush        bool     `json:"would_push"`
	WouldPublish     bool     `json:"would_publish"`
	EvidencePaths    []string `json:"evidence_paths"`
	Blockers         []string `json:"blockers,omitempty"`
	SuggestedActions []string `json:"suggested_actions,omitempty"`
}

func (m *Mutator) AcquireTokens(ctx context.Context, paths ...string) ([]FileToken, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tokens := make([]FileToken, 0, len(paths))
	for _, path := range paths {
		token, err := m.readToken(path)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	return tokens, nil
}

func (m *Mutator) CaptureLearning(ctx context.Context, req CaptureLearningRequest) (Receipt, error) {
	receipt := m.receipt("pm.capture_learning", "chore(pm): capture learning")
	if err := ctx.Err(); err != nil {
		return receipt, err
	}
	if err := validateRequiredText(map[string]string{
		"what":    req.What,
		"context": req.Context,
		"action":  req.Action,
	}); err != nil {
		return m.invalid(receipt, err.Error()), ErrInvalidInput
	}
	if containsHighConfidenceSecret(req.What, req.Context, req.Action) {
		return m.invalidWithCode(receipt, "SECRET_LIKE_CONTENT", LearningPath, "learning content looks like a secret"), ErrInvalidInput
	}
	unlock, err := m.lockMutation(&receipt)
	if err != nil {
		return receipt, err
	}
	defer unlock()
	if err := m.validateExistingToken(req.LockTokens, LearningPath, &receipt); err != nil {
		return receipt, err
	}

	fullPath, err := m.absolutePath(LearningPath)
	if err != nil {
		return m.invalid(receipt, err.Error()), ErrInvalidInput
	}
	current, err := os.ReadFile(fullPath)
	if err != nil {
		return m.unavailable(receipt, LearningPath, "learning file is missing"), ErrNotFound
	}

	block := fmt.Sprintf("- **Learning**: %s / **Context**: %s / **Action**: %s\n",
		strings.TrimSpace(req.What),
		strings.TrimSpace(req.Context),
		strings.TrimSpace(req.Action),
	)
	next := ensureTrailingNewline(string(current)) + block
	if err := writeFileAtomic(fullPath, []byte(next), 0o644); err != nil {
		return m.invalidWithCode(receipt, "WRITE_FAILED", LearningPath, err.Error()), err
	}

	return m.writeSuccess(receipt, LearningPath, "append")
}

func (m *Mutator) AddChangelogFragment(ctx context.Context, req AddChangelogFragmentRequest) (Receipt, error) {
	receipt := m.receipt("pm.add_changelog_fragment", "chore(pm): update unreleased changelog")
	if err := ctx.Err(); err != nil {
		return receipt, err
	}
	if !allowedChangelogSections[req.Section] {
		return m.invalidWithCode(receipt, "UNKNOWN_CHANGELOG_SECTION", ChangelogPath, "unknown changelog section"), ErrInvalidInput
	}
	if err := validateRequiredText(map[string]string{"summary": req.Summary}); err != nil {
		return m.invalid(receipt, err.Error()), ErrInvalidInput
	}
	secretInputs := append([]string{req.Summary}, req.Details...)
	if containsHighConfidenceSecret(secretInputs...) {
		return m.invalidWithCode(receipt, "SECRET_LIKE_CONTENT", ChangelogPath, "changelog content looks like a secret"), ErrInvalidInput
	}
	unlock, err := m.lockMutation(&receipt)
	if err != nil {
		return receipt, err
	}
	defer unlock()
	if err := m.validateExistingToken(req.LockTokens, ChangelogPath, &receipt); err != nil {
		return receipt, err
	}

	fullPath, err := m.absolutePath(ChangelogPath)
	if err != nil {
		return m.invalid(receipt, err.Error()), ErrInvalidInput
	}
	currentBytes, err := os.ReadFile(fullPath)
	if err != nil {
		return m.unavailable(receipt, ChangelogPath, "unreleased changelog is missing"), ErrNotFound
	}
	next, err := insertChangelogFragment(string(currentBytes), req.Section, req.Summary, req.Details)
	if err != nil {
		return m.invalidWithCode(receipt, "CHANGELOG_SECTION_MISSING", ChangelogPath, err.Error()), ErrInvalidInput
	}
	if err := writeFileAtomic(fullPath, []byte(next), 0o644); err != nil {
		return m.invalidWithCode(receipt, "WRITE_FAILED", ChangelogPath, err.Error()), err
	}

	return m.writeSuccess(receipt, ChangelogPath, "append")
}

func (m *Mutator) PreviewCreateItem(ctx context.Context, req CreateItemRequest) (Preview, error) {
	if err := ctx.Err(); err != nil {
		return Preview{}, err
	}
	path, err := itemPath(req)
	if err != nil {
		return Preview{}, err
	}
	tokens, err := m.AcquireTokens(ctx, path)
	if err != nil {
		return Preview{}, err
	}
	return Preview{TargetPath: path, LockTokens: tokens, GeneratedID: req.ID}, nil
}

func (m *Mutator) CreateItem(ctx context.Context, req CreateItemRequest) (Receipt, error) {
	receipt := m.receipt("pm.file_item", "chore(pm): create PM item")
	if err := ctx.Err(); err != nil {
		return receipt, err
	}
	if err := m.validateCreateItem(req); err != nil {
		return m.invalid(receipt, err.Error()), ErrInvalidInput
	}
	path, err := itemPath(req)
	if err != nil {
		return m.invalid(receipt, err.Error()), ErrInvalidInput
	}
	unlock, err := m.lockMutation(&receipt)
	if err != nil {
		return receipt, err
	}
	defer unlock()
	if existingPath, err := m.findItemByID(req.Type, req.ID); err != nil {
		return m.invalidWithCode(receipt, "DUPLICATE_ID_CHECK_FAILED", "", err.Error()), err
	} else if existingPath != "" && existingPath != path {
		return m.invalidWithCode(receipt, "DUPLICATE_ITEM_ID", existingPath, "item id already exists at "+existingPath), ErrInvalidInput
	}
	if err := m.validateCreateToken(req.LockTokens, path, &receipt); err != nil {
		return receipt, err
	}
	content := renderItem(req, m.dateOrNow(req.Created))
	fullPath, err := m.absolutePath(path)
	if err != nil {
		return m.invalid(receipt, err.Error()), ErrInvalidInput
	}
	if err := writeFileAtomic(fullPath, []byte(content), 0o644); err != nil {
		return m.invalidWithCode(receipt, "WRITE_FAILED", path, err.Error()), err
	}
	receipt.GeneratedIDs = []string{req.ID}
	return m.writeSuccess(receipt, path, "create")
}

func (m *Mutator) PreviewCreateADR(ctx context.Context, req CreateADRRequest) (Preview, error) {
	if err := ctx.Err(); err != nil {
		return Preview{}, err
	}
	path, id, err := adrPath(req)
	if err != nil {
		return Preview{}, err
	}
	tokens, err := m.AcquireTokens(ctx, path)
	if err != nil {
		return Preview{}, err
	}
	return Preview{TargetPath: path, LockTokens: tokens, GeneratedID: id}, nil
}

func (m *Mutator) CreateADRSkeleton(ctx context.Context, req CreateADRRequest) (Receipt, error) {
	receipt := m.receipt("pm.open_adr", "docs(adr): open ADR skeleton")
	if err := ctx.Err(); err != nil {
		return receipt, err
	}
	if err := validateADR(req); err != nil {
		return m.invalid(receipt, err.Error()), ErrInvalidInput
	}
	path, id, err := adrPath(req)
	if err != nil {
		return m.invalid(receipt, err.Error()), ErrInvalidInput
	}
	unlock, err := m.lockMutation(&receipt)
	if err != nil {
		return receipt, err
	}
	defer unlock()
	if err := m.validateCreateToken(req.LockTokens, path, &receipt); err != nil {
		return receipt, err
	}
	content := renderADR(req, m.dateOrNow(req.Date))
	fullPath, err := m.absolutePath(path)
	if err != nil {
		return m.invalid(receipt, err.Error()), ErrInvalidInput
	}
	if err := writeFileAtomic(fullPath, []byte(content), 0o644); err != nil {
		return m.invalidWithCode(receipt, "WRITE_FAILED", path, err.Error()), err
	}
	receipt.GeneratedIDs = []string{id}
	return m.writeSuccess(receipt, path, "create")
}

func (m *Mutator) PlanStatusMove(ctx context.Context, req StatusMoveRequest) (StatusMovePlan, error) {
	if err := ctx.Err(); err != nil {
		return StatusMovePlan{}, err
	}
	if req.Type == "" || req.SourcePath == "" || req.FromStatus == "" || req.ToStatus == "" {
		return StatusMovePlan{}, fmt.Errorf("%w: type, source_path, from_status, and to_status are required", ErrInvalidInput)
	}
	if !transitionAllowed(req.FromStatus, req.ToStatus) {
		return StatusMovePlan{}, fmt.Errorf("%w: illegal status transition %s to %s", ErrInvalidInput, req.FromStatus, req.ToStatus)
	}
	if _, err := m.absolutePath(req.SourcePath); err != nil {
		return StatusMovePlan{}, fmt.Errorf("%w: %s", ErrInvalidInput, err)
	}
	targetFolder, ok := statusFolder(req.ToStatus)
	if !ok {
		return StatusMovePlan{}, fmt.Errorf("%w: unknown target status %s", ErrInvalidInput, req.ToStatus)
	}
	rel, err := cleanRel(req.SourcePath)
	if err != nil {
		return StatusMovePlan{}, fmt.Errorf("%w: %s", ErrInvalidInput, err)
	}
	parts := strings.Split(rel, "/")
	if len(parts) < 5 {
		return StatusMovePlan{}, fmt.Errorf("%w: source path is not an AmanPM backlog item", ErrInvalidInput)
	}
	parts[len(parts)-2] = targetFolder
	return StatusMovePlan{
		SourcePath: rel,
		TargetPath: strings.Join(parts, "/"),
		FromStatus: req.FromStatus,
		ToStatus:   req.ToStatus,
	}, nil
}

func (m *Mutator) MoveItem(ctx context.Context, req StatusMoveRequest) (Receipt, error) {
	receipt := m.receipt("pm.move_item", "chore(pm): move PM item")
	if err := ctx.Err(); err != nil {
		return receipt, err
	}
	plan, err := m.PlanStatusMove(ctx, req)
	if err != nil {
		return m.invalid(receipt, err.Error()), err
	}
	unlock, err := m.lockMutation(&receipt)
	if err != nil {
		return receipt, err
	}
	defer unlock()
	if err := m.validateExistingToken(req.LockTokens, plan.SourcePath, &receipt); err != nil {
		return receipt, err
	}
	if plan.SourcePath == plan.TargetPath {
		return m.moveItemInPlace(plan, receipt)
	}
	targetToken, ok, err := tokenForPath(req.LockTokens, plan.TargetPath)
	if err != nil {
		return m.invalid(receipt, err.Error()), ErrInvalidInput
	}
	if !ok {
		return m.invalidWithCode(receipt, "LOCK_TOKEN_REQUIRED", plan.TargetPath, "destination lock token is required"), ErrInvalidInput
	}
	currentTarget, err := m.readToken(plan.TargetPath)
	if err != nil {
		return m.invalidWithCode(receipt, "TOKEN_READ_FAILED", plan.TargetPath, err.Error()), err
	}
	if targetToken.Token != currentTarget.Token {
		receipt.Validation.Status = ValidationConflict
		receipt.Diagnostics = append(receipt.Diagnostics, Diagnostic{
			Severity: "error",
			Code:     "STALE_LOCK_TOKEN",
			Message:  "destination path changed after preview",
			Path:     plan.TargetPath,
		})
		return receipt, ErrConflict
	}
	if currentTarget.Exists {
		return m.invalidWithCode(receipt, "TARGET_ALREADY_EXISTS", plan.TargetPath, "destination already exists"), ErrInvalidInput
	}

	sourcePath, err := m.absolutePath(plan.SourcePath)
	if err != nil {
		return m.invalid(receipt, err.Error()), ErrInvalidInput
	}
	targetPath, err := m.absolutePath(plan.TargetPath)
	if err != nil {
		return m.invalid(receipt, err.Error()), ErrInvalidInput
	}
	sourceContent, err := os.ReadFile(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return m.unavailable(receipt, plan.SourcePath, "source item is missing"), ErrNotFound
		}
		return m.invalidWithCode(receipt, "READ_FAILED", plan.SourcePath, err.Error()), err
	}
	updatedContent, err := replaceFrontmatterStatus(string(sourceContent), plan.FromStatus, plan.ToStatus)
	if err != nil {
		return m.invalidWithCode(receipt, "STATUS_UPDATE_FAILED", plan.SourcePath, err.Error()), ErrInvalidInput
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return m.invalidWithCode(receipt, "WRITE_FAILED", plan.TargetPath, err.Error()), err
	}
	if err := writeFileAtomic(targetPath, []byte(updatedContent), 0o644); err != nil {
		return m.invalidWithCode(receipt, "WRITE_FAILED", plan.TargetPath, err.Error()), err
	}
	if err := os.Remove(sourcePath); err != nil {
		_ = os.Remove(targetPath)
		return m.invalidWithCode(receipt, "SOURCE_REMOVE_FAILED", plan.SourcePath, err.Error()), err
	}

	sourceAfter, err := m.readToken(plan.SourcePath)
	if err != nil {
		return m.invalidWithCode(receipt, "POST_WRITE_VALIDATION_FAILED", plan.SourcePath, err.Error()), err
	}
	targetAfter, err := m.readToken(plan.TargetPath)
	if err != nil {
		return m.invalidWithCode(receipt, "POST_WRITE_VALIDATION_FAILED", plan.TargetPath, err.Error()), err
	}
	receipt.Validation.Status = ValidationOK
	receipt.Validation.PostWriteTokens = []FileToken{sourceAfter, targetAfter}
	receipt.ChangedFiles = []ChangedFile{
		{
			Path:      plan.SourcePath,
			Size:      sourceAfter.Size,
			Token:     sourceAfter,
			Operation: "delete",
		},
		{
			Path:      plan.TargetPath,
			Size:      targetAfter.Size,
			Token:     targetAfter,
			Operation: "create",
		},
	}
	return receipt, nil
}

func (m *Mutator) moveItemInPlace(plan StatusMovePlan, receipt Receipt) (Receipt, error) {
	sourcePath, err := m.absolutePath(plan.SourcePath)
	if err != nil {
		return m.invalid(receipt, err.Error()), ErrInvalidInput
	}
	sourceContent, err := os.ReadFile(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return m.unavailable(receipt, plan.SourcePath, "source item is missing"), ErrNotFound
		}
		return m.invalidWithCode(receipt, "READ_FAILED", plan.SourcePath, err.Error()), err
	}
	updatedContent, err := replaceFrontmatterStatus(string(sourceContent), plan.FromStatus, plan.ToStatus)
	if err != nil {
		return m.invalidWithCode(receipt, "STATUS_UPDATE_FAILED", plan.SourcePath, err.Error()), ErrInvalidInput
	}
	if err := writeFileAtomic(sourcePath, []byte(updatedContent), 0o644); err != nil {
		return m.invalidWithCode(receipt, "WRITE_FAILED", plan.SourcePath, err.Error()), err
	}
	return m.writeSuccess(receipt, plan.SourcePath, "modify")
}

func (m *Mutator) ResolveItem(ctx context.Context, req StatusMoveRequest) (Receipt, error) {
	req.ToStatus = "done"
	return m.MoveItem(ctx, req)
}

func (m *Mutator) DeferItem(ctx context.Context, req StatusMoveRequest) (Receipt, error) {
	req.ToStatus = "deferred"
	return m.MoveItem(ctx, req)
}

func (m *Mutator) PreflightRelease(ctx context.Context, req ReleasePreflightRequest) (Receipt, error) {
	receipt := m.releaseReceipt("pm.cut_release.preflight", req.Version)
	if err := ctx.Err(); err != nil {
		return receipt, err
	}
	version := normalizeReleaseVersion(req.Version)
	if version == "" {
		return m.invalid(receipt, "version is required"), ErrInvalidInput
	}
	receipt.Release.Version = version
	paths := req.EvidencePaths
	if len(paths) == 0 {
		paths = defaultReleaseEvidencePaths(version)
	}
	tokens, err := m.AcquireTokens(ctx, paths...)
	if err != nil {
		return receipt, err
	}
	receipt.Validation.PreWriteTokens = tokens
	receipt.Release.EvidencePaths = tokenPaths(tokens)
	for _, token := range tokens {
		if !token.Exists {
			receipt = m.releaseBlocker(receipt, "RELEASE_EVIDENCE_MISSING", token.Path, "missing evidence: "+token.Path)
		}
	}
	receipt = m.checkReleaseDivergence(receipt, version)
	if receipt.Validation.Status == "" {
		receipt.Validation.Status = ValidationOK
	}
	receipt.Release.SuggestedActions = []string{
		"run make ci-check and attach evidence",
		"verify changelog, tag, version, and public mirror state with source-cited evidence",
	}
	return receipt, nil
}

func (m *Mutator) checkReleaseDivergence(receipt Receipt, version string) Receipt {
	indexPath := ".aman-pm/index.yaml"
	indexVersion, indexContent, err := m.readIndexVersion(indexPath)
	if err != nil {
		receipt = m.releaseBlocker(receipt, "RELEASE_INDEX_UNREADABLE", indexPath, "index version unreadable: "+err.Error())
	} else if indexVersion != "" && indexVersion != version {
		receipt = m.releaseBlocker(receipt, "RELEASE_INDEX_VERSION_MISMATCH", indexPath,
			fmt.Sprintf("index version mismatch: index=%s requested=%s", indexVersion, version))
	}

	versionFile, err := m.readTrimmedFile("VERSION")
	if err != nil {
		receipt = m.releaseBlocker(receipt, "RELEASE_VERSION_FILE_UNREADABLE", "VERSION", "VERSION unreadable: "+err.Error())
	} else if versionFile != "" && normalizeReleaseVersion(versionFile) != version {
		receipt = m.releaseBlocker(receipt, "RELEASE_VERSION_FILE_MISMATCH", "VERSION",
			fmt.Sprintf("VERSION mismatch: file=%s requested=%s", normalizeReleaseVersion(versionFile), version))
	}

	tags, err := localGitTags(m.root)
	if err != nil {
		receipt = m.releaseBlocker(receipt, "RELEASE_TAGS_UNAVAILABLE", "", "local tags unavailable: "+err.Error())
	} else {
		latestTag := latestReleaseTag(tags)
		if indexVersion != "" && latestTag != "" && normalizeReleaseVersion(latestTag) != indexVersion {
			receipt = m.releaseBlocker(receipt, "RELEASE_LOCAL_TAG_DRIFT", "",
				fmt.Sprintf("local tag drift: latest=%s index=%s", latestTag, indexVersion))
		}
		requestedTag := releaseTag(version)
		if !hasGitTag(tags, requestedTag) {
			if latestTag == "" {
				latestTag = "none"
			}
			receipt = m.releaseBlocker(receipt, "RELEASE_TAG_MISSING", "",
				fmt.Sprintf("local tag missing: requested=%s latest=%s", requestedTag, latestTag))
		}
	}

	changelogPath := versionedChangelogPath(version)
	changelog, err := m.readTrimmedFile(changelogPath)
	if err == nil && changelog != "" && !strings.Contains(changelog, version) && !strings.Contains(changelog, releaseTag(version)) {
		receipt = m.releaseBlocker(receipt, "RELEASE_CHANGELOG_VERSION_MISMATCH", changelogPath,
			fmt.Sprintf("changelog evidence does not mention %s", releaseTag(version)))
	}

	if containsKnownReleaseGap(indexContent, version) {
		receipt = m.releaseBlocker(receipt, "KNOWN_RELEASE_GAP", indexPath,
			fmt.Sprintf("known release gap remains unresolved: %s public mirror verification", releaseTag(version)))
	}
	return receipt
}

func (m *Mutator) releaseBlocker(receipt Receipt, code, path, message string) Receipt {
	receipt.Validation.Status = ValidationBlocked
	receipt.Release.Blockers = appendUnique(receipt.Release.Blockers, message)
	receipt.Diagnostics = append(receipt.Diagnostics, Diagnostic{
		Severity: "error",
		Code:     code,
		Message:  message,
		Path:     path,
	})
	return receipt
}

func (m *Mutator) ConfirmRelease(ctx context.Context, req ReleaseConfirmationRequest) (Receipt, error) {
	receipt := m.releaseReceipt("pm.cut_release.confirm", req.Version)
	if err := ctx.Err(); err != nil {
		return receipt, err
	}
	receipt.Validation.PreWriteTokens = cloneTokens(req.PreflightTokens)
	receipt.Release.Confirmed = req.Confirmed
	receipt.Release.EvidencePaths = tokenPaths(req.PreflightTokens)
	if strings.TrimSpace(req.Version) == "" {
		return m.invalid(receipt, "version is required"), ErrInvalidInput
	}
	if !req.Confirmed {
		receipt.Validation.Status = ValidationBlocked
		receipt.Release.Blockers = append(receipt.Release.Blockers, "human confirmation is required")
		receipt.Diagnostics = append(receipt.Diagnostics, Diagnostic{
			Severity: "error",
			Code:     "HUMAN_CONFIRMATION_REQUIRED",
			Message:  "release confirmation was not supplied in the current request",
		})
		return receipt, ErrConfirmationRequired
	}
	if len(req.PreflightTokens) == 0 {
		return m.invalidWithCode(receipt, "PREFLIGHT_TOKENS_REQUIRED", "", "preflight tokens are required"), ErrInvalidInput
	}
	currentTokens, err := m.AcquireTokens(ctx, tokenPaths(req.PreflightTokens)...)
	if err != nil {
		return receipt, err
	}
	receipt.Validation.PostWriteTokens = currentTokens
	if stale := staleTokens(req.PreflightTokens, currentTokens); len(stale) > 0 {
		receipt.Validation.Status = ValidationConflict
		for _, path := range stale {
			receipt.Diagnostics = append(receipt.Diagnostics, Diagnostic{
				Severity: "error",
				Code:     "PREFLIGHT_TOKEN_DRIFT",
				Message:  "release preflight evidence changed after confirmation preview",
				Path:     path,
			})
		}
		return receipt, ErrConflict
	}
	receipt.Validation.Status = ValidationBlocked
	receipt.Release.Blockers = append(receipt.Release.Blockers, "release actions are not performed by core mutation helpers")
	receipt.Diagnostics = append(receipt.Diagnostics, Diagnostic{
		Severity: "info",
		Code:     "NO_RELEASE_PERFORMED",
		Message:  "preflight was confirmed, but tag/push/publish operations remain manual",
	})
	return receipt, nil
}

func (m *Mutator) receipt(operation, commitSubject string) Receipt {
	return Receipt{
		SchemaVersion: SchemaVersion,
		Operation:     operation,
		Validation: ValidationResult{
			CheckedAt: m.clock().UTC().Format(time.RFC3339),
		},
		SuggestedCommitMessage: commitSubject + "\n\n" + authoredByLine,
	}
}

func (m *Mutator) releaseReceipt(operation, version string) Receipt {
	receipt := m.receipt(operation, "chore(release): prepare v"+strings.TrimPrefix(version, "v"))
	receipt.Release = &ReleasePlan{
		Version:      version,
		Performed:    false,
		WouldTag:     false,
		WouldPush:    false,
		WouldPublish: false,
	}
	return receipt
}

func (m *Mutator) invalid(receipt Receipt, message string) Receipt {
	return m.invalidWithCode(receipt, "INVALID_INPUT", "", message)
}

func (m *Mutator) invalidWithCode(receipt Receipt, code, path, message string) Receipt {
	receipt.Validation.Status = ValidationInvalid
	receipt.Diagnostics = append(receipt.Diagnostics, Diagnostic{
		Severity: "error",
		Code:     code,
		Message:  message,
		Path:     path,
	})
	return receipt
}

func (m *Mutator) unavailable(receipt Receipt, path, message string) Receipt {
	receipt.Validation.Status = ValidationUnavailable
	receipt.Diagnostics = append(receipt.Diagnostics, Diagnostic{
		Severity: "error",
		Code:     "TARGET_MISSING",
		Message:  message,
		Path:     path,
	})
	return receipt
}

func (m *Mutator) writeSuccess(receipt Receipt, path, operation string) (Receipt, error) {
	token, err := m.readToken(path)
	if err != nil {
		return m.invalidWithCode(receipt, "POST_WRITE_VALIDATION_FAILED", path, err.Error()), err
	}
	receipt.Validation.Status = ValidationOK
	receipt.Validation.PostWriteTokens = []FileToken{token}
	receipt.ChangedFiles = []ChangedFile{{
		Path:      path,
		Size:      token.Size,
		Token:     token,
		Operation: operation,
	}}
	return receipt, nil
}

func (m *Mutator) lockMutation(receipt *Receipt) (func(), error) {
	lockPath := filepath.Join(os.TempDir(), "amanmcp-pm-mutation-"+sha256Hex([]byte(m.root))+".lock")
	lock := flock.New(lockPath)
	if err := lock.Lock(); err != nil {
		*receipt = m.invalidWithCode(*receipt, "MUTATION_LOCK_FAILED", "", err.Error())
		return nil, fmt.Errorf("acquire PM mutation lock: %w", err)
	}
	return func() {
		_ = lock.Unlock()
	}, nil
}

func (m *Mutator) validateExistingToken(tokens []FileToken, path string, receipt *Receipt) error {
	expected, ok, err := tokenForPath(tokens, path)
	if err != nil {
		*receipt = m.invalid(*receipt, err.Error())
		return ErrInvalidInput
	}
	if !ok {
		*receipt = m.invalidWithCode(*receipt, "LOCK_TOKEN_REQUIRED", path, "lock token is required")
		return ErrInvalidInput
	}
	receipt.Validation.PreWriteTokens = []FileToken{expected}
	if !expected.Exists {
		*receipt = m.unavailable(*receipt, path, "target file is missing")
		return ErrNotFound
	}
	current, err := m.readToken(path)
	if err != nil {
		*receipt = m.invalidWithCode(*receipt, "TOKEN_READ_FAILED", path, err.Error())
		return err
	}
	if expected.Token != current.Token {
		receipt.Validation.Status = ValidationConflict
		receipt.Diagnostics = append(receipt.Diagnostics, Diagnostic{
			Severity: "error",
			Code:     "STALE_LOCK_TOKEN",
			Message:  "target file changed after preview",
			Path:     path,
		})
		return ErrConflict
	}
	return nil
}

func (m *Mutator) validateCreateToken(tokens []FileToken, path string, receipt *Receipt) error {
	expected, ok, err := tokenForPath(tokens, path)
	if err != nil {
		*receipt = m.invalid(*receipt, err.Error())
		return ErrInvalidInput
	}
	if !ok {
		*receipt = m.invalidWithCode(*receipt, "LOCK_TOKEN_REQUIRED", path, "lock token is required")
		return ErrInvalidInput
	}
	receipt.Validation.PreWriteTokens = []FileToken{expected}
	current, err := m.readToken(path)
	if err != nil {
		*receipt = m.invalidWithCode(*receipt, "TOKEN_READ_FAILED", path, err.Error())
		return err
	}
	if current.Exists && expected.Exists {
		*receipt = m.invalidWithCode(*receipt, "TARGET_ALREADY_EXISTS", path, "target already exists")
		return ErrInvalidInput
	}
	if expected.Token != current.Token {
		receipt.Validation.Status = ValidationConflict
		receipt.Diagnostics = append(receipt.Diagnostics, Diagnostic{
			Severity: "error",
			Code:     "STALE_LOCK_TOKEN",
			Message:  "target path changed after preview",
			Path:     path,
		})
		return ErrConflict
	}
	return nil
}

func (m *Mutator) readToken(relPath string) (FileToken, error) {
	rel, err := cleanRel(relPath)
	if err != nil {
		return FileToken{}, err
	}
	fullPath, err := m.absolutePath(rel)
	if err != nil {
		return FileToken{}, err
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return missingToken(rel), nil
		}
		return FileToken{}, fmt.Errorf("read token content %s: %w", rel, err)
	}
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return missingToken(rel), nil
		}
		return FileToken{}, fmt.Errorf("read token metadata %s: %w", rel, err)
	}
	if info.IsDir() {
		return FileToken{}, fmt.Errorf("token target is a directory: %s", rel)
	}
	contentHash := sha256Hex(content)
	token := FileToken{
		Path:        rel,
		Exists:      true,
		Size:        info.Size(),
		ModTime:     info.ModTime().UTC().Format(time.RFC3339Nano),
		ContentHash: contentHash,
	}
	token.Token = tokenDigest(token)
	return token, nil
}

func (m *Mutator) absolutePath(relPath string) (string, error) {
	rel, err := cleanRel(relPath)
	if err != nil {
		return "", err
	}
	fullPath := filepath.Join(m.root, filepath.FromSlash(rel))
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	relToRoot, err := filepath.Rel(m.root, absPath)
	if err != nil {
		return "", fmt.Errorf("resolve path under root: %w", err)
	}
	if relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes project root: %s", relPath)
	}
	return absPath, nil
}

func (m *Mutator) dateOrNow(value time.Time) time.Time {
	if value.IsZero() {
		return m.clock()
	}
	return value
}

func cleanRel(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path is required")
	}
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("absolute paths are not allowed: %s", path)
	}
	rel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if rel == "." || strings.HasPrefix(rel, "../") || rel == ".." {
		return "", fmt.Errorf("path escapes project root: %s", path)
	}
	return rel, nil
}

func missingToken(path string) FileToken {
	token := FileToken{Path: path, Exists: false}
	token.Token = tokenDigest(token)
	return token
}

func tokenDigest(token FileToken) string {
	return sha256Hex([]byte(fmt.Sprintf("%s\x00%t\x00%d\x00%s\x00%s",
		token.Path,
		token.Exists,
		token.Size,
		token.ModTime,
		token.ContentHash,
	)))
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func tokenForPath(tokens []FileToken, path string) (FileToken, bool, error) {
	rel, err := cleanRel(path)
	if err != nil {
		return FileToken{}, false, err
	}
	for _, token := range tokens {
		tokenPath, err := cleanRel(token.Path)
		if err != nil {
			return FileToken{}, false, err
		}
		if tokenPath == rel {
			token.Path = tokenPath
			return token, true, nil
		}
	}
	return FileToken{}, false, nil
}

func staleTokens(expected, current []FileToken) []string {
	currentByPath := make(map[string]FileToken, len(current))
	for _, token := range current {
		currentByPath[token.Path] = token
	}
	var stale []string
	for _, token := range expected {
		if currentToken, ok := currentByPath[token.Path]; !ok || currentToken.Token != token.Token {
			stale = append(stale, token.Path)
		}
	}
	sort.Strings(stale)
	return stale
}

func cloneTokens(tokens []FileToken) []FileToken {
	out := make([]FileToken, len(tokens))
	copy(out, tokens)
	return out
}

func tokenPaths(tokens []FileToken) []string {
	paths := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token.Path != "" {
			paths = append(paths, token.Path)
		}
	}
	return paths
}

func normalizeReleaseVersion(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}

func releaseTag(version string) string {
	return "v" + normalizeReleaseVersion(version)
}

func versionedChangelogPath(version string) string {
	parts := strings.Split(normalizeReleaseVersion(version), ".")
	minorDir := normalizeReleaseVersion(version)
	if len(parts) >= 2 {
		minorDir = parts[0] + "." + parts[1]
	}
	return ".aman-pm/changelog/" + minorDir + "/" + releaseTag(version) + ".md"
}

func defaultReleaseEvidencePaths(version string) []string {
	tag := releaseTag(version)
	return []string{
		ChangelogPath,
		versionedChangelogPath(version),
		".aman-pm/index.yaml",
		"VERSION",
		".aman-pm/validation/release/ci-" + tag + ".md",
		".aman-pm/validation/release/public-mirror-" + tag + ".md",
	}
}

func (m *Mutator) readTrimmedFile(relPath string) (string, error) {
	path, err := m.absolutePath(relPath)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(content)), nil
}

func (m *Mutator) readIndexVersion(relPath string) (string, string, error) {
	path, err := m.absolutePath(relPath)
	if err != nil {
		return "", "", err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	var doc struct {
		Version  string `yaml:"version"`
		Snapshot struct {
			Version string `yaml:"version"`
		} `yaml:"snapshot"`
	}
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return "", string(content), err
	}
	version := strings.TrimSpace(doc.Snapshot.Version)
	if version == "" {
		version = strings.TrimSpace(doc.Version)
	}
	return normalizeReleaseVersion(version), string(content), nil
}

func localGitTags(root string) ([]string, error) {
	out, err := exec.Command("git", "-C", root, "tag", "-l", "--sort=version:refname").Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	tags := make([]string, 0, len(lines))
	for _, line := range lines {
		tag := strings.TrimSpace(line)
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags, nil
}

func latestReleaseTag(tags []string) string {
	for i := len(tags) - 1; i >= 0; i-- {
		if strings.HasPrefix(tags[i], "v") {
			return tags[i]
		}
	}
	return ""
}

func hasGitTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}

func containsKnownReleaseGap(content, version string) bool {
	lower := strings.ToLower(content)
	if !strings.Contains(lower, strings.ToLower(releaseTag(version))) {
		return false
	}
	if strings.Contains(lower, "public-mirror verification") {
		return true
	}
	return strings.Contains(lower, "public mirror") &&
		strings.Contains(lower, "verification") &&
		(strings.Contains(lower, "open follow-up") ||
			strings.Contains(lower, "remains open") ||
			strings.Contains(lower, "needs"))
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func validateRequiredText(fields map[string]string) error {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if strings.TrimSpace(fields[key]) == "" {
			return fmt.Errorf("%s is required", key)
		}
	}
	return nil
}

func ensureTrailingNewline(content string) string {
	if content == "" || strings.HasSuffix(content, "\n") {
		return content
	}
	return content + "\n"
}

var allowedChangelogSections = map[string]bool{
	"Added":         true,
	"Changed":       true,
	"Fixed":         true,
	"Removed":       true,
	"Security":      true,
	"Documentation": true,
}

func insertChangelogFragment(content, section, summary string, details []string) (string, error) {
	headerStart, headerEnd, ok := findMarkdownHeading(content, "## "+section)
	if !ok {
		return "", fmt.Errorf("section %q is not present", section)
	}
	afterHeader := content[headerEnd:]
	nextSection := strings.Index(afterHeader, "\n## ")
	insertAt := len(content)
	if nextSection >= 0 {
		insertAt = headerEnd + nextSection
	}
	_ = headerStart
	fragment := buildChangelogFragment(summary, details)
	prefix := content[:insertAt]
	suffix := content[insertAt:]
	if !strings.HasSuffix(prefix, "\n") {
		prefix += "\n"
	}
	return prefix + "\n" + fragment + suffix, nil
}

func findMarkdownHeading(content, heading string) (int, int, bool) {
	start := 0
	for {
		idx := strings.Index(content[start:], heading)
		if idx < 0 {
			return 0, 0, false
		}
		idx += start
		lineStartOK := idx == 0 || content[idx-1] == '\n'
		lineEnd := idx + len(heading)
		lineEndOK := lineEnd == len(content) || content[lineEnd] == '\n' || content[lineEnd] == '\r'
		if lineStartOK && lineEndOK {
			if lineEnd < len(content) && content[lineEnd] == '\r' {
				lineEnd++
			}
			if lineEnd < len(content) && content[lineEnd] == '\n' {
				lineEnd++
			}
			return idx, lineEnd, true
		}
		start = idx + len(heading)
	}
}

func buildChangelogFragment(summary string, details []string) string {
	var builder strings.Builder
	builder.WriteString("- ")
	builder.WriteString(strings.TrimSpace(summary))
	builder.WriteByte('\n')
	for _, detail := range details {
		if strings.TrimSpace(detail) == "" {
			continue
		}
		builder.WriteString("  - ")
		builder.WriteString(strings.TrimSpace(detail))
		builder.WriteByte('\n')
	}
	return builder.String()
}

type itemKind struct {
	dir    string
	prefix string
}

var itemKinds = map[string]itemKind{
	"feature": {dir: "features", prefix: "FEAT-"},
	"task":    {dir: "tasks", prefix: "TASK-"},
	"bug":     {dir: "bugs", prefix: "BUG-"},
	"debt":    {dir: "debt", prefix: "DEBT-"},
	"spike":   {dir: "spikes", prefix: "SPIKE-"},
	"epic":    {dir: "epics", prefix: "EPIC-"},
}

func itemPath(req CreateItemRequest) (string, error) {
	kind, ok := itemKinds[req.Type]
	if !ok {
		return "", fmt.Errorf("unsupported item type %q", req.Type)
	}
	if strings.TrimSpace(req.ID) == "" {
		return "", errors.New("id is required")
	}
	if !strings.HasPrefix(req.ID, kind.prefix) {
		return "", fmt.Errorf("id %q must use %s prefix", req.ID, kind.prefix)
	}
	if strings.TrimSpace(req.Title) == "" {
		return "", errors.New("title is required")
	}
	folder, ok := statusFolder(req.Status)
	if !ok {
		return "", fmt.Errorf("unsupported status %q", req.Status)
	}
	return fmt.Sprintf(".aman-pm/backlog/%s/%s/%s-%s.md", kind.dir, folder, req.ID, slug(req.Title)), nil
}

func (m *Mutator) findItemByID(itemType, id string) (string, error) {
	kind, ok := itemKinds[itemType]
	if !ok {
		return "", fmt.Errorf("unsupported item type %q", itemType)
	}
	root, err := m.absolutePath(".aman-pm/backlog/" + kind.dir)
	if err != nil {
		return "", err
	}
	var found string
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") || found != "" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if frontmatterDeclaresID(string(content), id) {
			rel, err := filepath.Rel(m.root, path)
			if err != nil {
				return err
			}
			found = filepath.ToSlash(rel)
		}
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return found, nil
}

func frontmatterDeclaresID(content, id string) bool {
	if !strings.HasPrefix(content, "---\n") {
		return false
	}
	end := strings.Index(content[4:], "\n---")
	if end < 0 {
		return false
	}
	frontmatter := content[:end+4]
	for _, line := range strings.Split(frontmatter, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) != "id" {
			continue
		}
		declared := strings.Trim(strings.TrimSpace(value), `"'`)
		return declared == id
	}
	return false
}

func (m *Mutator) validateCreateItem(req CreateItemRequest) error {
	if err := validateCreateItemFields(req); err != nil {
		return err
	}
	rules, ok, err := m.loadMutationRules()
	if err != nil {
		return fmt.Errorf("rules.yaml validation failed: %w", err)
	}
	if ok {
		if err := validateCreateItemAgainstRules(req, rules); err != nil {
			return fmt.Errorf("rules.yaml validation failed: %w", err)
		}
	}
	return nil
}

func validateCreateItemFields(req CreateItemRequest) error {
	if _, err := itemPath(req); err != nil {
		return err
	}
	if err := validateRequiredText(map[string]string{
		"priority":   req.Priority,
		"parent":     req.Parent,
		"context":    req.Context,
		"created_by": defaultCreatedBy(req.CreatedBy),
	}); err != nil {
		return err
	}
	if !allowedPriorities[req.Priority] {
		return fmt.Errorf("unsupported priority %q", req.Priority)
	}
	if createdBy := defaultCreatedBy(req.CreatedBy); createdBy != "ai" && createdBy != "human" {
		return fmt.Errorf("created_by must be ai or human")
	}
	if len(req.AcceptanceCriteria) == 0 {
		return errors.New("acceptance criteria are required")
	}
	if len(req.Deliverables) == 0 {
		return errors.New("deliverables are required")
	}
	if len(req.Validation) == 0 {
		return errors.New("validation expectations are required")
	}
	if containsPlaceholder(req.AcceptanceCriteria...) || containsPlaceholder(req.Deliverables...) || containsPlaceholder(req.Validation...) {
		return errors.New("placeholder acceptance criteria, deliverables, or validation expectations are not allowed")
	}
	if containsHighConfidenceSecret(req.Context) ||
		containsHighConfidenceSecret(req.AcceptanceCriteria...) ||
		containsHighConfidenceSecret(req.Deliverables...) ||
		containsHighConfidenceSecret(req.Validation...) {
		return errors.New("item content looks like a secret")
	}
	return nil
}

var allowedPriorities = map[string]bool{"P0": true, "P1": true, "P2": true, "P3": true}

type mutationRulesDoc struct {
	StateMachine struct {
		AllStatuses      []string `yaml:"all_statuses"`
		DirectoryMapping struct {
			Defaults      map[string]any            `yaml:"defaults"`
			TypeOverrides map[string]map[string]any `yaml:"type_overrides"`
		} `yaml:"directory_mapping"`
	} `yaml:"state_machine"`
	ItemTypes map[string]struct {
		RequiredFields []string `yaml:"required_fields"`
	} `yaml:"item_types"`
	Conventions struct {
		Priorities map[string]string `yaml:"priorities"`
	} `yaml:"conventions"`
}

func (m *Mutator) loadMutationRules() (mutationRulesDoc, bool, error) {
	path, err := m.absolutePath(".aman-pm/rules.yaml")
	if err != nil {
		return mutationRulesDoc{}, false, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return mutationRulesDoc{}, false, nil
		}
		return mutationRulesDoc{}, false, err
	}
	var rules mutationRulesDoc
	if err := yaml.Unmarshal(content, &rules); err != nil {
		return mutationRulesDoc{}, false, err
	}
	return rules, true, nil
}

func validateCreateItemAgainstRules(req CreateItemRequest, rules mutationRulesDoc) error {
	itemRules, ok := rules.ItemTypes[req.Type]
	if len(rules.ItemTypes) > 0 && !ok {
		return fmt.Errorf("unsupported item type %q", req.Type)
	}
	fieldValues := map[string]string{
		"id":       req.ID,
		"type":     req.Type,
		"status":   req.Status,
		"priority": req.Priority,
		"created":  "generated",
	}
	for _, field := range itemRules.RequiredFields {
		if strings.TrimSpace(fieldValues[field]) == "" {
			return fmt.Errorf("missing required frontmatter field %q", field)
		}
	}
	if len(rules.Conventions.Priorities) > 0 {
		if _, ok := rules.Conventions.Priorities[req.Priority]; !ok {
			return fmt.Errorf("unsupported priority %q", req.Priority)
		}
	}
	if len(rules.StateMachine.AllStatuses) > 0 && !containsString(rules.StateMachine.AllStatuses, req.Status) {
		return fmt.Errorf("unsupported status %q", req.Status)
	}
	expectedFolders := expectedRulesStatusFolders(rules, req.Type, req.Status)
	if len(expectedFolders) > 0 {
		actualFolder, ok := statusFolder(req.Status)
		if !ok || !containsString(expectedFolders, actualFolder) {
			return fmt.Errorf("status %q belongs in %s, mutation would write %q", req.Status, strings.Join(expectedFolders, ","), actualFolder)
		}
	}
	return nil
}

func expectedRulesStatusFolders(rules mutationRulesDoc, itemType, status string) []string {
	if typeMapping := rules.StateMachine.DirectoryMapping.TypeOverrides[itemType]; typeMapping != nil {
		if folders := yamlStringList(typeMapping[status]); len(folders) > 0 {
			return folders
		}
	}
	if folders := yamlStringList(rules.StateMachine.DirectoryMapping.Defaults[status]); len(folders) > 0 {
		return folders
	}
	return nil
}

func yamlStringList(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		return []string{typed}
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if str, ok := item.(string); ok && str != "" {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func renderItem(req CreateItemRequest, created time.Time) string {
	date := created.UTC().Format(time.DateOnly)
	var builder strings.Builder
	fmt.Fprintf(&builder, "---\nid: %s\ntype: %s\nstatus: %s\npriority: %s\nparent: %s\nsprint: null\narchitecture_tags: []\ncreated: %q\ncreated_by: %s\n---\n\n",
		req.ID,
		req.Type,
		req.Status,
		req.Priority,
		req.Parent,
		date,
		defaultCreatedBy(req.CreatedBy),
	)
	fmt.Fprintf(&builder, "# %s: %s\n\n", req.ID, req.Title)
	fmt.Fprintf(&builder, "## Context\n\n%s\n\n", strings.TrimSpace(req.Context))
	builder.WriteString("## Acceptance Criteria\n\n")
	for _, ac := range req.AcceptanceCriteria {
		fmt.Fprintf(&builder, "- [ ] %s\n", strings.TrimSpace(ac))
	}
	builder.WriteString("\n## Deliverables\n\n")
	for _, deliverable := range req.Deliverables {
		fmt.Fprintf(&builder, "- %s\n", strings.TrimSpace(deliverable))
	}
	builder.WriteString("\n## Validation\n\n")
	for _, validation := range req.Validation {
		fmt.Fprintf(&builder, "- [ ] %s\n", strings.TrimSpace(validation))
	}
	fmt.Fprintf(&builder, "\n## History\n\n- %s: Created\n", date)
	return builder.String()
}

func defaultCreatedBy(value string) string {
	if strings.TrimSpace(value) == "" {
		return "ai"
	}
	return strings.TrimSpace(value)
}

func adrPath(req CreateADRRequest) (string, string, error) {
	if req.Number <= 0 {
		return "", "", errors.New("ADR number must be positive")
	}
	if strings.TrimSpace(req.Title) == "" {
		return "", "", errors.New("ADR title is required")
	}
	id := fmt.Sprintf("ADR-%03d", req.Number)
	return fmt.Sprintf(".aman-pm/decisions/%s-%s.md", id, slug(req.Title)), id, nil
}

func validateADR(req CreateADRRequest) error {
	if _, _, err := adrPath(req); err != nil {
		return err
	}
	if err := validateRequiredText(map[string]string{
		"context":  req.Context,
		"decision": req.Decision,
	}); err != nil {
		return err
	}
	if containsPlaceholder(req.Context, req.Decision) {
		return errors.New("ADR context and decision must be authored content")
	}
	return nil
}

func renderADR(req CreateADRRequest, date time.Time) string {
	return fmt.Sprintf(`# ADR-%03d: %s

**Status:** Proposed
**Date:** %s
**Supersedes:** None
**Superseded by:** None

---

## Context

%s

## Decision

%s

## Consequences

This ADR is proposed and must be reviewed before acceptance.
`, req.Number, strings.TrimSpace(req.Title), date.UTC().Format(time.DateOnly), strings.TrimSpace(req.Context), strings.TrimSpace(req.Decision))
}

func replaceFrontmatterStatus(content, fromStatus, toStatus string) (string, error) {
	lineEnding := "\n"
	if strings.HasPrefix(content, "---\r\n") {
		lineEnding = "\r\n"
	}
	prefix := "---" + lineEnding
	if !strings.HasPrefix(content, prefix) {
		return "", errors.New("frontmatter is required")
	}
	bodyStart := len(prefix)
	terminator := lineEnding + "---"
	terminatorStart := strings.Index(content[bodyStart:], terminator)
	if terminatorStart < 0 {
		return "", errors.New("frontmatter terminator is missing")
	}
	terminatorStart += bodyStart
	frontmatterBody := content[bodyStart:terminatorStart]
	rest := content[terminatorStart:]
	lines := strings.Split(frontmatterBody, lineEnding)
	replaced := false
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "status:") {
			indent := line[:len(line)-len(trimmed)]
			currentRaw := strings.TrimSpace(strings.TrimPrefix(trimmed, "status:"))
			current := strings.Trim(currentRaw, `"'`)
			if current != fromStatus {
				return "", fmt.Errorf("frontmatter status is %q, expected %q", current, fromStatus)
			}
			quote := ""
			if len(currentRaw) >= 2 {
				first := currentRaw[0]
				last := currentRaw[len(currentRaw)-1]
				if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
					quote = string(first)
				}
			}
			lines[i] = indent + "status: " + quote + toStatus + quote
			replaced = true
			break
		}
	}
	if !replaced {
		return "", errors.New("frontmatter status field is missing")
	}
	return prefix + strings.Join(lines, lineEnding) + rest, nil
}

func transitionAllowed(fromStatus, toStatus string) bool {
	for _, candidate := range transitions[fromStatus] {
		if candidate == toStatus {
			return true
		}
	}
	return false
}

var transitions = map[string][]string{
	"backlog":     {"ready", "scheduled", "deferred", "cancelled"},
	"ready":       {"scheduled", "active", "in_progress", "deferred", "cancelled"},
	"scheduled":   {"active", "in_progress", "deferred", "cancelled"},
	"active":      {"in_progress", "blocked", "review", "done", "cancelled"},
	"in_progress": {"blocked", "review", "done", "validated", "cancelled"},
	"blocked":     {"in_progress", "active", "cancelled", "deferred"},
	"review":      {"done", "validated", "in_progress", "cancelled"},
	"deferred":    {"backlog", "ready", "cancelled"},
	"done":        {"validated", "archived"},
	"resolved":    {"validated", "archived"},
	"validated":   {"archived"},
}

func statusFolder(status string) (string, bool) {
	folder, ok := statusFolders[status]
	return folder, ok
}

var statusFolders = map[string]string{
	"active":         "active",
	"in_progress":    "active",
	"blocked":        "active",
	"review":         "active",
	"backlog":        "active",
	"ready":          "active",
	"scheduled":      "active",
	"done":           "done",
	"resolved":       "resolved",
	"validated":      "done",
	"cancelled":      "cancelled",
	"wontfix":        "wontfix",
	"wont_implement": "resolved",
	"deferred":       "deferred",
	"deprecated":     "deprecated",
	"obsolete":       "resolved",
	"converted":      "resolved",
	"archived":       "done",
}

func slug(title string) string {
	var builder strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			builder.WriteRune(r)
			lastDash = false
		case !lastDash:
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func containsPlaceholder(values ...string) bool {
	for _, value := range values {
		lower := strings.ToLower(strings.TrimSpace(value))
		if lower == "" ||
			strings.Contains(lower, "placeholder") ||
			strings.Contains(lower, "todo") ||
			strings.Contains(lower, "tbd") ||
			strings.Contains(lower, "fixme") ||
			strings.Contains(lower, "xxx") ||
			placeholderTokenPattern.MatchString(value) {
			return true
		}
	}
	return false
}

var placeholderTokenPattern = regexp.MustCompile(`(?i)(\{[A-Z][A-Z0-9_ -]{2,}\}|<\s*(TODO|TBD|PLACEHOLDER|FIXME)\s*>)`)

func containsHighConfidenceSecret(values ...string) bool {
	scanner := secrets.NewScanner(secrets.DefaultPolicy())
	for _, value := range values {
		result := scanner.ScanContent(secrets.ContentInput{
			Path:    "pm-mutation-input",
			Content: []byte(value),
			Source:  secrets.SourcePMFileItem,
		})
		if len(result.Warnings) > 0 {
			return true
		}
	}
	return false
}

func writeFileAtomic(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pmmutation-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	if err := syncParentDir(path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func syncParentDir(path string) error {
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open parent directory: %w", err)
	}
	defer func() { _ = dir.Close() }()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync parent directory: %w", err)
	}
	return nil
}
