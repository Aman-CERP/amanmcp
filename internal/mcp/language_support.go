package mcp

import (
	"strings"

	"github.com/Aman-CERP/amanmcp/internal/language"
	"github.com/Aman-CERP/amanmcp/internal/store"
)

const (
	LanguageSupportTierParserBacked = "tier_1_parser_backed"
	LanguageSupportTierLineFallback = "tier_2_line_fallback"
	LanguageSupportTierPlainText    = "tier_3_plain_text"
)

func languageSupportTierForChunk(chunk *store.Chunk) string {
	if chunk == nil {
		return LanguageSupportTierPlainText
	}

	lang := strings.ToLower(strings.TrimSpace(chunk.Language))
	provenance := chunkProvenance(chunk)

	if lang != "" {
		if def, ok := language.DefaultRegistry().GetByName(lang); ok && def.ContentType == language.ContentTypeCode {
			if def.Parser != language.ParserLineFallback {
				return LanguageSupportTierParserBacked
			}
			return LanguageSupportTierLineFallback
		}
	}

	if provenance == "ast" && chunk.ContentType == store.ContentTypeCode {
		return LanguageSupportTierParserBacked
	}
	if provenance == "line_fallback" && lang != "" && !isDocumentOrPlainLanguage(lang) {
		return LanguageSupportTierLineFallback
	}

	return LanguageSupportTierPlainText
}

func isDocumentOrPlainLanguage(lang string) bool {
	switch lang {
	case "", "markdown", "md", "pdf", "text", "txt", "rst":
		return true
	default:
		return false
	}
}
