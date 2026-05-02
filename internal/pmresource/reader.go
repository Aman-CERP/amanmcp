package pmresource

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	// ErrUnknownResource is returned when a reader is asked for an unregistered
	// pm:// resource URI.
	ErrUnknownResource = errors.New("unknown PM resource")

	relatedIDPattern       = regexp.MustCompile(`\b(?:FEAT|TASK|BUG|DEBT|SPIKE|EPIC|ADR)-[A-Za-z0-9-]+\b`)
	timestampPattern       = regexp.MustCompile(`\b20\d{2}-\d{2}-\d{2}(?:T[0-9:+-]+)?\b`)
	itemTitlePrefixPattern = regexp.MustCompile(`^[A-Z]+-[A-Za-z0-9-]+:\s+`)
)

const defaultReaderCacheTTL = 2 * time.Second

// Reader builds read-only PM resource envelopes from local PM source files.
type Reader struct {
	root     string
	clock    func() time.Time
	cacheTTL time.Duration

	mu       sync.Mutex
	cached   *pmState
	cachedAt time.Time
}

// Option configures a Reader.
type Option func(*Reader)

// WithClock injects a deterministic clock for tests.
func WithClock(clock func() time.Time) Option {
	return func(r *Reader) {
		r.clock = clock
	}
}

// NewReader creates a read-only PM resource reader rooted at a repository path.
func NewReader(root string, opts ...Option) *Reader {
	r := &Reader{
		root:     root,
		cacheTTL: defaultReaderCacheTTL,
		clock: func() time.Time {
			return time.Now().UTC()
		},
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Read returns a shared-envelope PM resource payload.
func (r *Reader) Read(ctx context.Context, uri string) (*Envelope, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("failed to read PM resource %s: %w", uri, err)
	}

	now := r.clock().UTC()
	state := r.collect(now)

	switch uri {
	case URIComplyState:
		return r.readCompliance(state, uri), nil
	case URISubstrateCounters:
		return r.readCounters(state, uri), nil
	case URIParityViolations:
		return r.readParity(state, uri), nil
	case URIGateReadiness:
		return r.readReadiness(state, uri), nil
	case URIBacklogOpen:
		return r.readBacklogOpen(state, uri), nil
	case URIDecisionsActive:
		return r.readDecisionsActive(state, uri), nil
	case URIChangelogUnreleased:
		return r.readChangelogUnreleased(state, uri), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnknownResource, uri)
	}
}

type pmState struct {
	root               string
	pmDir              string
	pmAvailable        bool
	now                time.Time
	rules              rulesDoc
	rulesLoaded        bool
	backlogAvailable   bool
	changelogAvailable bool
	decisionsAvailable bool
	items              []pmItem
	violations         []ViolationRecord
	diagnostics        []Diagnostic
	backlogIndexItems  []backlogIndexItem
	projectIndex       projectIndex
	activeSprint       sprintData
	decisions          []Decision
	changelogEntries   []ChangelogEntry
	sourcePaths        map[string]struct{}
	latestSourceMod    time.Time
	readModel          readModelInfo
	terminalStatuses   map[string]bool
}

type rulesDoc struct {
	StateMachine struct {
		AllStatuses      []string            `yaml:"all_statuses"`
		Categories       map[string][]string `yaml:"categories"`
		DirectoryMapping struct {
			Defaults      map[string]any            `yaml:"defaults"`
			TypeOverrides map[string]map[string]any `yaml:"type_overrides"`
		} `yaml:"directory_mapping"`
	} `yaml:"state_machine"`
	ItemTypes map[string]struct {
		RequiredFields             []string `yaml:"required_fields"`
		RequiresAcceptanceCriteria bool     `yaml:"requires_acceptance_criteria"`
	} `yaml:"item_types"`
	Conventions struct {
		BacklogRoot      string `yaml:"backlog_root"`
		ACSectionHeading string `yaml:"ac_section_heading"`
	} `yaml:"conventions"`
}

type pmItem struct {
	ID           string
	Type         string
	Status       string
	Priority     string
	Parent       string
	Sprint       string
	SourcePath   string
	Title        string
	StatusFolder string
	Frontmatter  map[string]any
	Body         string
}

type backlogIndexItem struct {
	ID       string `yaml:"id"`
	Type     string `yaml:"type"`
	Title    string `yaml:"title"`
	Status   string `yaml:"status"`
	Priority string `yaml:"priority"`
	File     string `yaml:"file"`
}

type backlogIndex struct {
	Items []backlogIndexItem `yaml:"items"`
}

type projectIndex struct {
	Snapshot struct {
		Generated string `yaml:"generated"`
		Sprint    any    `yaml:"sprint"`
	} `yaml:"snapshot"`
	Metrics struct {
		GeneratedAt     string         `yaml:"generated_at"`
		TotalItems      int            `yaml:"total_items"`
		ByType          map[string]int `yaml:"by_type"`
		ByStatus        map[string]int `yaml:"by_status"`
		ByPriority      map[string]int `yaml:"by_priority"`
		ActiveBreakdown map[string]int `yaml:"active_breakdown"`
	} `yaml:"metrics"`
}

type sprintData struct {
	Sprint     string
	SourcePath string
	Items      []sprintItem
}

type sprintItem struct {
	ID     string
	Status string
}

type readModelInfo struct {
	Path       string
	Status     string
	ModifiedAt time.Time
}

func (r *Reader) collect(now time.Time) pmState {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cached != nil && r.cacheTTL > 0 {
		age := now.Sub(r.cachedAt)
		if age >= 0 && age < r.cacheTTL {
			state := *r.cached
			state.now = now
			return state
		}
	}

	state := r.collectUncached(now)
	r.cached = &state
	r.cachedAt = now
	return state
}

func (r *Reader) collectUncached(now time.Time) pmState {
	root := r.root
	if root == "" {
		root = "."
	}
	state := pmState{
		root:             root,
		pmDir:            filepath.Join(root, ".aman-pm"),
		now:              now,
		sourcePaths:      make(map[string]struct{}),
		terminalStatuses: make(map[string]bool),
	}

	info, err := os.Stat(state.pmDir)
	if err != nil || !info.IsDir() {
		state.diagnostics = append(state.diagnostics, Diagnostic{
			Severity:   "error",
			Code:       "PM_ROOT_UNAVAILABLE",
			Message:    ".aman-pm directory is missing or unreadable",
			SourcePath: ".aman-pm",
		})
		state.readModel = r.readModelFreshness(&state)
		return state
	}
	state.pmAvailable = true

	r.loadRules(&state)
	r.scanBacklog(&state)
	r.loadBacklogIndex(&state)
	r.loadProjectIndex(&state)
	r.loadActiveSprint(&state)
	r.scanDecisions(&state)
	r.parseChangelog(&state)
	r.checkCounterConsistency(&state)
	r.checkChangelogCurrency(&state)
	r.checkDuplicateIDs(&state)
	state.readModel = r.readModelFreshness(&state)

	return state
}

func (r *Reader) loadRules(state *pmState) {
	path := filepath.Join(state.pmDir, "rules.yaml")
	state.addSource(path)
	if err := loadYAML(path, &state.rules); err != nil {
		state.diagnostics = append(state.diagnostics, Diagnostic{
			Severity:   "error",
			Code:       "RULES_UNAVAILABLE",
			Message:    fmt.Sprintf("failed to read rules.yaml: %v", err),
			SourcePath: state.rel(path),
		})
		state.violations = append(state.violations, newViolation("POL-RULES-001", "critical", state.rel(path), "rules.yaml is missing or invalid", "restore a parseable rules.yaml"))
		return
	}
	state.rulesLoaded = true
	for _, status := range state.rules.StateMachine.Categories["terminal"] {
		state.terminalStatuses[status] = true
	}
}

func (r *Reader) scanBacklog(state *pmState) {
	backlogRoot := state.resolvePMPath(state.rules.Conventions.BacklogRoot, "backlog")
	info, err := os.Stat(backlogRoot)
	if err != nil || !info.IsDir() {
		state.diagnostics = append(state.diagnostics, Diagnostic{
			Severity:   "error",
			Code:       "BACKLOG_UNAVAILABLE",
			Message:    "backlog root is missing or unreadable",
			SourcePath: state.rel(backlogRoot),
		})
		return
	}
	state.backlogAvailable = true
	state.addSource(backlogRoot)

	typeDirs := map[string]string{
		"epics":    "epic",
		"features": "feature",
		"tasks":    "task",
		"spikes":   "spike",
		"bugs":     "bug",
		"debt":     "debt",
	}
	statusSet := make(map[string]bool)
	for _, status := range state.rules.StateMachine.AllStatuses {
		statusSet[status] = true
	}

	err = filepath.WalkDir(backlogRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			state.diagnostics = append(state.diagnostics, Diagnostic{
				Severity:   "error",
				Code:       "BACKLOG_SCAN_ERROR",
				Message:    err.Error(),
				SourcePath: state.rel(path),
			})
			return nil
		}
		if entry.IsDir() || filepath.Ext(path) != ".md" || entry.Name() == "README.md" || strings.HasPrefix(entry.Name(), "index.") {
			return nil
		}

		state.addSource(path)
		relToBacklog, err := filepath.Rel(backlogRoot, path)
		if err != nil {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(relToBacklog), "/")
		if len(parts) < 3 {
			return nil
		}
		itemType := typeDirs[parts[0]]
		if itemType == "" {
			return nil
		}
		statusFolder := parts[1]
		frontmatter, body, err := parseFrontmatter(path)
		relPath := state.rel(path)
		if err != nil {
			state.violations = append(state.violations, newViolation("POL-FM-001", "error", relPath, err.Error(), "fix YAML frontmatter syntax"))
			return nil
		}
		if len(frontmatter) == 0 {
			state.violations = append(state.violations, newViolation("POL-FM-001", "error", relPath, "missing YAML frontmatter", "add required item metadata"))
			return nil
		}

		item := pmItem{
			ID:           scalarString(frontmatter["id"]),
			Type:         scalarString(frontmatter["type"]),
			Status:       scalarString(frontmatter["status"]),
			Priority:     scalarString(frontmatter["priority"]),
			Parent:       scalarString(frontmatter["parent"]),
			Sprint:       scalarString(frontmatter["sprint"]),
			SourcePath:   relPath,
			Title:        normalizeItemTitle(scalarString(frontmatter["id"]), markdownTitle(body, scalarString(frontmatter["id"]))),
			StatusFolder: statusFolder,
			Frontmatter:  frontmatter,
			Body:         body,
		}
		if item.ID == "" {
			item.ID = strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		}
		if item.Type == "" {
			item.Type = itemType
		}
		state.items = append(state.items, item)

		itemRules := state.rules.ItemTypes[item.Type]
		for _, field := range itemRules.RequiredFields {
			if scalarString(frontmatter[field]) == "" {
				state.violations = append(state.violations, newViolation("POL-004", "error", relPath, fmt.Sprintf("missing required frontmatter field %q", field), "add the required field"))
			}
		}
		if len(statusSet) > 0 && !statusSet[item.Status] {
			state.violations = append(state.violations, newViolation("POL-005", "error", relPath, fmt.Sprintf("invalid status %q", item.Status), "use a status from rules.yaml"))
		}
		if expected := state.expectedStatusFolders(item.Type, item.Status); len(expected) > 0 && !containsString(expected, statusFolder) {
			state.violations = append(state.violations, newViolation("POL-002", "error", relPath, fmt.Sprintf("status %q belongs in %s, found %q", item.Status, strings.Join(expected, ","), statusFolder), "move the file or change status"))
		}
		if itemRules.RequiresAcceptanceCriteria && state.statusRequiresAC(item.Status) && !hasAcceptanceCriteria(body, state.rules.Conventions.ACSectionHeading) {
			state.violations = append(state.violations, newViolation("POL-011", "error", relPath, "missing checkable acceptance criteria", "add a ## Acceptance Criteria section with checkboxes"))
		}
		return nil
	})
	if err != nil {
		state.diagnostics = append(state.diagnostics, Diagnostic{
			Severity:   "error",
			Code:       "BACKLOG_SCAN_ERROR",
			Message:    err.Error(),
			SourcePath: state.rel(backlogRoot),
		})
	}
}

func (r *Reader) loadBacklogIndex(state *pmState) {
	path := filepath.Join(state.pmDir, "backlog", "index.yaml")
	state.addSource(path)
	var index backlogIndex
	if err := loadYAML(path, &index); err != nil {
		state.diagnostics = append(state.diagnostics, Diagnostic{
			Severity:   "warning",
			Code:       "BACKLOG_INDEX_UNAVAILABLE",
			Message:    fmt.Sprintf("failed to read backlog index: %v", err),
			SourcePath: state.rel(path),
		})
		return
	}
	state.backlogIndexItems = index.Items
}

func (r *Reader) loadProjectIndex(state *pmState) {
	path := filepath.Join(state.pmDir, "index.yaml")
	state.addSource(path)
	var index projectIndex
	if err := loadYAML(path, &index); err != nil {
		state.diagnostics = append(state.diagnostics, Diagnostic{
			Severity:   "warning",
			Code:       "PROJECT_INDEX_UNAVAILABLE",
			Message:    fmt.Sprintf("failed to read project index: %v", err),
			SourcePath: state.rel(path),
		})
		return
	}
	state.projectIndex = index
}

func (r *Reader) loadActiveSprint(state *pmState) {
	activeRoot := filepath.Join(state.pmDir, "sprints", "active")
	state.addSource(activeRoot)
	entries, err := os.ReadDir(activeRoot)
	if err != nil {
		state.diagnostics = append(state.diagnostics, Diagnostic{
			Severity:   "warning",
			Code:       "ACTIVE_SPRINT_UNAVAILABLE",
			Message:    "active sprint directory is missing or unreadable",
			SourcePath: state.rel(activeRoot),
		})
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(activeRoot, entry.Name(), "items.yaml")
		var raw struct {
			Sprint any              `yaml:"sprint"`
			Items  []map[string]any `yaml:"items"`
		}
		if err := loadYAML(path, &raw); err != nil {
			state.diagnostics = append(state.diagnostics, Diagnostic{
				Severity:   "warning",
				Code:       "ACTIVE_SPRINT_UNAVAILABLE",
				Message:    fmt.Sprintf("failed to read active sprint items: %v", err),
				SourcePath: state.rel(path),
			})
			continue
		}
		state.addSource(path)
		state.activeSprint = sprintData{
			Sprint:     scalarString(raw.Sprint),
			SourcePath: state.rel(path),
			Items:      flattenSprintItems(raw.Items),
		}
		return
	}
}

func (r *Reader) scanDecisions(state *pmState) {
	decisionsRoot := filepath.Join(state.pmDir, "decisions")
	info, err := os.Stat(decisionsRoot)
	if err != nil || !info.IsDir() {
		state.diagnostics = append(state.diagnostics, Diagnostic{
			Severity:   "warning",
			Code:       "DECISIONS_UNAVAILABLE",
			Message:    "decisions directory is missing or unreadable",
			SourcePath: state.rel(decisionsRoot),
		})
		return
	}
	state.decisionsAvailable = true
	entries, err := os.ReadDir(decisionsRoot)
	if err != nil {
		state.diagnostics = append(state.diagnostics, Diagnostic{
			Severity:   "warning",
			Code:       "DECISIONS_UNAVAILABLE",
			Message:    err.Error(),
			SourcePath: state.rel(decisionsRoot),
		})
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" || entry.Name() == "index.md" || entry.Name() == "deferred-register.md" || strings.Contains(entry.Name(), "template") {
			continue
		}
		path := filepath.Join(decisionsRoot, entry.Name())
		content, err := os.ReadFile(path)
		relPath := state.rel(path)
		state.addSource(path)
		if err != nil {
			state.diagnostics = append(state.diagnostics, Diagnostic{
				Severity:   "error",
				Code:       "DECISION_READ_ERROR",
				Message:    err.Error(),
				SourcePath: relPath,
			})
			continue
		}
		text := string(content)
		status := markdownMetadata(text, "Status")
		if status == "" {
			state.violations = append(state.violations, newViolation("POL-DECISION-001", "error", relPath, "decision is missing **Status:** metadata", "add a decision status"))
		}
		if isInactiveDecision(status) {
			continue
		}
		state.decisions = append(state.decisions, Decision{
			ID:           strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())),
			Title:        markdownTitle(text, strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))),
			Status:       status,
			SourcePath:   relPath,
			Supersedes:   parseDecisionRefs(markdownMetadata(text, "Supersedes")),
			SupersededBy: parseDecisionRefs(markdownMetadata(text, "Superseded by")),
		})
	}
	sort.Slice(state.decisions, func(i, j int) bool {
		return state.decisions[i].ID < state.decisions[j].ID
	})
}

func (r *Reader) parseChangelog(state *pmState) {
	path := filepath.Join(state.pmDir, "changelog", "unreleased.md")
	content, err := os.ReadFile(path)
	state.addSource(path)
	if err != nil {
		state.diagnostics = append(state.diagnostics, Diagnostic{
			Severity:   "error",
			Code:       "CHANGELOG_UNAVAILABLE",
			Message:    "unreleased changelog is missing or unreadable",
			SourcePath: state.rel(path),
		})
		return
	}
	state.changelogAvailable = true
	section := ""
	for idx, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		text := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		state.changelogEntries = append(state.changelogEntries, ChangelogEntry{
			Section:         section,
			Text:            text,
			SourcePath:      state.rel(path),
			Line:            idx + 1,
			RelatedIDs:      relatedIDPattern.FindAllString(text, -1),
			ParsedTimestamp: firstMatch(timestampPattern, text),
		})
	}
}

func (r *Reader) checkCounterConsistency(state *pmState) {
	if !state.pmAvailable || !state.rulesLoaded {
		return
	}
	path := filepath.Join(state.pmDir, "counters.yaml")
	state.addSource(path)
	var counters map[string]any
	if err := loadYAML(path, &counters); err != nil {
		state.diagnostics = append(state.diagnostics, Diagnostic{
			Severity:   "warning",
			Code:       "COUNTERS_UNAVAILABLE",
			Message:    fmt.Sprintf("failed to read counters.yaml: %v", err),
			SourcePath: state.rel(path),
		})
		return
	}
	highest := highestIDs(state.items)
	sequences := asMap(counters["sequences"])
	for _, itemType := range []string{"bug", "debt", "spike"} {
		expected, ok := highest[itemType][""]
		if !ok {
			continue
		}
		current := asInt(asMap(sequences[itemType])["current"])
		if current < expected {
			state.violations = append(state.violations, newViolation("POL-007", "warning", state.rel(path), fmt.Sprintf("counter for %s is %d, below filesystem highest %d", itemType, current, expected), "update counters.yaml after confirming skipped IDs"))
		}
	}
}

func (r *Reader) checkChangelogCurrency(state *pmState) {
	if !state.pmAvailable {
		return
	}
	if !state.changelogAvailable {
		state.violations = append(state.violations, newViolation("POL-013", "warning", ".aman-pm/changelog/unreleased.md", "unreleased changelog is missing", "create .aman-pm/changelog/unreleased.md"))
		return
	}
	if len(state.changelogEntries) == 0 {
		state.violations = append(state.violations, newViolation("POL-013", "warning", ".aman-pm/changelog/unreleased.md", "unreleased changelog has no entries", "add a PM changelog entry"))
	}
}

func (r *Reader) checkDuplicateIDs(state *pmState) {
	byID := make(map[string][]pmItem)
	for _, item := range state.items {
		byID[item.ID] = append(byID[item.ID], item)
	}
	for id, items := range byID {
		if len(items) <= 1 {
			continue
		}
		for _, item := range items {
			state.violations = append(state.violations, newViolation("POL-003", "critical", item.SourcePath, fmt.Sprintf("duplicate item ID %q", id), "rename or merge duplicate work items"))
		}
	}
}

func (r *Reader) readModelFreshness(state *pmState) readModelInfo {
	path := filepath.Join(state.root, ".amanmcp", "amanpm-read-model.sqlite")
	relPath := state.rel(path)
	info, err := os.Stat(path)
	if err != nil {
		return readModelInfo{Path: relPath, Status: "unavailable"}
	}
	status := "fresh"
	if !state.latestSourceMod.IsZero() && info.ModTime().Before(state.latestSourceMod) {
		status = "stale"
	}
	return readModelInfo{Path: relPath, Status: status, ModifiedAt: info.ModTime().UTC()}
}

func (r *Reader) readCompliance(state pmState, uri string) *Envelope {
	status := ValidationOK
	if !state.pmAvailable || !state.rulesLoaded || !state.backlogAvailable {
		status = ValidationUnavailable
	} else if hasBlockingViolations(state.violations) {
		status = ValidationInvalid
	}
	data := ComplianceData{
		Status:                 complianceStatus(status, state.violations),
		Mode:                   "advisory",
		ViolationCount:         len(state.violations),
		BlockingViolationCount: blockingViolationCount(state.violations),
		Violations:             state.violations,
		CheckedCommand:         "in-process collector equivalent to amanpm-comply --no-sync-check",
	}
	return state.envelope(uri, status, "pm_compliance_collector", data, state.diagnostics, state.blockers())
}

func (r *Reader) readCounters(state pmState, uri string) *Envelope {
	status := state.baseResourceStatus()
	if status == ValidationOK && state.readModel.Status != "fresh" {
		status = ValidationStale
	}
	diagnostics := append([]Diagnostic{}, state.diagnostics...)
	diagnostics = appendReadModelDiagnostics(diagnostics, state.readModel)
	data := CountersData{
		TotalItems:      len(state.items),
		OpenItemCount:   countOpenItems(state.items, state.terminalStatuses),
		ByType:          countBy(state.items, func(item pmItem) string { return item.Type }),
		ByStatus:        countBy(state.items, func(item pmItem) string { return item.Status }),
		ByPriority:      countBy(state.items, func(item pmItem) string { return item.Priority }),
		ActiveBreakdown: activeBreakdown(state.items, state.terminalStatuses),
		Sprint:          state.sprintCounters(),
		ReadModel:       state.readModel.data(),
	}
	return state.envelope(uri, status, "pm_counter_collector", data, diagnostics, state.blockers())
}

func (r *Reader) readParity(state pmState, uri string) *Envelope {
	parityViolations := state.parityViolations()
	status := state.baseResourceStatus()
	if status == ValidationOK && len(parityViolations) > 0 {
		status = ValidationInvalid
	}
	if status == ValidationOK && state.readModel.Status == "stale" {
		status = ValidationStale
	}
	diagnostics := append([]Diagnostic{}, state.diagnostics...)
	diagnostics = appendReadModelDiagnostics(diagnostics, state.readModel)
	data := ParityData{
		ViolationCount: len(parityViolations),
		Violations:     parityViolations,
	}
	blockers := state.blockers()
	for _, violation := range parityViolations {
		if violation.Severity == "error" || violation.Severity == "critical" {
			blockers = append(blockers, ValidationBlocker{
				Code:       "PM_PARITY_MISMATCH",
				Severity:   violation.Severity,
				Message:    fmt.Sprintf("%s expected %s actual %s", violation.Check, violation.Expected, violation.Actual),
				SourcePath: violation.SourcePath,
			})
		}
	}
	return state.envelope(uri, status, "pm_parity_collector", data, diagnostics, blockers)
}

func (r *Reader) readReadiness(state pmState, uri string) *Envelope {
	gates := state.readinessGates()
	status := ValidationOK
	blockerCount := 0
	for _, gate := range gates {
		switch gate.Status {
		case string(ValidationInvalid):
			status = ValidationInvalid
		case string(ValidationUnavailable):
			if status != ValidationInvalid {
				status = ValidationUnavailable
			}
		case string(ValidationStale):
			if status == ValidationOK {
				status = ValidationStale
			}
		}
		blockerCount += len(gate.Blockers)
	}
	data := ReadinessData{
		Status:       string(status),
		BlockerCount: blockerCount,
		Gates:        gates,
	}
	return state.envelope(uri, status, "pm_readiness_adapter", data, state.diagnostics, state.blockers())
}

func (r *Reader) readBacklogOpen(state pmState, uri string) *Envelope {
	status := state.baseResourceStatus()
	items := make([]BacklogItem, 0)
	if status != ValidationUnavailable {
		for _, item := range state.items {
			if state.terminalStatuses[item.Status] {
				continue
			}
			items = append(items, BacklogItem{
				ID:         item.ID,
				Type:       item.Type,
				Status:     item.Status,
				Priority:   item.Priority,
				Parent:     item.Parent,
				Sprint:     item.Sprint,
				SourcePath: item.SourcePath,
				Title:      item.Title,
			})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
	data := BacklogOpenData{
		Count: len(items),
		Empty: len(items) == 0,
		Items: items,
	}
	return state.envelope(uri, status, "pm_backlog_file_scan", data, state.diagnostics, state.blockers())
}

func (r *Reader) readDecisionsActive(state pmState, uri string) *Envelope {
	status := ValidationOK
	if !state.pmAvailable || !state.decisionsAvailable {
		status = ValidationUnavailable
	} else if hasBlockingViolations(state.violations) {
		status = ValidationInvalid
	}
	data := DecisionsActiveData{
		Count:     len(state.decisions),
		Empty:     len(state.decisions) == 0,
		Decisions: state.decisions,
	}
	return state.envelope(uri, status, "pm_decision_file_scan", data, state.diagnostics, state.blockers())
}

func (r *Reader) readChangelogUnreleased(state pmState, uri string) *Envelope {
	status := ValidationOK
	diagnostics := append([]Diagnostic{}, state.diagnostics...)
	if !state.pmAvailable || !state.changelogAvailable {
		status = ValidationUnavailable
	} else if len(state.changelogEntries) == 0 {
		status = ValidationInvalid
		diagnostics = append(diagnostics, Diagnostic{
			Severity:   "error",
			Code:       "CHANGELOG_EMPTY",
			Message:    "unreleased changelog has no parseable entries",
			SourcePath: ".aman-pm/changelog/unreleased.md",
		})
	}
	data := ChangelogUnreleasedData{
		Count:   len(state.changelogEntries),
		Empty:   len(state.changelogEntries) == 0,
		Entries: state.changelogEntries,
	}
	return state.envelope(uri, status, "pm_changelog_parser", data, diagnostics, state.blockers())
}

func (s pmState) baseResourceStatus() ValidationStatus {
	if !s.pmAvailable || !s.rulesLoaded || !s.backlogAvailable {
		return ValidationUnavailable
	}
	if hasBlockingViolations(s.violations) {
		return ValidationInvalid
	}
	return ValidationOK
}

func (s pmState) envelope(uri string, status ValidationStatus, method string, data any, diagnostics []Diagnostic, blockers []ValidationBlocker) *Envelope {
	diagnostics = append(append([]Diagnostic{}, diagnostics...), violationDiagnostics(s.violations)...)
	sourcePaths := sortedKeys(s.sourcePaths)
	readModelModified := ""
	if !s.readModel.ModifiedAt.IsZero() {
		readModelModified = s.readModel.ModifiedAt.Format(time.RFC3339)
	}
	sourceLatest := ""
	if !s.latestSourceMod.IsZero() {
		sourceLatest = s.latestSourceMod.UTC().Format(time.RFC3339)
	}
	return &Envelope{
		SchemaVersion: SchemaVersion,
		ResourceURI:   uri,
		GeneratedAt:   s.now.Format(time.RFC3339),
		Authority: Authority{
			Source:        "amanpm_file_ssot",
			Level:         authorityLevel(status),
			Authoritative: status == ValidationOK,
			Note:          authorityNote(status),
		},
		SourcePaths: sourcePaths,
		Derivation: Derivation{
			Method:   method,
			ReadOnly: true,
			Inputs:   sourcePaths,
		},
		Validation: Validation{
			Status:    status,
			CheckedAt: s.now.Format(time.RFC3339),
			Freshness: Freshness{
				SourceLatestModified: sourceLatest,
				ReadModelPath:        s.readModel.Path,
				ReadModelModified:    readModelModified,
				ReadModelStatus:      s.readModel.Status,
			},
			Blockers: blockers,
		},
		Diagnostics: diagnostics,
		Data:        data,
	}
}

func violationDiagnostics(violations []ViolationRecord) []Diagnostic {
	diagnostics := make([]Diagnostic, 0, len(violations))
	for _, violation := range violations {
		diagnostics = append(diagnostics, Diagnostic{
			Severity:   violation.Severity,
			Code:       violation.PolicyID,
			Message:    violation.Message,
			SourcePath: violation.SourcePath,
		})
	}
	return diagnostics
}

func (s *pmState) addSource(path string) {
	relPath := s.rel(path)
	s.sourcePaths[relPath] = struct{}{}
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() && info.ModTime().After(s.latestSourceMod) {
		s.latestSourceMod = info.ModTime()
	}
}

func (s pmState) rel(path string) string {
	rel, err := filepath.Rel(s.root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func (s pmState) resolvePMPath(configured, fallback string) string {
	value := configured
	if value == "" {
		value = fallback
	}
	if filepath.IsAbs(value) {
		return value
	}
	if strings.HasPrefix(value, ".aman-pm/") {
		return filepath.Join(s.pmDir, strings.TrimPrefix(value, ".aman-pm/"))
	}
	return filepath.Join(s.pmDir, value)
}

func (s pmState) expectedStatusFolders(itemType, status string) []string {
	if override := s.rules.StateMachine.DirectoryMapping.TypeOverrides[itemType]; override != nil {
		if values := stringSliceFromAny(override[status]); len(values) > 0 {
			return values
		}
	}
	return stringSliceFromAny(s.rules.StateMachine.DirectoryMapping.Defaults[status])
}

func (s pmState) statusRequiresAC(status string) bool {
	for _, category := range []string{"planning", "execution"} {
		if containsString(s.rules.StateMachine.Categories[category], status) {
			return true
		}
	}
	return false
}

func (s pmState) blockers() []ValidationBlocker {
	blockers := make([]ValidationBlocker, 0)
	for _, violation := range s.violations {
		if !violation.Blocking {
			continue
		}
		blockers = append(blockers, ValidationBlocker{
			Code:       violation.PolicyID,
			Severity:   violation.Severity,
			Message:    violation.Message,
			SourcePath: violation.SourcePath,
		})
	}
	return blockers
}

func (s pmState) sprintCounters() SprintCounters {
	counters := SprintCounters{
		ActiveSprint: s.activeSprint.Sprint,
		ItemCount:    len(s.activeSprint.Items),
		ByStatus:     make(map[string]int),
		SourcePath:   s.activeSprint.SourcePath,
	}
	for _, item := range s.activeSprint.Items {
		counters.ByStatus[item.Status]++
	}
	return counters
}

func (s pmState) parityViolations() []ParityViolation {
	violations := make([]ParityViolation, 0)
	if len(s.backlogIndexItems) == 0 && len(s.items) == 0 {
		return violations
	}
	indexByID := make(map[string]backlogIndexItem)
	for _, item := range s.backlogIndexItems {
		indexByID[item.ID] = item
		sourcePath := ".aman-pm/backlog/" + filepath.ToSlash(item.File)
		if _, err := os.Stat(filepath.Join(s.root, sourcePath)); err != nil {
			violations = append(violations, ParityViolation{
				Check:      "backlog_index_source_exists",
				SourcePath: sourcePath,
				Expected:   "source file exists",
				Actual:     "missing",
				Severity:   "error",
			})
		}
	}
	for _, item := range s.items {
		indexItem, ok := indexByID[item.ID]
		if !ok {
			violations = append(violations, ParityViolation{
				Check:      "backlog_index_contains_item",
				SourcePath: item.SourcePath,
				Expected:   "indexed",
				Actual:     "missing",
				Severity:   "error",
			})
			continue
		}
		compare := []struct {
			check    string
			expected string
			actual   string
		}{
			{"status", item.Status, indexItem.Status},
			{"type", item.Type, indexItem.Type},
			{"priority", item.Priority, indexItem.Priority},
			{"title", item.Title, indexItem.Title},
		}
		for _, candidate := range compare {
			if candidate.expected != candidate.actual {
				violations = append(violations, ParityViolation{
					Check:      "backlog_index_" + candidate.check,
					SourcePath: item.SourcePath,
					Expected:   candidate.expected,
					Actual:     candidate.actual,
					Severity:   "error",
				})
			}
		}
	}
	if s.projectIndex.Metrics.TotalItems != 0 && s.projectIndex.Metrics.TotalItems != len(s.items) {
		violations = append(violations, ParityViolation{
			Check:      "project_index_total_items",
			SourcePath: ".aman-pm/index.yaml",
			Expected:   strconv.Itoa(len(s.items)),
			Actual:     strconv.Itoa(s.projectIndex.Metrics.TotalItems),
			Severity:   "warning",
		})
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].SourcePath == violations[j].SourcePath {
			return violations[i].Check < violations[j].Check
		}
		return violations[i].SourcePath < violations[j].SourcePath
	})
	return violations
}

func (s pmState) readinessGates() []ReadinessGate {
	checkedAt := s.now.Format(time.RFC3339)
	gates := []ReadinessGate{
		{
			ID:            "pm-validation",
			Status:        string(ValidationOK),
			EvidencePaths: []string{".aman-pm/rules.yaml", ".aman-pm/backlog/"},
			CheckedAt:     checkedAt,
		},
		{
			ID:            "active-sprint",
			Status:        string(ValidationOK),
			EvidencePaths: []string{s.activeSprint.SourcePath},
			CheckedAt:     checkedAt,
		},
		{
			ID:            "read-model-freshness",
			Status:        string(ValidationOK),
			EvidencePaths: []string{s.readModel.Path},
			CheckedAt:     checkedAt,
		},
		{
			ID:            "unreleased-changelog",
			Status:        string(ValidationOK),
			EvidencePaths: []string{".aman-pm/changelog/unreleased.md"},
			CheckedAt:     checkedAt,
		},
	}
	if !s.pmAvailable || !s.rulesLoaded || !s.backlogAvailable {
		gates[0].Status = string(ValidationUnavailable)
		gates[0].Blockers = append(gates[0].Blockers, "PM rules or backlog sources are unavailable")
	} else if hasBlockingViolations(s.violations) {
		gates[0].Status = string(ValidationInvalid)
		gates[0].Blockers = append(gates[0].Blockers, "PM validation has blocking violations")
	}
	if s.activeSprint.SourcePath == "" {
		gates[1].Status = string(ValidationUnavailable)
		gates[1].Blockers = append(gates[1].Blockers, "active sprint items.yaml is unavailable")
	}
	switch s.readModel.Status {
	case "stale":
		gates[2].Status = string(ValidationStale)
		gates[2].Blockers = append(gates[2].Blockers, "read model is older than source PM files")
	case "unavailable":
		gates[2].Status = string(ValidationStale)
		gates[2].Blockers = append(gates[2].Blockers, "read model is unavailable; direct-file fallback required")
	}
	if !s.changelogAvailable {
		gates[3].Status = string(ValidationUnavailable)
		gates[3].Blockers = append(gates[3].Blockers, "unreleased changelog is unavailable")
	} else if len(s.changelogEntries) == 0 {
		gates[3].Status = string(ValidationInvalid)
		gates[3].Blockers = append(gates[3].Blockers, "unreleased changelog has no entries")
	}
	return gates
}

func (r readModelInfo) data() ReadModelData {
	modified := ""
	if !r.ModifiedAt.IsZero() {
		modified = r.ModifiedAt.Format(time.RFC3339)
	}
	return ReadModelData{
		Path:       r.Path,
		Status:     r.Status,
		ModifiedAt: modified,
	}
}

func appendReadModelDiagnostics(diagnostics []Diagnostic, readModel readModelInfo) []Diagnostic {
	switch readModel.Status {
	case "stale":
		return append(diagnostics, Diagnostic{
			Severity:   "warning",
			Code:       "READ_MODEL_STALE",
			Message:    "AmanPM read model is older than source PM files",
			SourcePath: readModel.Path,
		})
	case "unavailable":
		return append(diagnostics, Diagnostic{
			Severity:   "warning",
			Code:       "READ_MODEL_UNAVAILABLE",
			Message:    "AmanPM read model is unavailable; direct-file fallback is in use",
			SourcePath: readModel.Path,
		})
	default:
		return diagnostics
	}
}

func loadYAML(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		return err
	}
	return nil
}

func parseFrontmatter(path string) (map[string]any, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		return map[string]any{}, content, nil
	}
	end := strings.Index(content[4:], "\n---")
	if end == -1 {
		return nil, "", fmt.Errorf("unterminated frontmatter")
	}
	end += 4
	var frontmatter map[string]any
	if err := yaml.Unmarshal([]byte(content[4:end]), &frontmatter); err != nil {
		return nil, "", err
	}
	body := strings.TrimLeft(content[end+4:], "\n")
	if frontmatter == nil {
		frontmatter = map[string]any{}
	}
	return frontmatter, body, nil
}

func markdownTitle(content, fallback string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return fallback
}

func normalizeItemTitle(id, title string) string {
	prefix := id + ":"
	if strings.HasPrefix(title, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(title, prefix))
	}
	return itemTitlePrefixPattern.ReplaceAllString(title, "")
}

func markdownMetadata(content, key string) string {
	prefix := "**" + key + ":**"
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func parseDecisionRefs(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "none") || strings.EqualFold(value, "n/a") {
		return nil
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';'
	})
	refs := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			refs = append(refs, part)
		}
	}
	return refs
}

func isInactiveDecision(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	return status == "superseded" || status == "deprecated" || status == "obsolete"
}

func hasAcceptanceCriteria(body, heading string) bool {
	if heading == "" {
		heading = "## Acceptance Criteria"
	}
	inSection := false
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == heading {
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(line, "## ") {
			return false
		}
		trimmed := strings.TrimSpace(line)
		if inSection && (strings.HasPrefix(trimmed, "- [ ] ") || strings.HasPrefix(trimmed, "- [x] ") || strings.HasPrefix(trimmed, "- [X] ")) {
			return true
		}
	}
	return false
}

func flattenSprintItems(items []map[string]any) []sprintItem {
	out := make([]sprintItem, 0)
	var walk func([]map[string]any)
	walk = func(rows []map[string]any) {
		for _, row := range rows {
			id := scalarString(row["id"])
			if id == "" {
				continue
			}
			out = append(out, sprintItem{ID: id, Status: scalarString(row["status"])})
			children, ok := row["tasks"].([]any)
			if !ok {
				continue
			}
			childRows := make([]map[string]any, 0, len(children))
			for _, child := range children {
				if childMap, ok := child.(map[string]any); ok {
					childRows = append(childRows, childMap)
				}
			}
			walk(childRows)
		}
	}
	walk(items)
	return out
}

func newViolation(policyID, severity, sourcePath, message, hint string) ViolationRecord {
	return ViolationRecord{
		PolicyID:   policyID,
		Severity:   severity,
		SourcePath: sourcePath,
		Message:    message,
		Hint:       hint,
		Blocking:   severity == "error" || severity == "critical",
	}
}

func scalarString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func asMap(value any) map[string]any {
	if out, ok := value.(map[string]any); ok {
		return out
	}
	return map[string]any{}
}

func asInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		i, _ := strconv.Atoi(v)
		return i
	default:
		return 0
	}
}

func stringSliceFromAny(value any) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		return []string{v}
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, scalarString(item))
		}
		return out
	default:
		return []string{scalarString(v)}
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func countBy(items []pmItem, key func(pmItem) string) map[string]int {
	counts := make(map[string]int)
	for _, item := range items {
		value := key(item)
		if value != "" {
			counts[value]++
		}
	}
	return counts
}

func countOpenItems(items []pmItem, terminal map[string]bool) int {
	count := 0
	for _, item := range items {
		if !terminal[item.Status] {
			count++
		}
	}
	return count
}

func activeBreakdown(items []pmItem, terminal map[string]bool) map[string]int {
	counts := make(map[string]int)
	for _, item := range items {
		if terminal[item.Status] {
			continue
		}
		counts[item.Type+"_open"]++
	}
	return counts
}

func highestIDs(items []pmItem) map[string]map[string]int {
	highest := make(map[string]map[string]int)
	for _, item := range items {
		if highest[item.Type] == nil {
			highest[item.Type] = make(map[string]int)
		}
		switch item.Type {
		case "bug", "debt", "spike":
			parts := strings.Split(item.ID, "-")
			if len(parts) < 2 {
				continue
			}
			number, err := strconv.Atoi(parts[1])
			if err == nil && number > highest[item.Type][""] {
				highest[item.Type][""] = number
			}
		}
	}
	return highest
}

func hasBlockingViolations(violations []ViolationRecord) bool {
	return blockingViolationCount(violations) > 0
}

func blockingViolationCount(violations []ViolationRecord) int {
	count := 0
	for _, violation := range violations {
		if violation.Blocking {
			count++
		}
	}
	return count
}

func complianceStatus(status ValidationStatus, violations []ViolationRecord) string {
	if status == ValidationOK && len(violations) > 0 {
		return "ok_with_advisory_violations"
	}
	return string(status)
}

func authorityLevel(status ValidationStatus) string {
	if status == ValidationOK {
		return "validated_file_ssot"
	}
	return "advisory_" + string(status)
}

func authorityNote(status ValidationStatus) string {
	switch status {
	case ValidationOK:
		return "Compiled resource view over validated PM source files."
	case ValidationStale:
		return "Resource is advisory because derived freshness is stale."
	case ValidationInvalid:
		return "Resource is advisory because PM validation or parity failed."
	case ValidationUnavailable:
		return "Resource is advisory because required PM sources are unavailable."
	default:
		return "Resource authority is unknown."
	}
}

func firstMatch(pattern *regexp.Regexp, value string) string {
	return pattern.FindString(value)
}
