package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestProvidersTable(t *testing.T) {
	seen := map[string]bool{}
	prefixes := map[string]string{}
	for _, p := range append(Providers(), custom) {
		if p.ID == "" || p.Label == "" || seen[p.ID] {
			t.Errorf("provider %+v: id and label, once", p)
		}
		seen[p.ID] = true
		if p.ID != Custom && !strings.HasPrefix(p.Console, "https://") {
			t.Errorf("%s: console %q", p.ID, p.Console)
		}
		if p.wire == wireChat && (p.ID != Custom && !strings.HasPrefix(p.baseURL, "https://") || p.maxField == "") {
			t.Errorf("%s: chat needs an https host and a limit field: %+v", p.ID, p)
		}
		if p.usage && !p.stream {
			t.Errorf("%s: usage option without streaming", p.ID)
		}
		if p.DefaultModel != "" && !ValidModel(p.ID, p.DefaultModel) {
			t.Errorf("%s: default %q is not a model it takes", p.ID, p.DefaultModel)
		}
		for _, m := range p.Models {
			if m.Label == "" || m.Note == "" || !ValidModel(p.ID, m.ID) {
				t.Errorf("%s: model %+v", p.ID, m)
			}
		}
		for _, pre := range p.KeyPrefixes {
			if other, ok := prefixes[pre]; ok {
				t.Errorf("prefix %q is %s's and %s's", pre, other, p.ID)
			}
			prefixes[pre] = p.ID
		}
	}
	if !seen[Anthropic] || !seen["openai"] || len(Providers()) < 8 {
		t.Errorf("providers: %v", seen)
	}
}

func TestGuessAndValidate(t *testing.T) {
	for key, want := range map[string]string{
		"sk-ant-api03-abc": Anthropic, "sk-proj-abc": "openai", "sk-svcacct-abc": "openai", "sk-or-v1-abc": "openrouter",
		"xai-abc": "xai", "gsk_abc": "groq", "AIzaSyAbc": "gemini", "sk-abc": "", "": "", "abcdef": "",
	} {
		if got := GuessProvider(key); got != want {
			t.Errorf("GuessProvider(%q) = %q, want %q", key, got, want)
		}
	}
	for _, tc := range []struct {
		provider, model string
		ok              bool
	}{
		{Anthropic, "claude-opus-5-5", true}, {"", "claude-haiku-5-5", true}, {Anthropic, "claude-opus-4-1", false}, {Anthropic, "gpt-6-astra", false},
		{"openai", "gpt-6-astra", true}, {"openai", "gpt-4.1", true}, {"openai", "bad id!", false}, {"openai", "", false},
		{"openrouter", "openai/gpt-6-astra", true}, {Custom, "llama3.1:8b", true}, {"nope", "x", false},
	} {
		if got := ValidModel(tc.provider, tc.model); got != tc.ok {
			t.Errorf("ValidModel(%q, %q) = %v", tc.provider, tc.model, got)
		}
	}
	for model, want := range map[string]string{"gpt-6-astra": "openai", "claude-haiku-5-5": Anthropic, "grok-4.7": "xai", "something-else": Anthropic} {
		if got := ProviderOf(model); got != want {
			t.Errorf("ProviderOf(%q) = %q", model, got)
		}
	}
	if Label("openai") != "OpenAI (GPT)" || Label("") != "Anthropic (Claude)" || Label("nope") != "nope" {
		t.Errorf("labels: %q %q %q", Label("openai"), Label(""), Label("nope"))
	}
}

func TestServerConfiguration(t *testing.T) {
	for _, tc := range []struct {
		provider, model, want string
		ok                    bool
	}{
		{"", "", "claude-opus-5-5", true}, {"openai", "", "gpt-6-astra", true}, {"openai", "gpt-6-luna", "gpt-6-luna", true},
		{"openrouter", "", "", false}, {"openrouter", "openai/gpt-6-astra", "openai/gpt-6-astra", true},
		{Custom, "", "", false}, {Custom, "llama3.1:8b", "llama3.1:8b", true},
		{Anthropic, "gpt-6-astra", "", false}, {"nope", "", "", false},
	} {
		got, err := ServerModel(tc.provider, tc.model)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("ServerModel(%q, %q) = %q, %v", tc.provider, tc.model, got, err)
		}
	}
	for _, tc := range []struct {
		provider, baseURL string
		ok                bool
	}{
		{"", "", true}, {"openai", "", true}, {"openai", "https://proxy.example/v1", true}, {Custom, "http://ollama:11434/v1", true},
		{Custom, "", false}, {Custom, "ftp://x", false}, {"nope", "", false},
	} {
		if _, err := New(tc.provider, tc.baseURL); (err == nil) != tc.ok {
			t.Errorf("New(%q, %q): %v", tc.provider, tc.baseURL, err)
		}
	}
	// Only the server's provider is pointed elsewhere; the others keep their
	// hosts, and the outbound client refuses anything but those.
	c := mustNew(t, "openai", "https://proxy.example/v1")
	if p, _ := Find("gemini"); c.endpoint(p) != p.baseURL {
		t.Errorf("gemini moved: %s", c.endpoint(p))
	}
	if p, _ := Find("openai"); c.endpoint(p) != "https://proxy.example/v1" {
		t.Errorf("openai not moved: %s", c.endpoint(p))
	}
	if _, err := c.JSON(context.Background(), Request{Account: Account{Provider: "nope", APIKey: "k", Model: "m"}}); !errors.Is(err, ErrModel) {
		t.Errorf("unknown provider: %v", err)
	}
}
