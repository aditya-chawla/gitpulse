package llm

// Price is USD per million tokens. Some models charge more for long prompts;
// the tier is chosen per request from its total input tokens.
type Price struct {
	InputPerMTok, OutputPerMTok         float64
	LongInputPerMTok, LongOutputPerMTok float64 // 0 = no long-prompt tier
	LongThreshold                       int     // prompts over this many input tokens use the long tier
}

// ModelInfo is what gitpulse knows about a model without calling the API.
type ModelInfo struct {
	Price Price
	// SupportsEffort: output_config.effort (incl. "low") is accepted. Only
	// sent when true, so an unknown model never gets an unsupported field.
	SupportsEffort bool
}

// knownModels holds prices verified against
// https://platform.claude.com/docs/en/about-claude/pricing (2026-11-23).
// Thinking tokens are billed as output tokens and are already included in the
// API's reported output_tokens. LLM_PRICE_* env vars override these.
var knownModels = map[string]ModelInfo{
	// "Claude Haiku 5.5 (for prompts up to 100,000 tokens)": $0.10 in / $0.50 out;
	// over 100,000 prompt tokens (cache reads and writes count): $0.50 / $2.50.
	// Effort: all five levels, default medium.
	"claude-haiku-5-5":  {Price: Price{0.10, 0.50, 0.50, 2.50, 100_000}, SupportsEffort: true},
	"claude-sonnet-5-5": {Price: Price{InputPerMTok: 2, OutputPerMTok: 10}, SupportsEffort: true},
	"claude-opus-5-5":   {Price: Price{InputPerMTok: 4, OutputPerMTok: 20}, SupportsEffort: true},
}

// LookupModel returns the built-in info for a model, if any.
func LookupModel(model string) (ModelInfo, bool) {
	m, ok := knownModels[model]
	return m, ok
}

// Cost computes the USD cost of one request from its token usage.
func (p Price) Cost(inputTokens, outputTokens int) float64 {
	in, out := p.InputPerMTok, p.OutputPerMTok
	if p.LongThreshold > 0 && inputTokens > p.LongThreshold {
		in, out = p.LongInputPerMTok, p.LongOutputPerMTok
	}
	return (float64(inputTokens)*in + float64(outputTokens)*out) / 1e6
}
