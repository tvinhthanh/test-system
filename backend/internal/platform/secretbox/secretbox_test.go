package secretbox

import (
	"bytes"
	"errors"
	"testing"
)

func TestSealOpen(t *testing.T) {
	box := New([]byte("pepper-pepper-pepper-pepper-pepper"), "atp llm key v1")
	sealed := box.Seal("org-1", []byte("sk-ant-secret"))
	if bytes.Contains(sealed, []byte("sk-ant-secret")) {
		t.Fatal("plaintext visible in the sealed value")
	}
	got, err := box.Open("org-1", sealed)
	if err != nil || string(got) != "sk-ant-secret" {
		t.Fatalf("open: %q %v", got, err)
	}
	if _, err := box.Open("org-2", sealed); !errors.Is(err, ErrOpen) {
		t.Errorf("another row's sealed value opened: %v", err)
	}
	if _, err := New([]byte("pepper-pepper-pepper-pepper-pepper"), "atp totp secret v1").Open("org-1", sealed); !errors.Is(err, ErrOpen) {
		t.Errorf("another purpose's box opened it: %v", err)
	}
	if _, err := New([]byte("another-pepper-another-pepper-xx"), "atp llm key v1").Open("org-1", sealed); !errors.Is(err, ErrOpen) {
		t.Errorf("another pepper opened it: %v", err)
	}
	if _, err := box.Open("org-1", sealed[:5]); !errors.Is(err, ErrOpen) {
		t.Errorf("truncated value: %v", err)
	}
	if bytes.Equal(box.Seal("org-1", []byte("x")), box.Seal("org-1", []byte("x"))) {
		t.Error("two seals of one value are identical — nonce reused")
	}
}
