package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// A stand-in for a Chat Completions endpoint: it answers a bad key as
// OpenAI does and hands every other request, body decoded, to the test.
func chatServer(t *testing.T, handle func(w http.ResponseWriter, r *http.Request, body map[string]any)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer bad-key" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided: bad-key.","type":"invalid_request_error","code":"invalid_api_key"}}`))
			return
		}
		var body map[string]any
		if r.Method == http.MethodPost {
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("request body is not JSON: %s", raw)
			}
		}
		handle(w, r, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// reply answers as the request asked: streamed (the text in two pieces, the
// finish, a usage chunk, [DONE], with the comments OpenRouter sends between)
// or whole (after the blank lines DeepSeek sends while it works).
func reply(w http.ResponseWriter, body map[string]any, text, finish string) {
	model := fmt.Sprint(body["model"]) + "-2026-10-01"
	if body["stream"] != true {
		w.Header().Set("Content-Type", "application/json")
		b, _ := json.Marshal(map[string]any{"model": model, "choices": []any{map[string]any{"index": 0,
			"message": map[string]any{"role": "assistant", "content": text}, "finish_reason": finish}},
			"usage": map[string]any{"prompt_tokens": 120, "completion_tokens": 40}})
		_, _ = w.Write(append([]byte("\n\n"), b...))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	piece := func(delta map[string]any, finish any) {
		b, _ := json.Marshal(map[string]any{"model": model, "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
	}
	_, _ = fmt.Fprint(w, ": OPENROUTER PROCESSING\n\n")
	half := len(text) / 2
	piece(map[string]any{"role": "assistant", "content": text[:half]}, nil)
	piece(map[string]any{"content": text[half:]}, nil)
	piece(map[string]any{}, finish)
	_, _ = fmt.Fprintf(w, "data: {\"model\":%q,\"choices\":[],\"usage\":{\"prompt_tokens\":120,\"completion_tokens\":40}}\n\n", model)
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

func failWith(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

var casesSchema = map[string]any{
	"type": "object", "additionalProperties": false, "required": []string{"cases"},
	"properties": map[string]any{"cases": map[string]any{"type": "array", "items": map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"title"},
		"properties": map[string]any{"title": map[string]any{"type": "string"}},
	}}},
}

const casesAnswer = `{"cases":[{"title":"Log in"}]}`

func ask(provider, model string) Request {
	return Request{Account: Account{Provider: provider, APIKey: "good-key", Model: model}, System: "sys", User: "usr", Schema: casesSchema}
}

func TestChatRequestShape(t *testing.T) {
	var seen map[string]any
	srv := chatServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer good-key" {
			t.Errorf("%s %s auth %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		seen = body
		reply(w, body, casesAnswer, "stop")
	})
	c := mustNew(t, "openai", srv.URL)
	res, err := c.JSON(context.Background(), ask("openai", "gpt-6-astra"))
	if err != nil {
		t.Fatal(err)
	}
	if string(res.JSON) != casesAnswer || res.Model != "gpt-6-astra-2026-10-01" || res.InputTokens != 120 || res.OutputTokens != 40 {
		t.Fatalf("result: %+v", res)
	}
	rf, _ := seen["response_format"].(map[string]any)
	js, _ := rf["json_schema"].(map[string]any)
	so, _ := seen["stream_options"].(map[string]any)
	msgs, _ := seen["messages"].([]any)
	if seen["model"] != "gpt-6-astra" || seen["max_completion_tokens"] != float64(16000) || seen["stream"] != true || so["include_usage"] != true ||
		rf["type"] != "json_schema" || js["strict"] != true || js["name"] != "answer" || js["schema"] == nil || seen["reasoning_effort"] != "medium" || len(msgs) != 2 {
		t.Errorf("request body: %v", seen)
	}
	if _, ok := seen["max_tokens"]; ok {
		t.Errorf("OpenAI got max_tokens, which its reasoning models refuse: %v", seen)
	}
	if m, _ := msgs[0].(map[string]any); m["role"] != "system" || m["content"] != "sys" {
		t.Errorf("system message: %v", msgs[0])
	}
	// A model off the suggestions may not reason: no effort is sent.
	if _, err := c.JSON(context.Background(), ask("openai", "gpt-4.1")); err != nil {
		t.Fatal(err)
	}
	if _, ok := seen["reasoning_effort"]; ok {
		t.Errorf("effort sent to a model not known to take it: %v", seen)
	}
}

// Each provider is asked in the dialect its documentation gives.
func TestChatDialects(t *testing.T) {
	for _, tc := range []struct {
		provider, model, limit string
		stream, usage, schema  bool
	}{
		{"gemini", "gemini-3.8-flash", "max_tokens", true, true, true},
		{"xai", "grok-4.7", "max_tokens", true, false, true},
		{"mistral", "mistral-large-latest", "max_tokens", true, false, true},
		{"deepseek", "deepseek-v4-pro", "max_tokens", false, false, false},
		{"groq", "openai/gpt-oss-120b", "max_completion_tokens", false, false, true},
		{"openrouter", "openai/gpt-6-astra", "max_tokens", true, false, true},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			var seen map[string]any
			srv := chatServer(t, func(w http.ResponseWriter, _ *http.Request, body map[string]any) {
				seen = body
				answer := casesAnswer
				if !tc.schema {
					answer = "```json\n" + casesAnswer + "\n```" // JSON mode may still fence it
				}
				reply(w, body, answer, "stop")
			})
			res, err := mustNew(t, tc.provider, srv.URL).JSON(context.Background(), ask(tc.provider, tc.model))
			if err != nil {
				t.Fatal(err)
			}
			if string(res.JSON) != casesAnswer || res.InputTokens != 120 {
				t.Errorf("result: %+v", res)
			}
			rf, _ := seen["response_format"].(map[string]any)
			_, hasUsage := seen["stream_options"]
			if (seen["stream"] == true) != tc.stream || hasUsage != tc.usage || seen[tc.limit] != float64(16000) {
				t.Errorf("stream/usage/limit: %v", seen)
			}
			wantFormat := map[bool]string{true: "json_schema", false: "json_object"}[tc.schema]
			if rf["type"] != wantFormat {
				t.Errorf("response_format %v, want %s", rf, wantFormat)
			}
			msgs, _ := seen["messages"].([]any)
			system, _ := msgs[0].(map[string]any)
			if inPrompt := strings.Contains(fmt.Sprint(system["content"]), `"required":["cases"]`); inPrompt == tc.schema {
				t.Errorf("schema in the prompt %v with response_format %s: %v", inPrompt, wantFormat, system["content"])
			}
			if _, ok := seen["reasoning_effort"]; ok {
				t.Errorf("effort sent: %v", seen)
			}
			if p, _ := seen["provider"].(map[string]any); (p["require_parameters"] == true) != (tc.provider == "openrouter") {
				t.Errorf("provider routing: %v", seen["provider"])
			}
		})
	}
}

// What a moment can fix is tried again; what a request can drop is dropped.
func TestChatRetriesAndFallbacks(t *testing.T) {
	var calls atomic.Int32
	var bodies []map[string]any
	var first func(w http.ResponseWriter, n int32) bool // true: it answered
	srv := chatServer(t, func(w http.ResponseWriter, _ *http.Request, body map[string]any) {
		n := calls.Add(1)
		bodies = append(bodies, body)
		if first(w, n) {
			return
		}
		reply(w, body, casesAnswer, "stop")
	})
	c := mustNew(t, "openai", srv.URL)
	run := func(name string, f func(w http.ResponseWriter, n int32) bool) (int32, error) {
		calls.Store(0)
		bodies = nil
		first = f
		_, err := c.JSON(context.Background(), ask("openai", "gpt-6.1-sol"))
		t.Logf("%s: %d call(s), %v", name, calls.Load(), err)
		return calls.Load(), err
	}

	n, err := run("no schema here", func(w http.ResponseWriter, n int32) bool {
		if n == 1 {
			failWith(w, 400, `{"error":{"message":"Invalid parameter: 'response_format' of type 'json_schema' is not supported with this model.","type":"invalid_request_error","param":"response_format"}}`)
		}
		return n == 1
	})
	if rf, _ := bodies[len(bodies)-1]["response_format"].(map[string]any); err != nil || n != 2 || rf["type"] != "json_object" {
		t.Errorf("schema fallback: %d calls, %v, last format %v", n, err, rf)
	}
	n, err = run("limit too high", func(w http.ResponseWriter, n int32) bool {
		if n == 1 {
			failWith(w, 400, `{"error":{"message":"max_completion_tokens is too large: 16000. This model supports at most 8192 completion tokens.","type":"invalid_request_error"}}`)
		}
		return n == 1
	})
	if _, still := bodies[len(bodies)-1]["max_completion_tokens"]; err != nil || n != 2 || still {
		t.Errorf("limit fallback: %d calls, %v, limit still sent %v", n, err, still)
	}
	n, err = run("overloaded once", func(w http.ResponseWriter, n int32) bool {
		if n == 1 {
			failWith(w, 503, `{"error":{"message":"The server is overloaded.","type":"service_unavailable_error","code":"server_is_overloaded"}}`)
		}
		return n == 1
	})
	if err != nil || n != 2 {
		t.Errorf("5xx retried: %d calls, %v", n, err)
	}
	n, err = run("rate limited", func(w http.ResponseWriter, _ int32) bool {
		failWith(w, 429, `{"error":{"message":"Rate limit reached for requests","type":"requests","code":"rate_limit_exceeded"}}`)
		return true
	})
	if !errors.Is(err, ErrRateLimited) || n != 3 {
		t.Errorf("429 gives up after three: %d calls, %v", n, err)
	}
	n, err = run("out of quota", func(w http.ResponseWriter, _ int32) bool {
		failWith(w, 429, `{"error":{"message":"You exceeded your current quota, please check your plan and billing details.","type":"insufficient_quota","code":"insufficient_quota"}}`)
		return true
	})
	if !errors.Is(err, ErrQuota) || n != 1 {
		t.Errorf("a quota is not retried: %d calls, %v", n, err)
	}
}

func TestChatErrors(t *testing.T) {
	var answer func(w http.ResponseWriter, body map[string]any)
	srv := chatServer(t, func(w http.ResponseWriter, _ *http.Request, body map[string]any) { answer(w, body) })
	c := mustNew(t, "openai", srv.URL)
	for _, tc := range []struct {
		name   string
		answer func(w http.ResponseWriter, body map[string]any)
		want   error
	}{
		{"xAI's bad key is a 400", func(w http.ResponseWriter, _ map[string]any) {
			failWith(w, 400, `{"code":"Client specified an invalid argument","error":"Incorrect API key provided: xa***cd. You can obtain an API key from https://console.x.ai."}`)
		}, ErrKey},
		{"Google's bad key is a 400 in a list", func(w http.ResponseWriter, _ map[string]any) {
			failWith(w, 400, `[{"error":{"code":400,"message":"API key not valid. Please pass a valid API key.","status":"INVALID_ARGUMENT"}}]`)
		}, ErrKey},
		{"payment required", func(w http.ResponseWriter, _ map[string]any) {
			failWith(w, 402, `{"error":{"code":402,"message":"Insufficient credits"}}`)
		}, ErrQuota},
		{"model not found", func(w http.ResponseWriter, _ map[string]any) {
			failWith(w, 404, `{"error":{"message":"The model 'gpt-6.1-sol' does not exist or you do not have access to it.","type":"invalid_request_error","code":"model_not_found"}}`)
		}, ErrModel},
		{"a 400 about something else", func(w http.ResponseWriter, _ map[string]any) {
			failWith(w, 400, `{"error":{"message":"messages: too long"}}`)
		}, ErrUnavailable},
		{"refusal", func(w http.ResponseWriter, body map[string]any) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"refusal\":\"I can't help with that.\"}}]}\n\n")
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		}, ErrRefused},
		{"content filter", func(w http.ResponseWriter, body map[string]any) { reply(w, body, "", "content_filter") }, ErrRefused},
		{"cut off", func(w http.ResponseWriter, body map[string]any) { reply(w, body, `{"cases":[{"ti`, "length") }, ErrTruncated},
		{"an error mid-stream", func(w http.ResponseWriter, _ map[string]any) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"{\"}}]}\n\n")
			_, _ = fmt.Fprint(w, "data: {\"error\":{\"code\":502,\"message\":\"Provider returned error\"}}\n\n")
		}, ErrUnavailable},
		{"stream ends early", func(w http.ResponseWriter, _ map[string]any) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"{\\\"cases\\\":[\"}}]}\n\n")
		}, ErrUnavailable},
		{"not the schema", func(w http.ResponseWriter, body map[string]any) { reply(w, body, `{"cases":[{"name":"x"}]}`, "stop") }, ErrUnavailable},
		{"not JSON", func(w http.ResponseWriter, body map[string]any) { reply(w, body, "Sure! Here are the cases.", "stop") }, ErrUnavailable},
		{"empty", func(w http.ResponseWriter, body map[string]any) { reply(w, body, "", "stop") }, ErrUnavailable},
	} {
		answer = tc.answer
		if _, err := c.JSON(context.Background(), ask("openai", "gpt-6.1-sol")); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
	bad := ask("openai", "gpt-6.1-sol")
	bad.APIKey = "bad-key"
	if _, err := c.JSON(context.Background(), bad); !errors.Is(err, ErrKey) {
		t.Errorf("rejected key: %v", err)
	}
	// The operator's endpoint exists only where LLM_PROVIDER names it.
	if _, err := c.JSON(context.Background(), ask(Custom, "llama3.1:8b")); !errors.Is(err, ErrModel) {
		t.Errorf("an endpoint this server does not have: %v", err)
	}
}

func TestCheckKeyChat(t *testing.T) {
	var models string
	var posts atomic.Int32
	srv := chatServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/models":
			if models == "" {
				http.NotFound(w, r)
				return
			}
			if models == "restricted" {
				failWith(w, 403, `{"error":{"message":"You have insufficient permissions for this operation. Missing scopes: api.model.read.","type":"invalid_request_error"}}`)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(models))
		case r.Method == http.MethodGet && r.URL.Path == "/key":
			_, _ = w.Write([]byte(`{"data":{"label":"sk-or-v1-abc...","limit_remaining":74.5}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/chat/completions":
			posts.Add(1)
			if body["model"] == "nope" {
				failWith(w, 404, `{"error":{"message":"model 'nope' not found"}}`)
				return
			}
			reply(w, body, "OK", "length")
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	ctx := context.Background()
	acct := func(provider, key, model string) Account {
		return Account{Provider: provider, APIKey: key, Model: model}
	}

	models = `{"object":"list","data":[{"id":"gpt-6-astra"},{"id":"gpt-6.1-sol"},{"id":"gpt-6-luna"},{"id":"whisper-1"}]}`
	openai := mustNew(t, "openai", srv.URL)
	if err := openai.CheckKey(ctx, acct("openai", "good-key", "gpt-6-astra")); err != nil {
		t.Errorf("listed model: %v", err)
	}
	if err := openai.CheckKey(ctx, acct("openai", "bad-key", "gpt-6-astra")); !errors.Is(err, ErrKey) {
		t.Errorf("bad key: %v", err)
	}
	var me *ModelError
	err := openai.CheckKey(ctx, acct("openai", "good-key", "gpt-6-astr"))
	if !errors.Is(err, ErrModel) || !errors.As(err, &me) || strings.Join(me.Available, ",") != "gpt-6-astra,gpt-6-luna,gpt-6.1-sol" {
		t.Errorf("mistyped model: %v %+v", err, me)
	}
	if posts.Load() != 0 {
		t.Errorf("a key check with a model list spent tokens: %d completions", posts.Load())
	}

	models = `{"object":"list","data":[{"id":"models/gemini-3.8-flash","object":"model"}]}`
	if err := mustNew(t, "gemini", srv.URL).CheckKey(ctx, acct("gemini", "good-key", "gemini-3.8-flash")); err != nil {
		t.Errorf("Google's models/ prefix: %v", err)
	}

	// OpenRouter's list is public: the key is proven at /key first.
	models = `{"data":[{"id":"openai/gpt-6-astra"}]}`
	or := mustNew(t, "openrouter", srv.URL)
	if err := or.CheckKey(ctx, acct("openrouter", "bad-key", "openai/gpt-6-astra")); !errors.Is(err, ErrKey) {
		t.Errorf("OpenRouter bad key: %v", err)
	}
	if err := or.CheckKey(ctx, acct("openrouter", "good-key", "openai/gpt-6-astra")); err != nil {
		t.Errorf("OpenRouter: %v", err)
	}

	// No list to read — an endpoint without /models, a key restricted away
	// from it — and the smallest completion decides instead.
	for _, list := range []string{"", "restricted"} {
		models = list
		posts.Store(0)
		custom := mustNew(t, Custom, srv.URL)
		if err := custom.CheckKey(ctx, acct(Custom, "good-key", "llama3.1:8b")); err != nil || posts.Load() != 1 {
			t.Errorf("probe (%q): %v, %d completions", list, err, posts.Load())
		}
		if err := custom.CheckKey(ctx, acct(Custom, "good-key", "nope")); !errors.Is(err, ErrModel) {
			t.Errorf("probe of a missing model (%q): %v", list, err)
		}
	}
}
