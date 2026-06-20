package search

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/store"
)

type SourceClass string

const (
	SourceClassSourceCode   SourceClass = "source_code"
	SourceClassTest         SourceClass = "test"
	SourceClassDocs         SourceClass = "docs"
	SourceClassADR          SourceClass = "adr"
	SourceClassPMItem       SourceClass = "pm_item"
	SourceClassGenerated    SourceClass = "generated"
	SourceClassArchived     SourceClass = "archived"
	SourceClassReviewCorpus SourceClass = "review_corpus"
	SourceClassConfig       SourceClass = "config"
	SourceClassRawEvidence  SourceClass = "raw_evidence"
	SourceClassUnknown      SourceClass = "unknown"
)

type Authority string

const (
	AuthorityAuthoritative Authority = "authoritative"
	AuthorityActive        Authority = "active"
	AuthorityGenerated     Authority = "generated"
	AuthorityArchived      Authority = "archived"
	AuthorityAdvisory      Authority = "advisory"
	AuthorityRawEvidence   Authority = "raw_evidence"
	AuthorityUnknown       Authority = "unknown"
)

type Profile string

const (
	ProfileCode          Profile = "code"
	ProfileProjectMemory Profile = "project-memory"
	ProfileReviewCorpus  Profile = "review-corpus"
	ProfileArchive       Profile = "archive"
)

func ParseProfile(value string) (Profile, error) {
	profile := Profile(strings.TrimSpace(value))
	switch profile {
	case "", ProfileCode, ProfileProjectMemory, ProfileReviewCorpus, ProfileArchive:
		return profile, nil
	default:
		return "", fmt.Errorf("unknown search profile %q; use one of: code, project-memory, review-corpus, archive", value)
	}
}

type DecisionStatus string

const (
	DecisionStatusProposed    DecisionStatus = "proposed"
	DecisionStatusAccepted    DecisionStatus = "accepted"
	DecisionStatusImplemented DecisionStatus = "implemented"
	DecisionStatusDeprecated  DecisionStatus = "deprecated"
	DecisionStatusSuperseded  DecisionStatus = "superseded"
	DecisionStatusUnknown     DecisionStatus = "unknown"
)

type SourceMetadata struct {
	SourceClass     SourceClass
	Authority       Authority
	Profile         Profile
	SourcePath      string
	LastModified    *time.Time
	GitStatus       string
	SourceHash      string
	Generated       bool
	Stale           bool
	FreshnessReason string
	DecisionStatus  DecisionStatus
	Supersedes      []string
	SupersededBy    []string
	CurrentAsOf     *time.Time
	Indexable       bool
}

type SourceMetadataInput struct {
	Path         string
	ContentType  store.ContentType
	Language     string
	Content      string
	Metadata     map[string]string
	LastModified time.Time
	GitStatus    string
	SourceHash   string
	Generated    bool
	Stale        bool
	Indexable    *bool
	Rules        MetadataRules
}

type MetadataRules struct {
	Rules []MetadataRule
}

type MetadataRule struct {
	Pattern     string
	SourceClass SourceClass
	Authority   Authority
	Profile     Profile
	Generated   bool
	Stale       bool
}

type ProfileEligibility struct {
	Eligible         bool
	RequestedProfile Profile
	RequiredProfile  Profile
	Reason           string
	Action           string
}

type ProfileMismatch struct {
	SourcePath       string
	RequestedProfile Profile
	RequiredProfile  Profile
	SourceClass      SourceClass
	Authority        Authority
	Reason           string
	Action           string
}

type ProfileRule struct {
	Include              []string
	Exclude              []string
	SourceClasses        []SourceClass
	ExcludeSourceClasses []SourceClass
	Authorities          []Authority
	ExcludeAuthorities   []Authority
}

type ProfileRules struct {
	Profiles map[Profile]ProfileRule
}

var adrRefPattern = regexp.MustCompile(`(?i)\bADR-\d+\b`)

func DefaultProfileRules() ProfileRules {
	return ProfileRules{Profiles: map[Profile]ProfileRule{
		ProfileCode: {
			Include:              []string{"cmd/**", "internal/**", "pkg/**", "configs/**", ".amanmcp.yaml", ".gitignore"},
			SourceClasses:        []SourceClass{SourceClassSourceCode, SourceClassTest, SourceClassConfig},
			ExcludeSourceClasses: []SourceClass{SourceClassReviewCorpus, SourceClassArchived, SourceClassRawEvidence},
		},
		ProfileProjectMemory: {
			Include: []string{
				"README.md",
				"docs/**",
				".aman-pm/product/**",
				".aman-pm/backlog/**",
				".aman-pm/sprints/active/**",
				".aman-pm/decisions/**",
			},
			Exclude:              []string{"vend_feedback/**", "improvements_dump/**", "archive/**", "**/*.log"},
			SourceClasses:        []SourceClass{SourceClassDocs, SourceClassADR, SourceClassPMItem, SourceClassGenerated, SourceClassConfig, SourceClassUnknown},
			ExcludeSourceClasses: []SourceClass{SourceClassReviewCorpus, SourceClassArchived, SourceClassRawEvidence},
		},
		ProfileReviewCorpus: {
			Include:       []string{"vend_feedback/**", "improvements_dump/**"},
			SourceClasses: []SourceClass{SourceClassReviewCorpus, SourceClassRawEvidence},
			Authorities:   []Authority{AuthorityAdvisory, AuthorityRawEvidence},
		},
		ProfileArchive: {
			Include:       []string{"archive/**"},
			SourceClasses: []SourceClass{SourceClassArchived},
			Authorities:   []Authority{AuthorityArchived},
		},
	}}
}

func DefaultMetadataRules() MetadataRules {
	return MetadataRules{Rules: []MetadataRule{
		{Pattern: "vend_feedback/**", SourceClass: SourceClassReviewCorpus, Authority: AuthorityAdvisory, Profile: ProfileReviewCorpus},
		{Pattern: "improvements_dump/**", SourceClass: SourceClassReviewCorpus, Authority: AuthorityAdvisory, Profile: ProfileReviewCorpus},
		{Pattern: "archive/**", SourceClass: SourceClassArchived, Authority: AuthorityArchived, Profile: ProfileArchive, Stale: true},
		{Pattern: "**/*.log", SourceClass: SourceClassRawEvidence, Authority: AuthorityRawEvidence, Profile: ProfileReviewCorpus},
		{Pattern: ".aman-pm/validation/**", SourceClass: SourceClassGenerated, Authority: AuthorityGenerated, Profile: ProfileProjectMemory, Generated: true},
		{Pattern: ".aman-pm/decisions/ADR-*.md", SourceClass: SourceClassADR, Authority: AuthorityUnknown, Profile: ProfileProjectMemory},
		{Pattern: "docs/reference/decisions/ADR-*.md", SourceClass: SourceClassADR, Authority: AuthorityUnknown, Profile: ProfileProjectMemory},
		{Pattern: ".aman-pm/backlog/**", SourceClass: SourceClassPMItem, Authority: AuthorityActive, Profile: ProfileProjectMemory},
		{Pattern: ".aman-pm/product/**", SourceClass: SourceClassPMItem, Authority: AuthorityActive, Profile: ProfileProjectMemory},
		{Pattern: ".aman-pm/sprints/active/**", SourceClass: SourceClassPMItem, Authority: AuthorityActive, Profile: ProfileProjectMemory},
		{Pattern: ".amanmcp.yaml", SourceClass: SourceClassConfig, Authority: AuthorityAuthoritative, Profile: ProfileCode},
		{Pattern: ".gitignore", SourceClass: SourceClassConfig, Authority: AuthorityAuthoritative, Profile: ProfileCode},
		{Pattern: "go.mod", SourceClass: SourceClassConfig, Authority: AuthorityAuthoritative, Profile: ProfileCode},
		{Pattern: "Makefile", SourceClass: SourceClassConfig, Authority: AuthorityAuthoritative, Profile: ProfileCode},
		{Pattern: "configs/**", SourceClass: SourceClassConfig, Authority: AuthorityActive, Profile: ProfileCode},
		{Pattern: "docs/**", SourceClass: SourceClassDocs, Authority: AuthorityActive, Profile: ProfileProjectMemory},
		{Pattern: "README.md", SourceClass: SourceClassDocs, Authority: AuthorityActive, Profile: ProfileProjectMemory},
	}}
}

// DecisionScopePrefixes returns path prefixes that metadata rules classify as
// ADR sources. Callers use this to widen decision lookup recall without
// duplicating path conventions outside the metadata classifier.
func DecisionScopePrefixes(rules MetadataRules) []string {
	if len(rules.Rules) == 0 {
		rules = DefaultMetadataRules()
	}

	seen := make(map[string]struct{})
	scopes := make([]string, 0)
	for _, rule := range rules.Rules {
		if rule.SourceClass != SourceClassADR {
			continue
		}
		scope := scopePrefixFromPattern(rule.Pattern)
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	return scopes
}

func scopePrefixFromPattern(pattern string) string {
	pattern = NormalizeSourcePath(pattern)
	if pattern == "" {
		return ""
	}

	metaIndex := len(pattern)
	for _, token := range []string{"**", "*", "?"} {
		if idx := strings.Index(pattern, token); idx >= 0 && idx < metaIndex {
			metaIndex = idx
		}
	}

	if metaIndex < len(pattern) {
		prefix := pattern[:metaIndex]
		if slash := strings.LastIndex(prefix, "/"); slash >= 0 {
			return strings.Trim(prefix[:slash], "/")
		}
		return ""
	}

	if dot := strings.LastIndex(pattern, "."); dot > strings.LastIndex(pattern, "/") {
		if slash := strings.LastIndex(pattern, "/"); slash >= 0 {
			return strings.Trim(pattern[:slash], "/")
		}
		return ""
	}

	return strings.Trim(pattern, "/")
}

func DeriveSourceMetadata(input SourceMetadataInput) SourceMetadata {
	path := NormalizeSourcePath(input.Path)
	indexable := true
	if input.Indexable != nil {
		indexable = *input.Indexable
	}

	meta := SourceMetadata{
		SourceClass: SourceClassUnknown,
		Authority:   AuthorityUnknown,
		Profile:     ProfileProjectMemory,
		SourcePath:  path,
		GitStatus:   metadataValue(input.Metadata, "git_status", input.GitStatus),
		SourceHash:  metadataValue(input.Metadata, "source_hash", input.SourceHash),
		Generated:   input.Generated,
		Stale:       input.Stale,
		Indexable:   indexable,
	}
	if meta.SourceHash == "" {
		meta.SourceHash = metadataValue(input.Metadata, "content_hash", "")
	}
	if !input.LastModified.IsZero() {
		modified := input.LastModified
		meta.LastModified = &modified
		meta.CurrentAsOf = &modified
	}

	rules := input.Rules
	if len(rules.Rules) == 0 {
		rules = DefaultMetadataRules()
	}
	applyPathRules(&meta, rules, path)
	applyContentFallbacks(&meta, input, path)
	applyFrontmatterMetadata(&meta, input.Metadata)
	applyDecisionMetadata(&meta, input.Content)
	applyFreshnessDefaults(&meta)

	return meta
}

func SourceMetadataFromChunk(chunk *store.Chunk) SourceMetadata {
	return SourceMetadataFromChunkWithRules(chunk, DefaultMetadataRules())
}

func SourceMetadataFromChunkWithRules(chunk *store.Chunk, rules MetadataRules) SourceMetadata {
	if chunk == nil {
		return SourceMetadata{
			SourceClass:     SourceClassUnknown,
			Authority:       AuthorityUnknown,
			Profile:         ProfileProjectMemory,
			Indexable:       true,
			FreshnessReason: "missing chunk metadata",
		}
	}

	content := chunk.Content
	if content == "" {
		content = chunk.RawContent
	}

	return DeriveSourceMetadata(SourceMetadataInput{
		Path:         chunk.FilePath,
		ContentType:  chunk.ContentType,
		Language:     chunk.Language,
		Content:      content,
		Metadata:     chunk.Metadata,
		LastModified: chunk.UpdatedAt,
		Rules:        rules,
	})
}

func NormalizeSourcePath(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	return strings.Trim(path, "/")
}

func MetadataPriority(meta SourceMetadata) int {
	score := map[Authority]int{
		AuthorityAuthoritative: 100,
		AuthorityActive:        80,
		AuthorityGenerated:     45,
		AuthorityAdvisory:      35,
		AuthorityRawEvidence:   25,
		AuthorityArchived:      20,
		AuthorityUnknown:       10,
	}[meta.Authority]

	switch meta.DecisionStatus {
	case DecisionStatusAccepted, DecisionStatusImplemented:
		score += 10
	case DecisionStatusProposed:
		score -= 10
	case DecisionStatusDeprecated, DecisionStatusSuperseded:
		score -= 30
	case DecisionStatusUnknown:
		if meta.SourceClass == SourceClassADR {
			score -= 5
		}
	}

	if meta.Generated {
		score -= 5
	}
	if meta.Stale {
		score -= 10
	}
	return score
}

func ExplainProfileEligibility(meta SourceMetadata, requested Profile) ProfileEligibility {
	return ExplainProfileEligibilityWithRules(meta, requested, DefaultProfileRules())
}

func ExplainProfileEligibilityWithRules(meta SourceMetadata, requested Profile, rules ProfileRules) ProfileEligibility {
	if len(rules.Profiles) == 0 {
		rules = DefaultProfileRules()
	}
	if !meta.Indexable {
		return ProfileEligibility{
			Eligible:         false,
			RequestedProfile: requested,
			RequiredProfile:  meta.Profile,
			Reason:           "path is not indexable",
			Action:           "Check .gitignore and .amanmcp.yaml exclusions; profiles do not re-include excluded paths.",
		}
	}

	if requested == "" {
		required := requiredProfileForWithRules(meta, rules)
		if isDefaultExcluded(meta) || required == ProfileReviewCorpus || required == ProfileArchive {
			return profileMismatchEligibilityWithRequired(meta, requested, required)
		}
		return ProfileEligibility{Eligible: true, RequestedProfile: requested, RequiredProfile: meta.Profile}
	}

	if rule, ok := rules.Profiles[requested]; ok && profileRuleAllows(meta, rule) {
		return ProfileEligibility{Eligible: true, RequestedProfile: requested, RequiredProfile: requested}
	}

	return profileMismatchEligibilityWithRequired(meta, requested, requiredProfileForWithRules(meta, rules))
}

func ApplyProfileEligibility(results []*SearchResult, opts SearchOptions) ([]*SearchResult, []ProfileMismatch) {
	if len(results) == 0 {
		return results, nil
	}

	filtered := make([]*SearchResult, 0, len(results))
	mismatches := make([]ProfileMismatch, 0)
	for _, result := range results {
		if result == nil {
			continue
		}
		ensureResultMetadata(result)
		eligibility := ExplainProfileEligibilityWithRules(result.SourceMetadata, opts.Profile, opts.ProfileRules)
		if eligibility.Eligible {
			filtered = append(filtered, result)
			continue
		}
		mismatches = append(mismatches, ProfileMismatch{
			SourcePath:       result.SourceMetadata.SourcePath,
			RequestedProfile: opts.Profile,
			RequiredProfile:  eligibility.RequiredProfile,
			SourceClass:      result.SourceMetadata.SourceClass,
			Authority:        result.SourceMetadata.Authority,
			Reason:           eligibility.Reason,
			Action:           eligibility.Action,
		})
	}
	return filtered, mismatches
}

func ensureResultMetadata(result *SearchResult) {
	if result == nil {
		return
	}
	if result.SourceMetadata.SourceClass != "" {
		return
	}
	result.SourceMetadata = SourceMetadataFromChunk(result.Chunk)
}

func applyPathRules(meta *SourceMetadata, rules MetadataRules, path string) {
	for _, rule := range rules.Rules {
		if !matchMetadataPattern(rule.Pattern, path) {
			continue
		}
		meta.SourceClass = rule.SourceClass
		meta.Authority = rule.Authority
		meta.Profile = rule.Profile
		meta.Generated = meta.Generated || rule.Generated
		meta.Stale = meta.Stale || rule.Stale
		return
	}
}

func applyContentFallbacks(meta *SourceMetadata, input SourceMetadataInput, path string) {
	if meta.SourceClass != SourceClassUnknown {
		return
	}

	switch {
	case IsTestFile(path):
		meta.SourceClass = SourceClassTest
		meta.Authority = AuthorityActive
		meta.Profile = ProfileCode
	case input.ContentType == store.ContentTypeCode || isCodePath(path):
		meta.SourceClass = SourceClassSourceCode
		meta.Authority = AuthorityActive
		meta.Profile = ProfileCode
	case isConfigPath(path):
		meta.SourceClass = SourceClassConfig
		meta.Authority = AuthorityAuthoritative
		meta.Profile = ProfileCode
	case input.ContentType == store.ContentTypeMarkdown || input.ContentType == store.ContentTypePDF || input.ContentType == store.ContentTypeText || isDocPath(path):
		meta.SourceClass = SourceClassDocs
		meta.Authority = AuthorityActive
		meta.Profile = ProfileProjectMemory
	}
}

func applyFrontmatterMetadata(meta *SourceMetadata, metadata map[string]string) {
	if len(metadata) == 0 {
		return
	}

	itemType := strings.ToLower(strings.TrimSpace(metadata["fm.type"]))
	status := strings.ToLower(strings.TrimSpace(metadata["fm.status"]))
	priority := strings.ToUpper(strings.TrimSpace(metadata["fm.priority"]))

	if isPMFrontmatterType(itemType) {
		meta.SourceClass = SourceClassPMItem
		meta.Authority = AuthorityActive
		meta.Profile = ProfileProjectMemory
	}

	switch status {
	case "resolved", "done", "closed", "cancelled", "canceled":
		meta.SourceClass = SourceClassArchived
		meta.Authority = AuthorityArchived
		meta.Profile = ProfileArchive
		meta.Stale = true
		if meta.FreshnessReason == "" {
			meta.FreshnessReason = "frontmatter status is " + status
		}
	case "active", "in_progress", "ready", "blocked":
		if meta.Authority == AuthorityUnknown {
			meta.Authority = AuthorityActive
		}
		if priority == "P0" && status == "active" {
			meta.Authority = AuthorityAuthoritative
		}
	}
}

func isPMFrontmatterType(itemType string) bool {
	switch itemType {
	case "bug", "feature", "task", "debt", "epic", "spike":
		return true
	default:
		return false
	}
}

func applyDecisionMetadata(meta *SourceMetadata, content string) {
	if meta.SourceClass != SourceClassADR && !strings.Contains(strings.ToUpper(meta.SourcePath), "ADR-") {
		return
	}

	status := parseDecisionStatus(content)
	meta.DecisionStatus = status
	meta.Supersedes = parseADRRefs(content, "supersedes")
	meta.SupersededBy = parseADRRefs(content, "superseded_by")
	if len(meta.SupersededBy) == 0 {
		meta.SupersededBy = parseADRRefs(content, "superseded-by")
	}
	if len(meta.SupersededBy) == 0 {
		meta.SupersededBy = parseSupersededByFromStatus(content)
	}
	if len(meta.SupersededBy) > 0 {
		status = DecisionStatusSuperseded
		meta.DecisionStatus = DecisionStatusSuperseded
	}

	if meta.SourceClass == SourceClassReviewCorpus || meta.SourceClass == SourceClassGenerated || meta.SourceClass == SourceClassArchived {
		if status == "" {
			meta.DecisionStatus = DecisionStatusUnknown
		}
		return
	}

	meta.SourceClass = SourceClassADR
	meta.Profile = ProfileProjectMemory
	switch status {
	case DecisionStatusAccepted, DecisionStatusImplemented:
		meta.Authority = AuthorityAuthoritative
	case DecisionStatusDeprecated, DecisionStatusSuperseded:
		meta.Authority = AuthorityArchived
		meta.Profile = ProfileArchive
		meta.Stale = true
		if meta.FreshnessReason == "" {
			meta.FreshnessReason = "superseded ADR"
		}
	case DecisionStatusProposed:
		meta.Authority = AuthorityActive
	case DecisionStatusUnknown, "":
		meta.DecisionStatus = DecisionStatusUnknown
		meta.Authority = AuthorityUnknown
		if meta.FreshnessReason == "" {
			meta.FreshnessReason = "missing ADR status"
		}
	}
}

func applyFreshnessDefaults(meta *SourceMetadata) {
	if meta.SourceClass == SourceClassUnknown && meta.FreshnessReason == "" {
		meta.FreshnessReason = "unknown source classification"
	}
	if meta.Generated && meta.SourceHash == "" && meta.FreshnessReason == "" {
		meta.FreshnessReason = "generated source hash unavailable"
	}
	if meta.DecisionStatus == "" && meta.SourceClass == SourceClassADR {
		meta.DecisionStatus = DecisionStatusUnknown
	}
}

func metadataValue(values map[string]string, key, fallback string) string {
	if values == nil {
		return fallback
	}
	if value := values[key]; value != "" {
		return value
	}
	return fallback
}

func isDefaultExcluded(meta SourceMetadata) bool {
	return meta.SourceClass == SourceClassReviewCorpus ||
		meta.SourceClass == SourceClassArchived ||
		meta.SourceClass == SourceClassRawEvidence ||
		meta.Profile == ProfileReviewCorpus ||
		meta.Profile == ProfileArchive ||
		meta.Authority == AuthorityRawEvidence ||
		meta.Authority == AuthorityAdvisory
}

func profileMismatchEligibilityWithRequired(meta SourceMetadata, requested, required Profile) ProfileEligibility {
	if required == "" {
		required = meta.Profile
	}
	if required == "" {
		required = requiredProfileForWithRules(meta, DefaultProfileRules())
	}
	return ProfileEligibility{
		Eligible:         false,
		RequestedProfile: requested,
		RequiredProfile:  required,
		Reason:           "result excluded by selected search profile",
		Action:           "Select profile " + string(required) + " or narrow the query scope to inspect this source.",
	}
}

func requiredProfileForWithRules(meta SourceMetadata, rules ProfileRules) Profile {
	if len(rules.Profiles) == 0 {
		rules = DefaultProfileRules()
	}
	for _, profile := range []Profile{ProfileReviewCorpus, ProfileArchive, ProfileCode, ProfileProjectMemory} {
		rule, ok := rules.Profiles[profile]
		if !ok {
			continue
		}
		if pathExplicitlyIncluded(meta.SourcePath, rule) && !pathExplicitlyExcluded(meta.SourcePath, rule) {
			return profile
		}
	}
	if meta.Profile != "" {
		return meta.Profile
	}
	switch {
	case meta.SourceClass == SourceClassReviewCorpus || meta.SourceClass == SourceClassRawEvidence:
		return ProfileReviewCorpus
	case meta.SourceClass == SourceClassArchived || meta.Authority == AuthorityArchived:
		return ProfileArchive
	case meta.SourceClass == SourceClassSourceCode || meta.SourceClass == SourceClassTest:
		return ProfileCode
	default:
		return ProfileProjectMemory
	}
}

func profileRuleAllows(meta SourceMetadata, rule ProfileRule) bool {
	if pathExplicitlyExcluded(meta.SourcePath, rule) ||
		containsSourceClass(rule.ExcludeSourceClasses, meta.SourceClass) ||
		containsAuthority(rule.ExcludeAuthorities, meta.Authority) {
		return false
	}

	classAllowed := len(rule.SourceClasses) == 0 || containsSourceClass(rule.SourceClasses, meta.SourceClass)
	authorityAllowed := len(rule.Authorities) == 0 || containsAuthority(rule.Authorities, meta.Authority)
	if pathExplicitlyIncluded(meta.SourcePath, rule) {
		return classAllowed && authorityAllowed
	}

	return classAllowed && authorityAllowed
}

func pathExplicitlyIncluded(path string, rule ProfileRule) bool {
	return matchesAnyMetadataPattern(rule.Include, path)
}

func pathExplicitlyExcluded(path string, rule ProfileRule) bool {
	return matchesAnyMetadataPattern(rule.Exclude, path)
}

func matchesAnyMetadataPattern(patterns []string, path string) bool {
	for _, pattern := range patterns {
		if matchMetadataPattern(pattern, path) {
			return true
		}
	}
	return false
}

func containsSourceClass(values []SourceClass, target SourceClass) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsAuthority(values []Authority, target Authority) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func matchMetadataPattern(pattern, path string) bool {
	pattern = NormalizeSourcePath(pattern)
	path = NormalizeSourcePath(path)
	switch {
	case pattern == "":
		return false
	case pattern == path:
		return true
	case strings.HasSuffix(pattern, "/**"):
		prefix := strings.TrimSuffix(pattern, "/**")
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	case strings.HasPrefix(pattern, "**/*"):
		return strings.HasSuffix(path, strings.TrimPrefix(pattern, "**/*"))
	case strings.Contains(pattern, "*"):
		return wildcardMatch(pattern, path)
	default:
		return false
	}
}

func wildcardMatch(pattern, path string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == path
	}
	if !strings.HasPrefix(path, parts[0]) {
		return false
	}
	pos := len(parts[0])
	for _, part := range parts[1 : len(parts)-1] {
		idx := strings.Index(path[pos:], part)
		if idx < 0 {
			return false
		}
		pos += idx + len(part)
	}
	return strings.HasSuffix(path, parts[len(parts)-1])
}

func isCodePath(path string) bool {
	codeExts := []string{".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs", ".java", ".kt", ".c", ".cpp", ".h", ".hpp", ".rb", ".php", ".swift", ".sh"}
	for _, ext := range codeExts {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

func isDocPath(path string) bool {
	return strings.HasSuffix(path, ".md") || strings.HasSuffix(path, ".txt") || strings.HasSuffix(path, ".rst")
}

func isConfigPath(path string) bool {
	switch {
	case strings.HasSuffix(path, ".yaml"), strings.HasSuffix(path, ".yml"), strings.HasSuffix(path, ".toml"), strings.HasSuffix(path, ".json"):
		return true
	case strings.HasPrefix(path, ".github/"):
		return true
	}
	return false
}

func parseDecisionStatus(content string) DecisionStatus {
	content = strings.ToLower(content)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.Trim(line, "*"))
		if !strings.Contains(line, "status") {
			continue
		}
		switch {
		case strings.Contains(line, "implemented"):
			return DecisionStatusImplemented
		case strings.Contains(line, "accepted"):
			return DecisionStatusAccepted
		case strings.Contains(line, "superseded"):
			return DecisionStatusSuperseded
		case strings.Contains(line, "deprecated"):
			return DecisionStatusDeprecated
		case strings.Contains(line, "proposed"):
			return DecisionStatusProposed
		}
	}
	return DecisionStatusUnknown
}

func parseADRRefs(content, field string) []string {
	lines := strings.Split(content, "\n")
	field = strings.ToLower(field)
	refs := make([]string, 0)
	capturingList := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, field+":") {
			refs = append(refs, adrRefPattern.FindAllString(trimmed, -1)...)
			capturingList = strings.TrimSpace(strings.TrimPrefix(trimmed, field+":")) == ""
			continue
		}
		if capturingList {
			if strings.HasPrefix(trimmed, "-") {
				refs = append(refs, adrRefPattern.FindAllString(trimmed, -1)...)
				continue
			}
			if trimmed != "" {
				capturingList = false
			}
		}
	}
	return uniqueStrings(refs)
}

func parseSupersededByFromStatus(content string) []string {
	var refs []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(strings.Trim(line, "*"))
		lower := strings.ToLower(trimmed)
		if !strings.Contains(lower, "status") || !strings.Contains(lower, "superseded") || !strings.Contains(lower, " by ") {
			continue
		}
		refs = append(refs, adrRefPattern.FindAllString(trimmed, -1)...)
	}
	return uniqueStrings(refs)
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := strings.ToUpper(value)
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}
