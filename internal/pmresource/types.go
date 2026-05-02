package pmresource

// SchemaVersion is the first frozen PM resource envelope contract.
const SchemaVersion = "pm-resource.v1"

const (
	URIComplyState         = "pm://comply/state"
	URISubstrateCounters   = "pm://substrate/counters"
	URIParityViolations    = "pm://parity/violations"
	URIGateReadiness       = "pm://gate/readiness"
	URIBacklogOpen         = "pm://backlog/open"
	URIDecisionsActive     = "pm://decisions/active"
	URIChangelogUnreleased = "pm://changelog/unreleased"
)

// ResourceDefinition describes a registered PM resource.
type ResourceDefinition struct {
	URI         string
	Name        string
	Description string
	MIMEType    string
}

// ResourceDefinitions returns the frozen Sprint 13 PM resource inventory.
func ResourceDefinitions() []ResourceDefinition {
	return []ResourceDefinition{
		{
			URI:         URIComplyState,
			Name:        "pm_compliance_state",
			Description: "AmanPM compliance state, violations, and validation authority",
			MIMEType:    "application/json",
		},
		{
			URI:         URISubstrateCounters,
			Name:        "pm_substrate_counters",
			Description: "AmanPM item, sprint, index, and read-model counters",
			MIMEType:    "application/json",
		},
		{
			URI:         URIParityViolations,
			Name:        "pm_parity_violations",
			Description: "AmanPM direct-file, generated-index, and read-model parity diagnostics",
			MIMEType:    "application/json",
		},
		{
			URI:         URIGateReadiness,
			Name:        "pm_gate_readiness",
			Description: "AmanPM sprint and release readiness gates",
			MIMEType:    "application/json",
		},
		{
			URI:         URIBacklogOpen,
			Name:        "pm_backlog_open",
			Description: "Open AmanPM backlog items with source paths",
			MIMEType:    "application/json",
		},
		{
			URI:         URIDecisionsActive,
			Name:        "pm_decisions_active",
			Description: "Active AmanPM decisions and supersession metadata",
			MIMEType:    "application/json",
		},
		{
			URI:         URIChangelogUnreleased,
			Name:        "pm_changelog_unreleased",
			Description: "Unreleased AmanPM changelog entries",
			MIMEType:    "application/json",
		},
	}
}

// ResourceURIs returns the frozen Sprint 13 PM resource URIs.
func ResourceURIs() []string {
	definitions := ResourceDefinitions()
	uris := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		uris = append(uris, definition.URI)
	}
	return uris
}

// ValidationStatus is the top-level PM resource trust state.
type ValidationStatus string

const (
	ValidationOK          ValidationStatus = "ok"
	ValidationStale       ValidationStatus = "stale"
	ValidationInvalid     ValidationStatus = "invalid"
	ValidationUnavailable ValidationStatus = "unavailable"
)

// Envelope is the shared JSON envelope for every pm:// resource.
type Envelope struct {
	SchemaVersion string       `json:"schema_version"`
	ResourceURI   string       `json:"resource_uri"`
	GeneratedAt   string       `json:"generated_at"`
	Authority     Authority    `json:"authority"`
	SourcePaths   []string     `json:"source_paths"`
	Derivation    Derivation   `json:"derivation"`
	Validation    Validation   `json:"validation"`
	Diagnostics   []Diagnostic `json:"diagnostics"`
	Data          any          `json:"data"`
}

// Authority describes whether the compiled resource view may be treated as
// validated PM truth.
type Authority struct {
	Source        string `json:"source"`
	Level         string `json:"level"`
	Authoritative bool   `json:"authoritative"`
	Note          string `json:"note,omitempty"`
}

// Derivation describes how a resource payload was produced.
type Derivation struct {
	Method   string   `json:"method"`
	ReadOnly bool     `json:"read_only"`
	Inputs   []string `json:"inputs"`
	Notes    []string `json:"notes,omitempty"`
}

// Validation describes freshness, blockers, and status.
type Validation struct {
	Status    ValidationStatus    `json:"status"`
	CheckedAt string              `json:"checked_at"`
	Freshness Freshness           `json:"freshness"`
	Blockers  []ValidationBlocker `json:"blockers,omitempty"`
}

// Freshness surfaces source/read-model freshness without hiding stale state.
type Freshness struct {
	SourceLatestModified string `json:"source_latest_modified,omitempty"`
	ReadModelPath        string `json:"read_model_path,omitempty"`
	ReadModelModified    string `json:"read_model_modified,omitempty"`
	ReadModelStatus      string `json:"read_model_status,omitempty"`
}

// ValidationBlocker is a validation issue that prevents authoritative output.
type ValidationBlocker struct {
	Code       string `json:"code"`
	Severity   string `json:"severity"`
	Message    string `json:"message"`
	SourcePath string `json:"source_path,omitempty"`
}

// Diagnostic is a structured warning/error emitted by a resource.
type Diagnostic struct {
	Severity   string `json:"severity"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	SourcePath string `json:"source_path,omitempty"`
}

// ViolationRecord is a compliance violation.
type ViolationRecord struct {
	PolicyID   string `json:"policy_id"`
	Severity   string `json:"severity"`
	SourcePath string `json:"source_path"`
	Message    string `json:"message"`
	Hint       string `json:"hint,omitempty"`
	Blocking   bool   `json:"blocking"`
}

// ComplianceData is the pm://comply/state payload.
type ComplianceData struct {
	Status                 string            `json:"status"`
	Mode                   string            `json:"mode"`
	ViolationCount         int               `json:"violation_count"`
	BlockingViolationCount int               `json:"blocking_violation_count"`
	Violations             []ViolationRecord `json:"violations"`
	CheckedCommand         string            `json:"checked_command"`
}

// CountersData is the pm://substrate/counters payload.
type CountersData struct {
	TotalItems      int            `json:"total_items"`
	OpenItemCount   int            `json:"open_item_count"`
	ByType          map[string]int `json:"by_type"`
	ByStatus        map[string]int `json:"by_status"`
	ByPriority      map[string]int `json:"by_priority"`
	ActiveBreakdown map[string]int `json:"active_breakdown"`
	Sprint          SprintCounters `json:"sprint"`
	ReadModel       ReadModelData  `json:"read_model"`
}

// SprintCounters summarizes active sprint evidence.
type SprintCounters struct {
	ActiveSprint string         `json:"active_sprint,omitempty"`
	ItemCount    int            `json:"item_count"`
	ByStatus     map[string]int `json:"by_status"`
	SourcePath   string         `json:"source_path,omitempty"`
}

// ReadModelData summarizes read-model freshness.
type ReadModelData struct {
	Path       string `json:"path"`
	Status     string `json:"status"`
	ModifiedAt string `json:"modified_at,omitempty"`
}

// ParityData is the pm://parity/violations payload.
type ParityData struct {
	ViolationCount int               `json:"violation_count"`
	Violations     []ParityViolation `json:"violations"`
}

// ParityViolation records a direct-file/generated-view mismatch.
type ParityViolation struct {
	Check      string `json:"check"`
	SourcePath string `json:"source_path"`
	Expected   string `json:"expected"`
	Actual     string `json:"actual"`
	Severity   string `json:"severity"`
}

// ReadinessData is the pm://gate/readiness payload.
type ReadinessData struct {
	Status       string          `json:"status"`
	BlockerCount int             `json:"blocker_count"`
	Gates        []ReadinessGate `json:"gates"`
}

// ReadinessGate is a status/readiness gate.
type ReadinessGate struct {
	ID            string   `json:"id"`
	Status        string   `json:"status"`
	Blockers      []string `json:"blockers"`
	EvidencePaths []string `json:"evidence_paths"`
	CheckedAt     string   `json:"checked_at"`
}

// BacklogOpenData is the pm://backlog/open payload.
type BacklogOpenData struct {
	Count int           `json:"count"`
	Empty bool          `json:"empty"`
	Items []BacklogItem `json:"items"`
}

// BacklogItem is open backlog item metadata.
type BacklogItem struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	Priority   string `json:"priority,omitempty"`
	Parent     string `json:"parent,omitempty"`
	Sprint     string `json:"sprint,omitempty"`
	SourcePath string `json:"source_path"`
	Title      string `json:"title"`
}

// DecisionsActiveData is the pm://decisions/active payload.
type DecisionsActiveData struct {
	Count     int        `json:"count"`
	Empty     bool       `json:"empty"`
	Decisions []Decision `json:"decisions"`
}

// Decision is active decision metadata.
type Decision struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Status       string   `json:"status"`
	SourcePath   string   `json:"source_path"`
	Supersedes   []string `json:"supersedes"`
	SupersededBy []string `json:"superseded_by"`
}

// ChangelogUnreleasedData is the pm://changelog/unreleased payload.
type ChangelogUnreleasedData struct {
	Count   int              `json:"count"`
	Empty   bool             `json:"empty"`
	Entries []ChangelogEntry `json:"entries"`
}

// ChangelogEntry is a parsed unreleased changelog entry.
type ChangelogEntry struct {
	Section         string   `json:"section"`
	Text            string   `json:"text"`
	SourcePath      string   `json:"source_path"`
	Line            int      `json:"line"`
	RelatedIDs      []string `json:"related_ids,omitempty"`
	ParsedTimestamp string   `json:"parsed_timestamp,omitempty"`
}
