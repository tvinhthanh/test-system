package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
)

// Anthropic's Messages API, through the official Go SDK (QĐ-15).

func (c *Client) sdk(apiKey string) anthropic.Client {
	opts := []option.RequestOption{option.WithAPIKey(apiKey), option.WithMaxRetries(2), option.WithRequestTimeout(c.timeout)}
	if c.provider == Anthropic && c.baseURL != "" {
		opts = append(opts, option.WithBaseURL(c.baseURL))
	}
	return anthropic.NewClient(opts...)
}

// fallbackModels get the server-side refusal fallback (beta
// server-side-fallback-2026-07-01, "default" routing): a declined request
// is re-served by the API inside the same call. Claude Haiku 5.5 has none.
var fallbackModels = map[string]bool{"claude-opus-5-5": true, "claude-sonnet-5-5": true, "claude-fable-5-1": true}

// messages makes one streamed call and accumulates it. Streaming keeps a
// long answer from idling the connection until a proxy cuts it. Thinking is
// left at each model's default (adaptive; always on for Opus 5.5 and Fable
// 5.1); effort is medium.
func (c *Client) messages(ctx context.Context, req Request) (Result, error) {
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
		return Result{}, classifyMessages(err)
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

// checkMessages asks the Models API whether the key may use the model.
func (c *Client) checkMessages(ctx context.Context, acct Account) error {
	client := c.sdk(acct.APIKey)
	if _, err := client.Models.Get(ctx, acct.Model, anthropic.ModelGetParams{}); err != nil {
		return classifyMessages(err)
	}
	return nil
}

func classifyMessages(err error) error {
	var apierr *anthropic.Error
	if errors.As(err, &apierr) {
		switch {
		case apierr.StatusCode == 401 || apierr.StatusCode == 403:
			return fmt.Errorf("%w (%d)", ErrKey, apierr.StatusCode)
		case apierr.StatusCode == 404:
			return fmt.Errorf("%w (404)", ErrModel)
		case apierr.StatusCode == 429 || apierr.StatusCode == 529:
			return fmt.Errorf("%w (%d)", ErrRateLimited, apierr.StatusCode)
		case apierr.StatusCode == 400 && strings.Contains(strings.ToLower(apierr.RawJSON()), "credit balance"):
			return fmt.Errorf("%w (400)", ErrQuota)
		default:
			return fmt.Errorf("%w (%d)", ErrUnavailable, apierr.StatusCode)
		}
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}
