package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The OpenAI-compatible Chat Completions endpoint of every provider but
// Anthropic (QĐ-18): one wire shape spoken to the hosts of the providers
// table through platform/outbound. No vendor SDK — each host differs a
// little (the output limit's name, streaming, schemas) and the table says
// how.

var (
	// errNoSchema: the provider or model does not take response_format
	// json_schema; the call is made again in JSON mode.
	errNoSchema = errors.New("llm: no schema here")
	// errLimit: the output limit is more than the model allows; the call is
	// made again without one, so the model's own (lower) limit applies.
	errLimit = errors.New("llm: limit above the model's")
	// errNoList: no model list a key can read (an endpoint without
	// /models, an OpenAI key restricted away from it); the key is proven by
	// the smallest completion instead.
	errNoList = errors.New("llm: no model list")
)

type dialect struct {
	schema bool // response_format json_schema; else json_object
	limit  bool // send the output limit
}

func (c *Client) chat(ctx context.Context, p Provider, req Request) (Result, error) {
	d := dialect{schema: p.jsonSchema, limit: true}
	res, err := c.complete(ctx, p, req, d)
	// A model that takes no schema, or allows less output than asked, says so
	// with a 400; the same call without that part still answers — and JSON
	// checks the answer against the schema either way.
	for range 2 {
		switch {
		case d.schema && errors.Is(err, errNoSchema):
			d.schema = false
		case d.limit && errors.Is(err, errLimit):
			d.limit = false
		default:
			return res, err
		}
		res, err = c.complete(ctx, p, req, d)
	}
	return res, err
}

func (c *Client) complete(ctx context.Context, p Provider, req Request, d dialect) (Result, error) {
	system := req.System
	body := map[string]any{"model": req.Model}
	if d.limit {
		body[p.maxField] = req.MaxTokens
	}
	if d.schema {
		body["response_format"] = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "answer", "strict": true, "schema": req.Schema}}
	} else {
		s, err := json.Marshal(req.Schema)
		if err != nil {
			return Result{}, err
		}
		// JSON mode wants the word in the prompt, and gets the shape there.
		system += "\n\nAnswer with one JSON object and nothing else. It must match this JSON schema:\n" + string(s)
		body["response_format"] = map[string]any{"type": "json_object"}
	}
	body["messages"] = []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": req.User}}
	if p.effort[req.Model] {
		body["reasoning_effort"] = "medium"
	}
	if p.stream {
		body["stream"] = true
		if p.usage {
			body["stream_options"] = map[string]any{"include_usage": true}
		}
	}
	for k, v := range p.extra {
		body[k] = v
	}
	b, err := json.Marshal(body)
	if err != nil {
		return Result{}, err
	}
	resp, err := c.call(ctx, http.MethodPost, c.endpoint(p)+"/chat/completions", req.APIKey, b)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	a := &answer{}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		err = a.readStream(resp.Body)
	} else {
		err = a.readOne(resp.Body)
	}
	if err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		return Result{}, err
	}
	return a.result(d.schema)
}

// answer is a Chat Completions answer put together, streamed or not.
type answer struct {
	text, refusal strings.Builder
	finish, model string
	in, out       int64
}

// chunk is a whole answer or one streamed piece of it; providers send an
// error object in place of one when something fails mid-stream.
type chunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta        chatMessage `json:"delta"`
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
	Error json.RawMessage `json:"error"`
}

type chatMessage struct {
	Content json.RawMessage `json:"content"` // a string, or parts (Mistral's thinking models)
	Refusal string          `json:"refusal"`
}

func (a *answer) add(ch chunk) error {
	if len(ch.Error) > 0 && string(ch.Error) != "null" {
		return parseError(0, ch.Error).classify()
	}
	if ch.Model != "" {
		a.model = ch.Model
	}
	if ch.Usage != nil {
		a.in, a.out = ch.Usage.PromptTokens, ch.Usage.CompletionTokens
	}
	if len(ch.Choices) > 0 { // n is 1
		choice := ch.Choices[0]
		a.text.WriteString(textOf(choice.Delta.Content) + textOf(choice.Message.Content))
		a.refusal.WriteString(choice.Delta.Refusal + choice.Message.Refusal)
		if choice.FinishReason != "" {
			a.finish = choice.FinishReason
		}
	}
	return nil
}

func (a *answer) readOne(body io.Reader) error {
	var ch chunk
	// A JSON decoder skips the blank lines a provider sends while it works.
	if err := json.NewDecoder(body).Decode(&ch); err != nil {
		return fmt.Errorf("%w: the answer is unreadable: %v", ErrUnavailable, err)
	}
	return a.add(ch)
}

func (a *answer) readStream(body io.Reader) error {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	done := false
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue // event names, ": keep-alive" comments, blank lines
		}
		data := bytes.TrimSpace(line[len("data:"):])
		if string(data) == "[DONE]" {
			done = true
			break
		}
		var ch chunk
		if err := json.Unmarshal(data, &ch); err != nil {
			return fmt.Errorf("%w: a streamed piece is not JSON: %v", ErrUnavailable, err)
		}
		if err := a.add(ch); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("%w: the stream broke: %v", ErrUnavailable, err)
	}
	if !done && a.finish == "" {
		return fmt.Errorf("%w: the stream ended before the answer did", ErrUnavailable)
	}
	return nil
}

func (a *answer) result(schema bool) (Result, error) {
	switch {
	case a.refusal.Len() > 0 || a.finish == "content_filter":
		return Result{}, ErrRefused
	case a.finish == "length":
		return Result{}, ErrTruncated
	}
	text := strings.TrimSpace(a.text.String())
	if !schema {
		text = unfence(text)
	}
	if text == "" {
		return Result{}, fmt.Errorf("%w: the answer is empty", ErrUnavailable)
	}
	out := json.RawMessage(text)
	if !json.Valid(out) {
		return Result{}, fmt.Errorf("%w: the answer is not JSON", ErrUnavailable)
	}
	return Result{JSON: out, Model: a.model, InputTokens: a.in, OutputTokens: a.out}, nil
}

func textOf(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct{ Type, Text string }
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// unfence takes the JSON out of a Markdown code fence, which a model in
// JSON mode sometimes still writes.
func unfence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	i := strings.IndexByte(s, '\n')
	if i < 0 {
		return s
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s[i+1:]), "```"))
}

// checkChat: the key's model list says whether it may use the model —
// OpenRouter's list is public, so its key is proven first at /key.
func (c *Client) checkChat(ctx context.Context, p Provider, acct Account) error {
	base := c.endpoint(p)
	if p.keyPath != "" {
		resp, err := c.call(ctx, http.MethodGet, base+p.keyPath, acct.APIKey, nil)
		if err != nil {
			return err
		}
		resp.Body.Close()
	}
	models, err := c.listModels(ctx, base, acct.APIKey)
	if errors.Is(err, errNoList) {
		return c.probe(ctx, p, acct)
	}
	if err != nil {
		return err
	}
	for _, id := range models {
		if id == acct.Model {
			return nil
		}
	}
	return &ModelError{Model: acct.Model, Available: nearest(models, acct.Model, 8)}
}

func (c *Client) listModels(ctx context.Context, base, key string) ([]string, error) {
	resp, err := c.call(ctx, http.MethodGet, base+"/models", key, nil)
	var he *httpError
	if errors.As(err, &he) && (he.status == 404 || he.status == 405 || he.status == 501 ||
		he.status == 403 && has(strings.ToLower(he.api.message), "permission", "scope")) {
		return nil, errNoList
	}
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("%w: the model list is unreadable: %v", ErrUnavailable, err)
	}
	ids := make([]string, 0, len(list.Data))
	for _, m := range list.Data {
		ids = append(ids, strings.TrimPrefix(m.ID, "models/")) // Google names them models/<id>
	}
	return ids, nil
}

// probe proves key and model with the smallest completion — a few tokens —
// where no model list can be read.
func (c *Client) probe(ctx context.Context, p Provider, acct Account) error {
	body, err := json.Marshal(map[string]any{"model": acct.Model, p.maxField: 16,
		"messages": []map[string]string{{"role": "user", "content": "Reply with OK."}}})
	if err != nil {
		return err
	}
	resp, err := c.call(ctx, http.MethodPost, c.endpoint(p)+"/chat/completions", acct.APIKey, body)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// nearest is up to n of the key's models that begin most like the one asked
// for — what to try instead of a mistyped id.
func nearest(ids []string, model string, n int) []string {
	type scored struct {
		id   string
		same int
	}
	var all []scored
	want := strings.ToLower(model)
	for _, id := range ids {
		have, same := strings.ToLower(id), 0
		for same < len(have) && same < len(want) && have[same] == want[same] {
			same++
		}
		if same >= 3 {
			all = append(all, scored{id, same})
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].same != all[j].same {
			return all[i].same > all[j].same
		}
		return all[i].id < all[j].id
	})
	out := []string{}
	for i := 0; i < len(all) && i < n; i++ {
		out = append(out, all[i].id)
	}
	return out
}

// httpError is a non-2xx answer: what it means, and the status for a caller
// that reads it differently (a model list that is not there).
type httpError struct {
	status int
	api    apiError
	err    error
}

func (e *httpError) Error() string { return e.err.Error() }
func (e *httpError) Unwrap() error { return e.err }

// call sends one request and returns a 2xx answer for the caller to read and
// close. What a moment can fix — a 429 that is not a quota, a 5xx, a
// dropped connection — is tried twice more, as the Anthropic SDK does;
// anything else comes back classified.
func (c *Client) call(ctx context.Context, method, url, key string, body []byte) (*http.Response, error) {
	header := http.Header{"Authorization": {"Bearer " + key}, "Accept": {"application/json, text/event-stream"}}
	if body != nil {
		header.Set("Content-Type", "application/json")
	}
	for attempt := 0; ; attempt++ {
		resp, err := c.chatHTTP.Send(ctx, method, url, header, body)
		var wait time.Duration
		retry := true
		switch {
		case err != nil && ctx.Err() != nil:
			return nil, ctx.Err()
		case err != nil:
			err = fmt.Errorf("%w: %v", ErrUnavailable, err)
		case resp.StatusCode/100 == 2:
			return resp, nil
		default:
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
			resp.Body.Close()
			api := parseError(resp.StatusCode, raw)
			err = &httpError{status: resp.StatusCode, api: api, err: api.classify()}
			retry = resp.StatusCode >= 500 || resp.StatusCode == 429 && !errors.Is(err, ErrQuota)
			if s, perr := strconv.Atoi(resp.Header.Get("Retry-After")); perr == nil && s > 0 {
				wait = min(time.Duration(s)*time.Second, 20*time.Second)
			}
		}
		if !retry || attempt == 2 {
			return nil, err
		}
		if wait == 0 {
			wait = c.retryBase() << attempt
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, ctx.Err()
		case <-t.C:
		}
	}
}

func (c *Client) retryBase() time.Duration {
	if c.backoff > 0 {
		return c.backoff
	}
	return time.Second
}

// apiError is what a provider said went wrong, in any of their shapes:
// {"error":{"message","type","code"}} (OpenAI and most), a list around it
// (Google), {"code","error":"…"} (xAI), {"message"} or {"detail"} (Mistral).
type apiError struct {
	status  int
	code    string
	message string
}

func parseError(status int, raw []byte) apiError {
	e := apiError{status: status}
	var v any
	if json.Unmarshal(raw, &v) == nil {
		if list, ok := v.([]any); ok && len(list) > 0 {
			v = list[0]
		}
		m, _ := v.(map[string]any)
		switch inner := m["error"].(type) {
		case map[string]any:
			m = inner
		case string:
			e.message = inner
		}
		for _, k := range []string{"code", "type", "status"} {
			switch x := m[k].(type) {
			case string:
				e.code += " " + x
			case float64: // OpenRouter's code is the status, mid-stream too
				if e.status == 0 {
					e.status = int(x)
				}
			}
		}
		for _, k := range []string{"message", "detail"} {
			if s, ok := m[k].(string); ok && e.message == "" {
				e.message = s
			}
		}
	}
	if e.message == "" {
		e.message = string(raw)
	}
	e.code = strings.TrimSpace(e.code)
	e.message = clip(strings.Join(strings.Fields(e.message), " "), 300)
	return e
}

func (e apiError) classify() error {
	low := strings.ToLower(e.code + " " + e.message)
	what := fmt.Sprintf("(%d: %s)", e.status, e.message)
	invalid := e.status == 400 || e.status == 422
	switch {
	case e.status == 402 || has(low, "insufficient_quota", "credit_balance", "credit balance", "insufficient balance", "insufficient credits", "spend_limit", "usage_limit_exceeded"):
		return fmt.Errorf("%w %s", ErrQuota, what)
	case e.status == 401 || e.status == 403:
		return fmt.Errorf("%w %s", ErrKey, what)
	case invalid && has(low, "api key", "api_key", "apikey") && has(low, "invalid", "incorrect", "not valid", "expired", "revoked"):
		return fmt.Errorf("%w %s", ErrKey, what) // xAI and Google answer a bad key with a 400
	case e.status == 404 || has(low, "model_not_found", "does not exist", "not a valid model", "unknown model", "no such model", "model not found"):
		return fmt.Errorf("%w %s", ErrModel, what)
	case e.status == 429 || e.status == 529 || has(low, "overloaded"):
		return fmt.Errorf("%w %s", ErrRateLimited, what)
	case invalid && has(low, "max_tokens", "max_completion_tokens", "max_output_tokens"):
		return fmt.Errorf("%w %s: %w", ErrUnavailable, what, errLimit)
	case invalid && has(low, "response_format", "json_schema", "structured output", "schema"):
		return fmt.Errorf("%w %s: %w", ErrUnavailable, what, errNoSchema)
	}
	return fmt.Errorf("%w %s", ErrUnavailable, what)
}

func has(s string, words ...string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && n < len(s) && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n] + "…"
}
