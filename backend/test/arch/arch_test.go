// Package arch enforces the dependency rules of SPEC §6.9.3 (§6.9.7: "by
// compiler and CI, not by goodwill").
//
// It reads the import graph of every package under internal/ straight from
// the source files, so it needs no build tags, no external tool, and no
// network. A violation names the offending package and the import, which is
// all a reviewer needs.
//
// QĐ-11 condition 4: this test must exist and run before the second domain
// module does. It is intentionally strict on day one — loosening a rule later
// is a one-line diff with a reason attached; tightening one after a quarter of
// drift is a rewrite.
package arch

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// modulePath must match go.mod. Read it from the file so the two cannot drift.
func modulePath(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root(t), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	t.Fatal("go.mod has no module line")
	return ""
}

// root is the backend/ directory: two levels up from test/arch.
func root(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

// graph maps each package under internal/ (as a path relative to internal/,
// e.g. "platform/config" or "ingest") to the set of internal packages it
// imports, in the same form.
func graph(t *testing.T) map[string]map[string]bool {
	t.Helper()
	mod := modulePath(t)
	prefix := mod + "/internal/"
	internalDir := filepath.Join(root(t), "internal")

	g := map[string]map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(internalDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, _ := filepath.Rel(internalDir, filepath.Dir(path))
		pkg := filepath.ToSlash(rel)
		if g[pkg] == nil {
			g[pkg] = map[string]bool{}
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(p, prefix) {
				g[pkg][strings.TrimPrefix(p, prefix)] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// module returns the top-level module a package belongs to: "platform" for
// "platform/config", "ingest" for "ingest/internal/store".
func module(pkg string) string {
	return strings.SplitN(pkg, "/", 2)[0]
}

// Layer per §6.9.3. Anything under internal/ that is not platform, tenancy,
// authz or query is a domain module.
func layer(pkg string) string {
	switch m := module(pkg); m {
	case "platform", "tenancy", "authz", "query":
		return m
	default:
		return "domain"
	}
}

// forbiddenNames is §6.9.3 rule 2 and §6.9.12 item 1: the shared-junk-drawer
// package is the number-one killer of modular monoliths, no exceptions.
var forbiddenNames = map[string]bool{
	"common": true, "models": true, "model": true, "entity": true, "entities": true,
	"utils": true, "util": true, "shared": true, "helpers": true, "helper": true,
}

func TestNoJunkDrawerPackages(t *testing.T) {
	for pkg := range graph(t) {
		for _, seg := range strings.Split(pkg, "/") {
			if forbiddenNames[seg] {
				t.Errorf("internal/%s: package name %q is forbidden by §6.9.3 rule 2 — business types belong to the module that owns them", pkg, seg)
			}
		}
	}
}

func TestLayerRules(t *testing.T) {
	for pkg, imports := range graph(t) {
		for imp := range imports {
			from, to := layer(pkg), layer(imp)
			switch from {
			case "platform":
				if to != "platform" {
					t.Errorf("internal/%s imports internal/%s: platform/* may import nothing else under internal/ (§6.9.3)", pkg, imp)
				}
			case "tenancy", "authz":
				if to != "platform" {
					t.Errorf("internal/%s imports internal/%s: tenancy and authz may import only platform/* (§6.9.3)", pkg, imp)
				}
			case "query":
				if to != "platform" && to != "tenancy" {
					t.Errorf("internal/%s imports internal/%s: query may import only platform/* and tenancy (§6.9.3)", pkg, imp)
				}
			case "domain":
				if to == "query" {
					t.Errorf("internal/%s imports internal/%s: domain modules do not read the dashboard read model (§6.9.3)", pkg, imp)
				}
				if to == "domain" && module(imp) != module(pkg) && strings.Contains(imp, "/") {
					// The compiler already blocks <domain>/internal/...; this catches
					// any other sub-package that is not the single contract file.
					t.Errorf("internal/%s imports internal/%s: only the root package of another domain is importable (§6.9.4)", pkg, imp)
				}
			}
		}
	}
}

func TestDomainFanOutAtMostThree(t *testing.T) {
	fan := map[string]map[string]bool{}
	for pkg, imports := range graph(t) {
		if layer(pkg) != "domain" {
			continue
		}
		m := module(pkg)
		for imp := range imports {
			if layer(imp) == "domain" && module(imp) != m {
				if fan[m] == nil {
					fan[m] = map[string]bool{}
				}
				fan[m][module(imp)] = true
			}
		}
	}
	for m, others := range fan {
		if len(others) > 3 {
			names := make([]string, 0, len(others))
			for o := range others {
				names = append(names, o)
			}
			sort.Strings(names)
			t.Errorf("internal/%s imports %d other domains (%s): the limit is 3 (§6.9.3 rule 3) — this module is doing an orchestrator's job; split it or lift the coordination into cmd/", m, len(others), strings.Join(names, ", "))
		}
	}
}

func TestNoCyclesBetweenModules(t *testing.T) {
	// Collapse the package graph to the module graph, then look for a cycle.
	edges := map[string]map[string]bool{}
	for pkg, imports := range graph(t) {
		m := module(pkg)
		for imp := range imports {
			if n := module(imp); n != m {
				if edges[m] == nil {
					edges[m] = map[string]bool{}
				}
				edges[m][n] = true
			}
		}
	}
	const (
		white = iota
		grey
		black
	)
	color := map[string]int{}
	var stack []string
	var visit func(string) bool
	visit = func(n string) bool {
		color[n] = grey
		stack = append(stack, n)
		for next := range edges[n] {
			switch color[next] {
			case grey:
				cycle := append(append([]string(nil), stack[indexOf(stack, next):]...), next)
				t.Errorf("dependency cycle between modules: %s (§6.9.3 rule 1 — reverse one edge with an event, not a callback)", strings.Join(cycle, " -> "))
				return true
			case white:
				if visit(next) {
					return true
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
		return false
	}
	nodes := make([]string, 0, len(edges))
	for n := range edges {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	for _, n := range nodes {
		if color[n] == white {
			visit(n)
		}
	}
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return 0
}

// TestGraphIsNotEmpty guards the guard: if the walker ever silently finds
// nothing (moved directory, wrong root), every rule above passes vacuously.
func TestGraphIsNotEmpty(t *testing.T) {
	g := graph(t)
	if _, ok := g["platform/config"]; !ok {
		t.Fatalf("expected internal/platform/config in the graph; got %d packages — is the test running from backend/test/arch?", len(g))
	}
}
