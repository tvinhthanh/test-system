// Package llm is the one client for a large language model (QĐ-15, QĐ-18).
// A key may belong to any provider listed here: Anthropic, through its
// Messages API and the official Go SDK; the others through the
// OpenAI-compatible Chat Completions endpoint each of them serves, spoken
// over platform/outbound. Every host is fixed in this file — or, for the
// operator's own endpoint (LLM_PROVIDER=openai-compatible), by
// configuration — never by a request (§11.6 API7). It knows nothing about
// test cases: a system prompt, a user message and a JSON schema go in; JSON
// that matches the schema comes out. Providers that enforce a schema are
// asked to; every answer is checked against it here before a caller sees it
// (§11.6 API10).
//
// The API key travels per call: an organization's own key, or the server's.
// Errors are classified so the caller can tell a tester something true —
// key rejected, out of credit, model unavailable, rate limited, refused, or
// unreachable.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/outbound"
)

// DefaultModel is Anthropic's default, kept for callers that predate
// providers; a provider's own is Provider.DefaultModel.
const DefaultModel = "claude-opus-5-5"

// Anthropic is the provider of a request that names none, and of a server
// whose LLM_PROVIDER is empty.
const Anthropic = "anthropic"

// Custom is the operator's own OpenAI-compatible endpoint — a company
// gateway, a self-hosted model — offered only when LLM_PROVIDER names it,
// at LLM_BASE_URL, with LLM_MODEL as its model.
const Custom = "openai-compatible"

// Model is one choice offered with a provider. Anthropic's list is the
// allowlist; for the others it is a suggestion, and any model id the key may
// use is accepted (checked with the provider when it is saved).
type Model struct {
	ID    string
	Label string
	Note  string
}

const (
	wireMessages = "messages" // Anthropic's Messages API, through the SDK
	wireChat     = "chat"     // OpenAI-compatible Chat Completions
)

// Provider is one service whose keys the platform takes.
type Provider struct {
	ID           string
	Label        string
	Console      string   // where its API keys are made
	KeyHint      string   // how its keys begin, for the input's placeholder
	KeyPrefixes  []string // beginnings only this provider's keys have
	Models       []Model
	DefaultModel string // "" when none is suggested: the model id is typed
	AnyModel     bool   // model ids beyond Models are accepted

	wire    string
	baseURL string
	// How its Chat Completions differ.
	maxField   string          // the output limit: "max_completion_tokens" or "max_tokens"
	stream     bool            // the answer is streamed; else it comes in one response
	usage      bool            // stream_options.include_usage is understood
	jsonSchema bool            // response_format json_schema (strict); else json_object and the schema in the prompt
	keyPath    string          // a GET that proves the key when the model list does not (it is public)
	extra      map[string]any  // fields every request carries
	effort     map[string]bool // models sent reasoning_effort "medium", as Anthropic's get effort medium
}

var anthropicModels = []Model{
	{ID: "claude-opus-5-5", Label: "Claude Opus 5.5", Note: "Default. Best drafts for most projects."},
	{ID: "claude-sonnet-5-5", Label: "Claude Sonnet 5.5", Note: "Faster and about half the price of Opus."},
	{ID: "claude-haiku-5-5", Label: "Claude Haiku 5.5", Note: "Cheapest and fastest; simpler drafts."},
	{ID: "claude-fable-5-1", Label: "Claude Fable 5.1", Note: "Most capable, and the most expensive."},
}

// Models is Anthropic's list, kept for callers that predate providers.
var Models = anthropicModels

// providers, in the order the dashboard offers them. Model ids are the
// providers' own documentation of 30-10-2026; the dialect of each is what
// its documentation says it accepts.
var providers = []Provider{
	{
		ID: Anthropic, Label: "Anthropic (Claude)", Console: "https://console.anthropic.com/settings/keys",
		KeyHint: "sk-ant-…", KeyPrefixes: []string{"sk-ant-"},
		Models: anthropicModels, DefaultModel: "claude-opus-5-5",
		wire: wireMessages,
	},
	{
		ID: "openai", Label: "OpenAI (GPT)", Console: "https://platform.openai.com/api-keys",
		KeyHint: "sk-proj-…", KeyPrefixes: []string{"sk-proj-", "sk-svcacct-"},
		Models: []Model{
			{ID: "gpt-6-astra", Label: "GPT-6 Astra", Note: "OpenAI's most capable model, and its pick when unsure."},
			{ID: "gpt-6.1-sol", Label: "GPT-6.1 Sol", Note: "Close to Astra for a lower price."},
			{ID: "gpt-6.1-terra", Label: "GPT-6.1 Terra", Note: "Fast and efficient."},
			{ID: "gpt-6-luna", Label: "GPT-6 Luna", Note: "The most efficient, for high volume."},
		},
		DefaultModel: "gpt-6-astra", AnyModel: true,
		wire: wireChat, baseURL: "https://api.openai.com/v1",
		maxField: "max_completion_tokens", stream: true, usage: true, jsonSchema: true,
		effort: map[string]bool{"gpt-6-astra": true, "gpt-6.1-sol": true, "gpt-6.1-terra": true, "gpt-6-luna": true},
	},
	{
		ID: "gemini", Label: "Google (Gemini)", Console: "https://aistudio.google.com/apikey",
		KeyHint: "AIza…", KeyPrefixes: []string{"AIza"},
		Models: []Model{
			{ID: "gemini-4-argon", Label: "Gemini 4 Argon", Note: "Google's most capable model."},
			{ID: "gemini-3.8-flash", Label: "Gemini 3.8 Flash", Note: "Fast, and Google's pick for new projects."},
			{ID: "gemini-3.5-flash-lite", Label: "Gemini 3.5 Flash-Lite", Note: "The fastest and cheapest."},
		},
		DefaultModel: "gemini-3.8-flash", AnyModel: true,
		wire: wireChat, baseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
		maxField: "max_tokens", stream: true, usage: true, jsonSchema: true,
	},
	{
		ID: "xai", Label: "xAI (Grok)", Console: "https://console.x.ai",
		KeyHint: "xai-…", KeyPrefixes: []string{"xai-"},
		Models:       []Model{{ID: "grok-4.7", Label: "Grok 4.7", Note: "xAI's most capable model."}},
		DefaultModel: "grok-4.7", AnyModel: true,
		wire: wireChat, baseURL: "https://api.x.ai/v1",
		maxField: "max_tokens", stream: true, jsonSchema: true,
	},
	{
		ID: "mistral", Label: "Mistral", Console: "https://console.mistral.ai/api-keys",
		Models: []Model{
			{ID: "mistral-large-latest", Label: "Mistral Large", Note: "Mistral's largest general model."},
			{ID: "mistral-medium-latest", Label: "Mistral Medium", Note: "Close to Large for less."},
			{ID: "mistral-small-latest", Label: "Mistral Small", Note: "The fastest and cheapest."},
		},
		DefaultModel: "mistral-large-latest", AnyModel: true,
		wire: wireChat, baseURL: "https://api.mistral.ai/v1",
		maxField: "max_tokens", stream: true, jsonSchema: true,
	},
	{
		ID: "deepseek", Label: "DeepSeek", Console: "https://platform.deepseek.com/api_keys",
		KeyHint: "sk-…",
		Models: []Model{
			{ID: "deepseek-v4-pro", Label: "DeepSeek V4 Pro", Note: "DeepSeek's most capable model."},
			{ID: "deepseek-flash", Label: "DeepSeek Flash", Note: "Faster and cheaper."},
		},
		DefaultModel: "deepseek-v4-pro", AnyModel: true,
		// JSON mode only (no schema enforced), and an answer in one response:
		// DeepSeek sends blank lines while it works, so the wait is not idle.
		wire: wireChat, baseURL: "https://api.deepseek.com",
		maxField: "max_tokens",
	},
	{
		ID: "groq", Label: "Groq", Console: "https://console.groq.com/keys",
		KeyHint: "gsk_…", KeyPrefixes: []string{"gsk_"},
		Models: []Model{
			{ID: "openai/gpt-oss-120b", Label: "GPT-OSS 120B", Note: "The largest model Groq holds to a schema."},
			{ID: "qwen/qwen3.8-27b", Label: "Qwen 3.8 27B", Note: "Mid-size, held to a schema."},
			{ID: "openai/gpt-oss-20b", Label: "GPT-OSS 20B", Note: "The smallest and fastest."},
		},
		DefaultModel: "openai/gpt-oss-120b", AnyModel: true,
		// Groq holds an answer to a schema only when it is not streamed.
		wire: wireChat, baseURL: "https://api.groq.com/openai/v1",
		maxField: "max_completion_tokens", jsonSchema: true,
	},
	{
		ID: "openrouter", Label: "OpenRouter (many models, one key)", Console: "https://openrouter.ai/settings/keys",
		KeyHint: "sk-or-v1-…", KeyPrefixes: []string{"sk-or-"},
		AnyModel: true,
		// Its model list is public, so the key is proven at /key; and only
		// the endpoints that take response_format may answer.
		wire: wireChat, baseURL: "https://openrouter.ai/api/v1",
		maxField: "max_tokens", stream: true, jsonSchema: true,
		keyPath: "/key", extra: map[string]any{"provider": map[string]any{"require_parameters": true}},
	},
}

// custom is the operator's endpoint: whatever answers there is assumed to
// speak the common subset — one response, max_tokens, a schema when it
// takes one (else JSON mode).
var custom = Provider{
	ID: Custom, Label: "OpenAI-compatible endpoint (this server)", AnyModel: true,
	wire: wireChat, maxField: "max_tokens", jsonSchema: true,
}

// Providers is the list the dashboard offers, in its order; the operator's
// endpoint is not in it (it exists only where LLM_PROVIDER names it).
func Providers() []Provider {
	return append([]Provider(nil), providers...)
}

// Find returns a provider by id ("" is Anthropic), the operator's endpoint
// included.
func Find(id string) (Provider, bool) {
	if id == "" {
		id = Anthropic
	}
	if id == Custom {
		return custom, true
	}
	for _, p := range providers {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// Label is the provider's name for a sentence; the id when it is unknown.
func Label(id string) string {
	if p, ok := Find(id); ok {
		return p.Label
	}
	return id
}

// modelID is what a model id may look like at any provider: OpenRouter's
// and Groq's carry a slash, local servers a colon.
var modelID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,199}$`)

// ValidModel reports whether the provider takes the model id: one of
// Anthropic's list, or for the others anything shaped like a model id.
func ValidModel(provider, model string) bool {
	p, ok := Find(provider)
	if !ok {
		return false
	}
	if p.AnyModel {
		return modelID.MatchString(model)
	}
	for _, m := range p.Models {
		if m.ID == model {
			return true
		}
	}
	return false
}

// ProviderOf is the provider that offers the model by name, for a save that
// names only a model (a dashboard from before providers); Anthropic
// otherwise.
func ProviderOf(model string) string {
	for _, p := range providers {
		for _, m := range p.Models {
			if m.ID == model {
				return p.ID
			}
		}
	}
	return Anthropic
}

// GuessProvider names the provider a key belongs to when its beginning is
// that provider's alone ("sk-ant-", "sk-or-", "AIza", …); "" when unsure —
// DeepSeek's keys and OpenAI's older ones both begin with a bare "sk-".
func GuessProvider(apiKey string) string {
	for _, p := range providers {
		for _, pre := range p.KeyPrefixes {
			if strings.HasPrefix(apiKey, pre) {
				return p.ID
			}
		}
	}
	return ""
}

// ServerModel is the model the server's key uses when an organization chose
// none: LLM_MODEL, else the provider's default. The operator's endpoint has
// no default, so LLM_MODEL is required there.
func ServerModel(provider, model string) (string, error) {
	p, ok := Find(provider)
	if !ok {
		return "", fmt.Errorf("llm: LLM_PROVIDER=%q is not one of %s", provider, strings.Join(ids(), ", "))
	}
	if model == "" {
		model = p.DefaultModel
	}
	if model == "" {
		return "", fmt.Errorf("llm: LLM_PROVIDER=%s needs LLM_MODEL (no model is suggested there)", p.ID)
	}
	if !ValidModel(p.ID, model) {
		return "", fmt.Errorf("llm: LLM_MODEL=%q is not a model %s takes", model, p.Label)
	}
	return model, nil
}

func ids() []string {
	out := []string{}
	for _, p := range providers {
		out = append(out, p.ID)
	}
	return append(out, Custom)
}

var (
	// ErrKey: the API key was rejected (401/403, or a 400 that says so).
	ErrKey = errors.New("llm: API key rejected")
	// ErrQuota: the key's account is out of credit or over a spending limit
	// (402, OpenAI's insufficient_quota, Anthropic's credit balance) —
	// retrying cannot help.
	ErrQuota = errors.New("llm: account out of credit or over its limit")
	// ErrModel: the key cannot use that model (404), or it is not offered.
	ErrModel = errors.New("llm: model unavailable")
	// ErrRateLimited: 429 or overloaded, after the retries.
	ErrRateLimited = errors.New("llm: rate limited or overloaded")
	// ErrRefused: the model declined (a refusal, or a content filter).
	ErrRefused = errors.New("llm: request refused")
	// ErrTruncated: the answer hit the output limit before the JSON was complete.
	ErrTruncated = errors.New("llm: answer cut off")
	// ErrUnavailable: transport errors, 5xx, or an answer that is not JSON
	// matching the schema.
	ErrUnavailable = errors.New("llm: service unavailable")
)

// ModelError is ErrModel with some of the models the key can use, when the
// provider listed them — what to try instead of a mistyped id.
type ModelError struct {
	Model     string
	Available []string
}

func (e *ModelError) Error() string {
	return fmt.Sprintf("%v: %q is not among this key's models", ErrModel, e.Model)
}

func (e *ModelError) Unwrap() error { return ErrModel }

// Account is whose key pays, where it is spent and on which model.
type Account struct {
	Provider string // "" is Anthropic
	APIKey   string
	Model    string
}

// Request is one structured call.
type Request struct {
	Account
	System    string
	User      string
	Schema    map[string]any // JSON schema; every object lists all its properties as required and has additionalProperties: false
	MaxTokens int64          // 0 = 16000
}

// Result is the schema-valid JSON and what it cost. Model is the model that
// wrote it, as the provider names it: after Anthropic's refusal fallback the
// fallback model, whose attempt the token counts are (the API's top-level
// usage; the declined attempt is only in usage.iterations, billed when it
// streamed output or its refusal category is a billed one). A provider that
// streams without usage leaves the counts 0.
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

// maxAnswer bounds one Chat Completions answer (a model list or a stream of
// tens of thousands of tokens is well under it).
const maxAnswer = 16 << 20

type Client struct {
	provider string // the server's (LLM_PROVIDER)
	baseURL  string // where its calls go when set (LLM_BASE_URL)
	timeout  time.Duration
	chatHTTP *outbound.Client // the Chat Completions hosts, fixed here
	backoff  time.Duration    // the first Chat Completions retry's wait; 0 = 1 s
}

// New returns a client. provider is the server's (LLM_PROVIDER, "" =
// Anthropic); baseURL, when set, is where that provider's calls go instead
// of its usual host (tests, an egress proxy) — the only place they can go,
// for Custom. A call may take up to ten minutes: answers are streamed where
// the provider allows it (runnable test cases, QĐ-16, run to tens of
// thousands of tokens), and the limit bounds the whole exchange.
func New(provider, baseURL string) (*Client, error) {
	if provider == "" {
		provider = Anthropic
	}
	if _, ok := Find(provider); !ok {
		return nil, fmt.Errorf("llm: LLM_PROVIDER=%q is not one of %s", provider, strings.Join(ids(), ", "))
	}
	c := &Client{provider: provider, baseURL: strings.TrimSuffix(baseURL, "/"), timeout: 10 * time.Minute}
	if provider == Custom && c.baseURL == "" {
		return nil, errors.New("llm: LLM_PROVIDER=openai-compatible needs LLM_BASE_URL")
	}
	var hosts []string
	for _, p := range append(Providers(), custom) {
		if base := c.endpoint(p); p.wire == wireChat && base != "" {
			hosts = append(hosts, base)
		}
	}
	h, err := outbound.New(c.timeout, hosts...)
	if err != nil {
		return nil, fmt.Errorf("llm: LLM_BASE_URL: %w", err)
	}
	c.chatHTTP = h.WithMaxBody(maxAnswer)
	return c, nil
}

// Server is the client and the server key's model from configuration
// (LLM_PROVIDER, LLM_BASE_URL, LLM_MODEL) — what cmd/api and cmd/worker
// build at boot, refusing what this package does not know.
func Server(provider, baseURL, model string) (*Client, string, error) {
	c, err := New(provider, baseURL)
	if err != nil {
		return nil, "", err
	}
	m, err := ServerModel(provider, model)
	if err != nil {
		return nil, "", err
	}
	return c, m, nil
}

// endpoint is where a provider's calls go from this server; "" for the
// operator's endpoint on a server that has none.
func (c *Client) endpoint(p Provider) string {
	if p.ID == c.provider && c.baseURL != "" {
		return c.baseURL
	}
	return p.baseURL
}

func (c *Client) find(acct Account) (Provider, error) {
	p, ok := Find(acct.Provider)
	switch {
	case !ok:
		return Provider{}, fmt.Errorf("%w: provider %q is not offered", ErrModel, acct.Provider)
	case p.wire == wireChat && c.endpoint(p) == "":
		return Provider{}, fmt.Errorf("%w: this server has no %s", ErrModel, p.Label)
	case !ValidModel(p.ID, acct.Model):
		return Provider{}, fmt.Errorf("%w: %q is not offered", ErrModel, acct.Model)
	}
	return p, nil
}

// JSON makes one call and returns the structured answer, checked against
// the schema. Thinking is left at each model's default; effort is medium
// where the model takes it, set explicitly because the defaults differ.
func (c *Client) JSON(ctx context.Context, req Request) (Result, error) {
	p, err := c.find(req.Account)
	if err != nil {
		return Result{}, err
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = 16000
	}
	var res Result
	if p.wire == wireMessages {
		res, err = c.messages(ctx, req)
	} else {
		res, err = c.chat(ctx, p, req)
	}
	if err != nil {
		return Result{}, err
	}
	if err := Validate(req.Schema, res.JSON); err != nil {
		return Result{}, fmt.Errorf("%w: the answer does not match the schema: %v", ErrUnavailable, err)
	}
	return res, nil
}

// CheckKey asks the provider whether the key may use the model — from its
// model list, no tokens spent where it has one a key can read — so a wrong
// key is refused when it is saved, not when a tester first needs it.
func (c *Client) CheckKey(ctx context.Context, acct Account) error {
	p, err := c.find(acct)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if p.wire == wireMessages {
		return c.checkMessages(ctx, acct)
	}
	return c.checkChat(ctx, p, acct)
}
