package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
)

// deploy/openapi.yaml is the §11.6 API9 source of truth for the public
// surface. These tests parse it structurally (docs/openapi-contract-
// reimplementation.md: no indentation scanner) and hold it to the code in
// three ways: every declared operation is mounted and every mounted route is
// declared; the literals clients would copy — cookie name, CSRF header, the
// sequence lower bound — equal the Go constants and the validator; and the
// document is internally consistent (every local $ref resolves, operationIds
// are unique). Response shapes are pinned by api_test.go on a real database.

func loadOpenAPI(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("openapi.yaml does not parse: %v", err)
	}
	return doc
}

// at walks a dotted path of map keys; missing → nil.
func at(v any, path ...string) any {
	for _, key := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[key]
	}
	return v
}

var httpMethods = map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true, "head": true, "options": true}

// operations returns "METHOD /path" for every operation in the document.
func operations(t *testing.T, doc map[string]any) []string {
	t.Helper()
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatal("openapi.yaml has no paths object")
	}
	var out []string
	for path, item := range paths {
		methods, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("path %s is not a mapping", path)
		}
		for method := range methods {
			if httpMethods[method] {
				out = append(out, strings.ToUpper(method)+" "+path)
			}
		}
	}
	sort.Strings(out)
	return out
}

func TestOpenAPIMatchesMountedRoutes(t *testing.T) {
	_, h := newSecurityServer(t, 0)
	mounted := append([]string(nil), h.patterns...)
	sort.Strings(mounted)
	declared := operations(t, loadOpenAPI(t))

	set := func(list []string) map[string]bool {
		m := map[string]bool{}
		for _, x := range list {
			m[x] = true
		}
		return m
	}
	m, d := set(mounted), set(declared)
	var missing, unexpected []string
	for _, op := range mounted {
		if !d[op] {
			missing = append(missing, op)
		}
	}
	for _, op := range declared {
		if !m[op] {
			unexpected = append(unexpected, op)
		}
	}
	if len(missing)+len(unexpected) > 0 {
		t.Errorf("openapi.yaml and cmd/api disagree\n  mounted but not declared (document or remove):\n    %s\n  declared but not mounted (mount or remove):\n    %s",
			strings.Join(missing, "\n    "), strings.Join(unexpected, "\n    "))
	}
	for _, op := range declared {
		_, path, _ := strings.Cut(op, " ")
		if path != "/healthz" && !strings.HasPrefix(path, "/v1/") {
			t.Errorf("%s: only /healthz may live outside /v1/ (§11.6 API9)", op)
		}
	}
	t.Logf("%d operations declared, %d routes mounted", len(declared), len(mounted))
}

// TestOpenAPIBindsSharedLiterals: the names a generated client would copy
// verbatim must be the ones the server reads.
func TestOpenAPIBindsSharedLiterals(t *testing.T) {
	doc := loadOpenAPI(t)
	schemes := at(doc, "components", "securitySchemes")
	if got := at(schemes, "sessionCookie", "name"); got != authz.SessionCookie {
		t.Errorf("sessionCookie name %v, server reads cookie %q", got, authz.SessionCookie)
	}
	if got := at(schemes, "sessionCookie", "in"); got != "cookie" {
		t.Errorf("sessionCookie in=%v, want cookie", got)
	}
	if got := at(schemes, "csrf", "name"); got != authz.CSRFHeader {
		t.Errorf("csrf header %v, server reads %q", got, authz.CSRFHeader)
	}
	if got := at(schemes, "csrf", "in"); got != "header" {
		t.Errorf("csrf in=%v, want header", got)
	}
	// The CSRF cookie has no scheme of its own (it is the double-submit
	// source, not a credential); the header's description must name it
	// and nothing in the document may rename it.
	if desc, _ := at(schemes, "csrf", "description").(string); !strings.Contains(desc, "`"+authz.CSRFCookie+"`") {
		t.Errorf("csrf description does not name the %q cookie: %q", authz.CSRFCookie, desc)
	}
	raw, _ := os.ReadFile(filepath.Join("..", "..", "deploy", "openapi.yaml"))
	for _, wrong := range []string{"atp_sess`", "atp_sess\"", "name: atp_sess\n", "atp-csrf", "X-Csrf-Token", "X-CSRF-TOKEN"} {
		if strings.Contains(string(raw), wrong) {
			t.Errorf("openapi.yaml contains %q — a misspelling of a shared literal", wrong)
		}
	}
	// Event.sequence: 1-based on the wire (reporter starts at 1, the
	// validator refuses 0 — TestEventSequenceBoundary in internal/ingest).
	seq := at(doc, "components", "schemas", "Event", "properties", "sequence")
	if got := at(seq, "minimum"); fmt.Sprint(got) != "1" {
		t.Errorf("Event.sequence minimum = %v, want 1", got)
	}
	if got := at(seq, "type"); got != "integer" {
		t.Errorf("Event.sequence type = %v, want integer", got)
	}
	required, _ := at(doc, "components", "schemas", "Event", "required").([]any)
	if !containsAny(required, "sequence") {
		t.Errorf("Event.sequence is not required: %v", required)
	}
}

// TestOpenAPIIsInternallyConsistent: every local $ref resolves and every
// operationId is unique — the two ways a generated client silently breaks.
func TestOpenAPIIsInternallyConsistent(t *testing.T) {
	doc := loadOpenAPI(t)
	ids := map[string]string{}
	var refs []string
	var walk func(node any, where string)
	walk = func(node any, where string) {
		switch v := node.(type) {
		case map[string]any:
			if ref, ok := v["$ref"].(string); ok {
				refs = append(refs, ref)
			}
			if id, ok := v["operationId"].(string); ok {
				if prev, dup := ids[id]; dup {
					t.Errorf("operationId %q used at %s and %s", id, prev, where)
				}
				ids[id] = where
			}
			for k, child := range v {
				walk(child, where+"/"+k)
			}
		case []any:
			for i, child := range v {
				walk(child, fmt.Sprintf("%s[%d]", where, i))
			}
		}
	}
	walk(doc, "")
	for _, ref := range refs {
		if !strings.HasPrefix(ref, "#/") {
			t.Errorf("non-local $ref %q", ref)
			continue
		}
		if at(doc, strings.Split(strings.TrimPrefix(ref, "#/"), "/")...) == nil {
			t.Errorf("$ref %q does not resolve", ref)
		}
	}
	ops := operations(t, doc)
	if len(ids) != len(ops) {
		t.Errorf("%d operationIds for %d operations — every operation needs one", len(ids), len(ops))
	}
	t.Logf("%d $refs resolve, %d unique operationIds", len(refs), len(ids))
}

func containsAny(list []any, want string) bool {
	for _, x := range list {
		if x == want {
			return true
		}
	}
	return false
}
