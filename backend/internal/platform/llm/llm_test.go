package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// A stand-in for the Messages API: it checks what the SDK sends and answers
// like the real one, so the request shape (structured output, effort,
// refusal fallback, key header) and the error mapping are pinned without a
// network call or a real key.
func fakeAPI(t *testing.T, answer func(w http.ResponseWriter, body map[string]any)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") == "bad-key" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
			return
		}
		switch {
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v1/models/"):
			w.Header().Set("Content-Type", "application/json")
			if strings.HasSuffix(r.URL.Path, "/claude-fable-5-1") {
				w.WriteHeader(404)
				_, _ = w.Write([]byte(`{"type":"error","error":{"type":"not_found_error","message":"model"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"type":"model","id":"claude-opus-5-5","display_name":"Claude Opus 5.5","created_at":"2026-09-01T00:00:00Z"}`))
		case r.Method == "POST" && r.URL.Path == "/v1/messages":
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			body["_beta"] = r.Header.Get("Anthropic-Beta")
			if body["stream"] != true {
				w.Header().Set("Content-Type", "application/json")
				answer(w, body)
				return
			}
			// The answer is written as a message; streamed, it goes out as the
			// server-sent events the API sends for it.
			rec := httptest.NewRecorder()
			answer(rec, body)
			writeSSE(w, rec.Body.Bytes())
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// writeSSE sends a message as the Messages API streams it.
func writeSSE(w http.ResponseWriter, raw []byte) {
	var m struct {
		ID, Model  string
		Content    []struct{ Text string }
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		}
	}
	_ = json.Unmarshal(raw, &m)
	w.Header().Set("Content-Type", "text/event-stream")
	event := func(name string, data map[string]any) {
		b, _ := json.Marshal(data)
		_, _ = w.Write([]byte("event: " + name + "\ndata: " + string(b) + "\n\n"))
	}
	event("message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": m.ID, "type": "message", "role": "assistant", "model": m.Model, "content": []any{},
		"stop_reason": nil, "stop_sequence": nil, "usage": map[string]any{"input_tokens": m.Usage.InputTokens, "output_tokens": 1}}})
	event("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
	text := ""
	if len(m.Content) > 0 {
		text = m.Content[0].Text
	}
	// Two deltas, so accumulation is what puts the text together.
	half := len(text) / 2
	for _, part := range []string{text[:half], text[half:]} {
		event("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": part}})
	}
	event("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	event("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": m.StopReason, "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": m.Usage.OutputTokens}})
	event("message_stop", map[string]any{"type": "message_stop"})
}

func mustNew(t *testing.T, provider, baseURL string) *Client {
	t.Helper()
	c, err := New(provider, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	c.backoff = time.Millisecond
	return c
}

func message(text, stop string) string {
	b, _ := json.Marshal(map[string]any{
		"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5-5",
		"content":     []any{map[string]any{"type": "text", "text": text}},
		"stop_reason": stop, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": 120, "output_tokens": 40},
	})
	return string(b)
}

func TestJSONRequestShape(t *testing.T) {
	var seen map[string]any
	srv := fakeAPI(t, func(w http.ResponseWriter, body map[string]any) {
		seen = body
		_, _ = w.Write([]byte(message(`{"cases":[{"title":"Log in"}]}`, "end_turn")))
	})
	schema := map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"cases": map[string]any{"type": "array"}}}
	res, err := mustNew(t, Anthropic, srv.URL).JSON(context.Background(), Request{Account: Account{APIKey: "good-key", Model: "claude-opus-5-5"}, System: "sys", User: "usr", Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.JSON) != `{"cases":[{"title":"Log in"}]}` || res.InputTokens != 120 || res.OutputTokens != 40 {
		t.Fatalf("result: %+v", res)
	}
	oc, _ := seen["output_config"].(map[string]any)
	format, _ := oc["format"].(map[string]any)
	if seen["model"] != "claude-opus-5-5" || seen["max_tokens"] != float64(16000) || oc["effort"] != "medium" || format["type"] != "json_schema" || format["schema"] == nil || seen["stream"] != true {
		t.Errorf("request body: %v", seen)
	}
	if seen["fallbacks"] != "default" || !strings.Contains(seen["_beta"].(string), "server-side-fallback-2026-07-01") {
		t.Errorf("refusal fallback not requested: fallbacks=%v beta=%v", seen["fallbacks"], seen["_beta"])
	}
	if _, ok := seen["thinking"]; ok {
		t.Errorf("thinking sent explicitly: %v", seen["thinking"])
	}

	// Haiku has no server-side fallback: none is asked for.
	if _, err := mustNew(t, Anthropic, srv.URL).JSON(context.Background(), Request{Account: Account{APIKey: "good-key", Model: "claude-haiku-5-5"}, Schema: schema}); err != nil {
		t.Fatal(err)
	}
	if _, ok := seen["fallbacks"]; ok {
		t.Errorf("haiku got fallbacks: %v", seen["fallbacks"])
	}
}

func TestErrors(t *testing.T) {
	stop := "refusal"
	text := `{}`
	srv := fakeAPI(t, func(w http.ResponseWriter, _ map[string]any) { _, _ = w.Write([]byte(message(text, stop))) })
	c := mustNew(t, Anthropic, srv.URL)
	req := Request{Account: Account{APIKey: "good-key", Model: "claude-sonnet-5-5"}, Schema: map[string]any{"type": "object"}}
	if _, err := c.JSON(context.Background(), req); !errors.Is(err, ErrRefused) {
		t.Errorf("refusal: %v", err)
	}
	stop = "max_tokens"
	if _, err := c.JSON(context.Background(), req); !errors.Is(err, ErrTruncated) {
		t.Errorf("max_tokens: %v", err)
	}
	stop, text = "end_turn", "not json"
	if _, err := c.JSON(context.Background(), req); !errors.Is(err, ErrUnavailable) {
		t.Errorf("non-JSON answer: %v", err)
	}
	bad := req
	bad.APIKey = "bad-key"
	if _, err := c.JSON(context.Background(), bad); !errors.Is(err, ErrKey) {
		t.Errorf("rejected key: %v", err)
	}
	other := req
	other.Model = "gpt-whatever"
	if _, err := c.JSON(context.Background(), other); !errors.Is(err, ErrModel) {
		t.Errorf("model off the list: %v", err)
	}
	if err := c.CheckKey(context.Background(), Account{APIKey: "good-key", Model: "claude-opus-5-5"}); err != nil {
		t.Errorf("good key: %v", err)
	}
	if err := c.CheckKey(context.Background(), Account{APIKey: "bad-key", Model: "claude-opus-5-5"}); !errors.Is(err, ErrKey) {
		t.Errorf("bad key check: %v", err)
	}
	if err := c.CheckKey(context.Background(), Account{APIKey: "good-key", Model: "claude-fable-5-1"}); !errors.Is(err, ErrModel) {
		t.Errorf("model the key cannot use: %v", err)
	}
}

// Anthropic says an empty account with a 400, not a status of its own.
func TestCreditBalanceIsAQuota(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"Your credit balance is too low to access the Anthropic API. Please go to Plans & Billing to upgrade or purchase credits."}}`))
	}))
	t.Cleanup(srv.Close)
	_, err := mustNew(t, Anthropic, srv.URL).JSON(context.Background(), Request{Account: Account{APIKey: "good-key", Model: "claude-haiku-5-5"}, Schema: map[string]any{"type": "object"}})
	if !errors.Is(err, ErrQuota) {
		t.Errorf("credit balance: %v", err)
	}
}

// A decline mid-answer is re-served on the same stream: the declined
// model's partial text stays, a fallback block marks the switch, and the
// fallback model continues the text (server-side-fallback-2026-07-01). The
// answer is every text block together; the model is the one that finished.
func TestMidStreamFallbackKeepsTheWholeAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range []string{
			`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":120,"output_tokens":1}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"{\"cases\":[{\"ti"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"fallback","from":{"model":"claude-opus-5-5"},"to":{"model":"claude-opus-4-8"},"trigger":{"type":"refusal","category":"cyber"}}}`,
			`{"type":"content_block_stop","index":1}`,
			`{"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"tle\":\"SQL injection in the search box\"}]}"}}`,
			`{"type":"content_block_stop","index":2}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":40}}`,
			`{"type":"message_stop"}`,
		} {
			var ev struct{ Type string }
			_ = json.Unmarshal([]byte(e), &ev)
			_, _ = w.Write([]byte("event: " + ev.Type + "\ndata: " + e + "\n\n"))
		}
	}))
	t.Cleanup(srv.Close)
	res, err := mustNew(t, Anthropic, srv.URL).JSON(context.Background(), Request{Account: Account{APIKey: "good-key", Model: "claude-opus-5-5"}, Schema: map[string]any{"type": "object"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.JSON) != `{"cases":[{"title":"SQL injection in the search box"}]}` || res.Model != "claude-opus-4-8" {
		t.Errorf("answer %s from %s", res.JSON, res.Model)
	}
}

// TestLiveHostsRejectAFakeKey asks every provider's real host to check a key
// that cannot be anyone's: each must answer with a refusal this package
// reads as ErrKey — the host, the path and the error shape are right, with
// nothing spent. Opt-in (LLM_LIVE_HOSTS=1): it goes to the network.
func TestLiveHostsRejectAFakeKey(t *testing.T) {
	if os.Getenv("LLM_LIVE_HOSTS") != "1" {
		t.Skip("LLM_LIVE_HOSTS not set: no call to the real hosts")
	}
	c := mustNew(t, Anthropic, "")
	for _, p := range Providers() {
		t.Run(p.ID, func(t *testing.T) {
			model := p.DefaultModel
			if model == "" {
				model = "openai/gpt-6-astra"
			}
			fake := "atp-live-check-not-a-key"
			if len(p.KeyPrefixes) > 0 {
				fake = p.KeyPrefixes[0] + fake
			}
			err := c.CheckKey(context.Background(), Account{Provider: p.ID, APIKey: fake, Model: model})
			t.Logf("%s: %v", p.Label, err)
			if !errors.Is(err, ErrKey) {
				t.Errorf("a fake key was not read as rejected: %v", err)
			}
		})
	}
}

// TestLive is the one call to a real provider: each model it offers (or
// LLM_LIVE_MODELS, comma-separated) gets the key check and one small
// structured answer, through LLM_BASE_URL when that is set. The provider is
// LLM_LIVE_PROVIDER, else the one the key's beginning names, else
// Anthropic. It spends real tokens — cents, not dollars — so it runs only
// with LLM_LIVE_KEY set:
//
//	LLM_LIVE_KEY=sk-proj-... go test ./internal/platform/llm -run TestLive -v -count=1
//
// That is bash; in cmd.exe run `set "LLM_LIVE_KEY=sk-proj-..."` first, in
// PowerShell `$env:LLM_LIVE_KEY = "sk-proj-..."`. A key whose beginning is
// not its provider's alone (DeepSeek's, Mistral's, an older OpenAI "sk-…")
// needs LLM_LIVE_PROVIDER too. -count=1: a cached pass makes no call.
func TestLive(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("LLM_LIVE_KEY")) // as config reads LLM_API_KEY; cmd's `set` keeps a trailing space
	if key == "" {
		t.Skip("LLM_LIVE_KEY not set: no call to the real API")
	}
	provider := strings.TrimSpace(os.Getenv("LLM_LIVE_PROVIDER"))
	if provider == "" {
		provider = GuessProvider(key)
	}
	p, ok := Find(provider)
	if !ok {
		t.Fatalf("LLM_LIVE_PROVIDER=%q is not one of %s", provider, strings.Join(ids(), ", "))
	}
	var models []string
	for _, m := range p.Models {
		models = append(models, m.ID)
	}
	if only := os.Getenv("LLM_LIVE_MODELS"); only != "" {
		models = strings.Split(only, ",")
	}
	if len(models) == 0 {
		t.Fatalf("%s suggests no model: name one in LLM_LIVE_MODELS", p.Label)
	}
	t.Logf("provider %s, %d model(s)", p.Label, len(models))
	schema := map[string]any{
		"type": "object", "additionalProperties": false, "required": []any{"cases"},
		"properties": map[string]any{"cases": map[string]any{"type": "array", "items": map[string]any{
			"type": "object", "additionalProperties": false, "required": []any{"title"},
			"properties": map[string]any{"title": map[string]any{"type": "string"}},
		}}},
	}
	c, err := New(p.ID, os.Getenv("LLM_BASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range models {
		model = strings.TrimSpace(model)
		t.Run(model, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			acct := Account{Provider: p.ID, APIKey: key, Model: model}
			if err := c.CheckKey(ctx, acct); err != nil {
				var me *ModelError
				if errors.As(err, &me) && len(me.Available) > 0 {
					t.Fatalf("key check: %v — this key has %s", err, strings.Join(me.Available, ", "))
				}
				t.Fatalf("key check: %v", err)
			}
			res, err := c.JSON(ctx, Request{
				Account: acct, Schema: schema,
				System: "You write manual test cases for web pages.",
				User:   "Two test cases for a sign-in form with an email and a password field. Titles only.",
			})
			if err != nil {
				t.Fatal(err)
			}
			var out struct{ Cases []struct{ Title string } }
			if err := json.Unmarshal(res.JSON, &out); err != nil || len(out.Cases) == 0 || out.Cases[0].Title == "" {
				t.Fatalf("answer %s: %v", res.JSON, err)
			}
			t.Logf("served by %s: %d cases (%q …), %d input / %d output tokens", res.Model, len(out.Cases), out.Cases[0].Title, res.InputTokens, res.OutputTokens)
		})
	}
}
