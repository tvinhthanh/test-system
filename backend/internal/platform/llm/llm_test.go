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
	res, err := New(srv.URL).JSON(context.Background(), Request{APIKey: "good-key", Model: "claude-opus-5-5", System: "sys", User: "usr", Schema: schema})
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
	if _, err := New(srv.URL).JSON(context.Background(), Request{APIKey: "good-key", Model: "claude-haiku-5-5", Schema: schema}); err != nil {
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
	c := New(srv.URL)
	req := Request{APIKey: "good-key", Model: "claude-sonnet-5-5", Schema: map[string]any{"type": "object"}}
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
	if err := c.CheckKey(context.Background(), "good-key", "claude-opus-5-5"); err != nil {
		t.Errorf("good key: %v", err)
	}
	if err := c.CheckKey(context.Background(), "bad-key", "claude-opus-5-5"); !errors.Is(err, ErrKey) {
		t.Errorf("bad key check: %v", err)
	}
	if err := c.CheckKey(context.Background(), "good-key", "claude-fable-5-1"); !errors.Is(err, ErrModel) {
		t.Errorf("model the key cannot use: %v", err)
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
	res, err := New(srv.URL).JSON(context.Background(), Request{APIKey: "good-key", Model: "claude-opus-5-5", Schema: map[string]any{"type": "object"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.JSON) != `{"cases":[{"title":"SQL injection in the search box"}]}` || res.Model != "claude-opus-4-8" {
		t.Errorf("answer %s from %s", res.JSON, res.Model)
	}
}

// TestLive is the one call to the real Messages API: each offered model
// (or LLM_LIVE_MODELS, comma-separated) gets the key check and one small
// structured answer, through LLM_BASE_URL when that is set. It spends real
// tokens — cents, not dollars — so it runs only with LLM_LIVE_KEY set:
//
//	LLM_LIVE_KEY=sk-ant-... go test ./internal/platform/llm -run TestLive -v -count=1
//
// That is bash; in cmd.exe run `set LLM_LIVE_KEY=sk-ant-...` first, in
// PowerShell `$env:LLM_LIVE_KEY = "sk-ant-..."`. -count=1: a cached pass
// makes no call.
func TestLive(t *testing.T) {
	key := os.Getenv("LLM_LIVE_KEY")
	if key == "" {
		t.Skip("LLM_LIVE_KEY not set: no call to the real API")
	}
	var models []string
	for _, m := range Models {
		models = append(models, m.ID)
	}
	if only := os.Getenv("LLM_LIVE_MODELS"); only != "" {
		models = strings.Split(only, ",")
	}
	schema := map[string]any{
		"type": "object", "additionalProperties": false, "required": []any{"cases"},
		"properties": map[string]any{"cases": map[string]any{"type": "array", "items": map[string]any{
			"type": "object", "additionalProperties": false, "required": []any{"title"},
			"properties": map[string]any{"title": map[string]any{"type": "string"}},
		}}},
	}
	c := New(os.Getenv("LLM_BASE_URL"))
	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			if err := c.CheckKey(ctx, key, model); err != nil {
				t.Fatalf("key check: %v", err)
			}
			res, err := c.JSON(ctx, Request{
				APIKey: key, Model: model, Schema: schema,
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
