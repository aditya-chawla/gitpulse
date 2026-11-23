package llm

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

// Anthropic calls the Claude Messages API through the official Go SDK.
type Anthropic struct {
	client anthropic.Client
	model  string
	effort anthropic.OutputConfigEffort // empty = omit the field
}

type AnthropicConfig struct {
	APIKey string
	Model  string
	// Effort is sent only when the model is known to support it (see
	// knownModels). Empty means the model's default.
	Effort string
	// Options are extra SDK options (tests point BaseURL at a fake server).
	Options []option.RequestOption
}

func NewAnthropic(cfg AnthropicConfig) *Anthropic {
	opts := append([]option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
		option.WithRequestTimeout(90 * time.Second),
		option.WithMaxRetries(2), // 429/5xx/connection errors; Run logs one call either way
	}, cfg.Options...)
	a := &Anthropic{client: anthropic.NewClient(opts...), model: cfg.Model}
	if info, ok := LookupModel(cfg.Model); ok && info.SupportsEffort && cfg.Effort != "" {
		a.effort = anthropic.OutputConfigEffort(cfg.Effort)
	}
	return a
}

func (a *Anthropic) Name() string  { return "anthropic" }
func (a *Anthropic) Model() string { return a.model }

// Complete sends one request. Thinking is left at the model's default
// (adaptive on current models); its tokens are billed as output and count
// toward MaxTokens. The model gets no tools.
func (a *Anthropic) Complete(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: int64(req.MaxTokens),
		System:    []anthropic.TextBlockParam{{Text: req.System}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(req.User))},
		OutputConfig: anthropic.OutputConfigParam{
			Effort: a.effort,
			Format: anthropic.JSONOutputFormatParam{Schema: req.Schema},
		},
	}
	resp, err := a.client.Messages.New(ctx, params)
	if err != nil {
		return ProviderResponse{}, err
	}
	out := ProviderResponse{
		InputTokens:  int(resp.Usage.InputTokens + resp.Usage.CacheCreationInputTokens + resp.Usage.CacheReadInputTokens),
		OutputTokens: int(resp.Usage.OutputTokens),
	}
	switch resp.StopReason {
	case anthropic.StopReasonRefusal:
		out.Refused = true
		return out, nil
	case anthropic.StopReasonMaxTokens:
		return out, errors.New("response truncated at max_tokens; raise LLM_MAX_OUTPUT_TOKENS")
	}
	var text strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(t.Text)
		}
	}
	out.Text = text.String()
	return out, nil
}
