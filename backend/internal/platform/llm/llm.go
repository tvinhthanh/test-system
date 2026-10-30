// Package llm is the one client for a large language model (QĐ-15):
// Anthropic's Messages API through the official Go SDK, to one host fixed
// by configuration (api.anthropic.com unless LLM_BASE_URL says otherwise —
// tests and an egress proxy). It knows nothing about test cases: a system
// prompt, a user message and a JSON schema go in; JSON that matches the
// schema comes out (structured outputs, `output_config.format`).
//
// The API key travels per call: an organization's own key, or the server's.
// Errors are classified so the caller can tell a tester something true —
// key rejected, model unavailable, rate limited, refused, or unreachable.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
)

// DefaultModel is used when nothing else is chosen.
const DefaultModel = "claude-opus-5-5"

// Model is one choice offered to an organization; the list is the
// allowlist — a model id that is not here is refused before any call.
type Model struct {
	ID    string
	Label string
	Note  string
}

var Models = []Model{
	{ID: "claude-opus-5-5", Label: "Claude Opus 5.5", Note: "Default. Best drafts for most projects."},
	{ID: "claude-sonnet-5-5", Label: "Claude Sonnet 5.5", Note: "Faster and about half the price of Opus."},
	{ID: "claude-haiku-5-5", Label: "Claude Haiku 5.5", Note: "Cheapest and fastest; simpler drafts."},
	{ID: "claude-fable-5-1", Label: "Claude Fable 5.1", Note: "Most capable, and the most expensive."},
}

// ValidModel reports whether id is in Models.
func ValidModel(id string) bool {
	for _, m := range Models {
		if m.ID == id {
			return true
		}
	}
	return false
}

var (
	// ErrKey: the API key was rejected (401/403).
	ErrKey = errors.New("llm: API key rejected")
	// ErrModel: the key cannot use that model (404), or it is not offered.
	ErrModel = errors.New("llm: model unavailable")
	// ErrRateLimited: 429 or 529 after the SDK's own retries.
	ErrRateLimited = errors.New("llm: rate limited or overloaded")
	// ErrRefused: the model declined (stop_reason refusal).
	ErrRefused = errors.New("llm: request refused")
	// ErrTruncated: the answer hit max_tokens before the JSON was complete.
	ErrTruncated = errors.New("llm: answer cut off")
	// ErrUnavailable: transport errors, 5xx, or an answer that is not JSON.
	ErrUnavailable = errors.New("llm: service unavailable")
)

// Request is one structured call.
type Request struct {
	APIKey    string
	Model     string
	System    string
	User      string
	Schema    map[string]any // JSON schema; every object needs additionalProperties: false
	MaxTokens int64          // 0 = 16000
}

// Result is the schema-valid JSON and what it cost. Model is the model that
// wrote it: after a refusal fallback, the fallback model, and the token
// counts are that attempt's (the API's top-level usage). The declined attempt
// is only in usage.iterations; it is billed when it streamed output or its
// refusal category is a billed one.
type Result struct {
	JSON         json.RawMessage
	Model        string
	InputTokens  int64
	OutputTokens int64
}

// ServedBy is the model to record with the token counts: Model, or
// requested when the answer did not name one.
func (r Result) ServedBy(requested string) string {
	if r.Model != "" {
		return r.Model
	}
	return requested
}

type Client struct {
	baseURL string
	timeout time.Duration
}

// New returns a client; baseURL "" is the Anthropic API. A call may take
// up to ten minutes: answers are streamed (runnable test cases, QĐ-16, run
// to tens of thousands of tokens), and the limit bounds the whole stream.
func New(baseURL string) *Client {
	return &Client{baseURL: strings.TrimSuffix(baseURL, "/"), timeout: 10 * time.Minute}
}

func (c *Client) sdk(apiKey string) anthropic.Client {
	opts := []option.RequestOption{option.WithAPIKey(apiKey), option.WithMaxRetries(2), option.WithRequestTimeout(c.timeout)}
	if c.baseURL != "" {
		opts = append(opts, option.WithBaseURL(c.baseURL))
	}
	return anthropic.NewClient(opts...)
}

// fallbackModels get the server-side refusal fallback (beta
// server-side-fallback-2026-07-01, "default" routing): a declined request
// is re-served by the API inside the same call. Claude Haiku 5.5 has none.
var fallbackModels = map[string]bool{"claude-opus-5-5": true, "claude-sonnet-5-5": true, "claude-fable-5-1": true}

// JSON makes one streamed call, accumulates it, and returns the structured
// answer. Streaming keeps a long answer from idling the connection until a
// proxy cuts it. Thinking is left at each model's default (adaptive; always
// on for Opus 5.5 and Fable 5.1); effort is medium, set explicitly because
// the models' defaults differ.
func (c *Client) JSON(ctx context.Context, req Request) (Result, error) {
	if !ValidModel(req.Model) {
		return Result{}, fmt.Errorf("%w: %q is not offered", ErrModel, req.Model)
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = 16000
	}
	params := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: req.MaxTokens,
		System:    []anthropic.BetaTextBlockParam{{Text: req.System}},
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(req.User))},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: anthropic.BetaOutputConfigEffortMedium,
			Format: anthropic.BetaJSONOutputFormatParam{Schema: req.Schema},
		},
	}
	if fallbackModels[req.Model] {
		params.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
		params.Fallbacks = anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()}
	}
	client := c.sdk(req.APIKey)
	stream := client.Beta.Messages.NewStreaming(ctx, params)
	msg := anthropic.BetaMessage{}
	for stream.Next() {
		if err := msg.Accumulate(stream.Current()); err != nil {
			_ = stream.Close()
			return Result{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
	}
	if err := stream.Err(); err != nil {
		return Result{}, classify(err)
	}
	switch msg.StopReason {
	case anthropic.BetaStopReasonRefusal:
		return Result{}, ErrRefused
	case anthropic.BetaStopReasonMaxTokens:
		return Result{}, ErrTruncated
	}
	var text strings.Builder
	for _, block := range msg.Content {
		if b, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			text.WriteString(b.Text)
		}
	}
	out := json.RawMessage(text.String())
	if !json.Valid(out) {
		return Result{}, fmt.Errorf("%w: the answer is not JSON", ErrUnavailable)
	}
	return Result{JSON: out, Model: string(msg.Model), InputTokens: msg.Usage.InputTokens, OutputTokens: msg.Usage.OutputTokens}, nil
}

// CheckKey asks the Models API whether the key may use the model — no
// tokens spent — so a wrong key is refused when it is saved, not when a
// tester first needs it.
func (c *Client) CheckKey(ctx context.Context, apiKey, model string) error {
	if !ValidModel(model) {
		return fmt.Errorf("%w: %q is not offered", ErrModel, model)
	}
	client := c.sdk(apiKey)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if _, err := client.Models.Get(ctx, model, anthropic.ModelGetParams{}); err != nil {
		return classify(err)
	}
	return nil
}

func classify(err error) error {
	var apierr *anthropic.Error
	if errors.As(err, &apierr) {
		switch apierr.StatusCode {
		case 401, 403:
			return fmt.Errorf("%w (%d)", ErrKey, apierr.StatusCode)
		case 404:
			return fmt.Errorf("%w (404)", ErrModel)
		case 429, 529:
			return fmt.Errorf("%w (%d)", ErrRateLimited, apierr.StatusCode)
		default:
			return fmt.Errorf("%w (%d)", ErrUnavailable, apierr.StatusCode)
		}
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}
