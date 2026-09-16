package pg

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestParseMigrationsOrdersAndValidates(t *testing.T) {
	good := fstest.MapFS{
		"0002_second.sql": {Data: []byte("select 2;")},
		"0001_first.sql":  {Data: []byte("select 1;")},
		"0003_third.sql":  {Data: []byte("select 3;")},
	}
	ms, err := ParseMigrations(good)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 3 || ms[0].Name != "first" || ms[2].Version != 3 || ms[1].SQL != "select 2;" {
		t.Errorf("unexpected parse: %+v", ms)
	}

	bad := map[string]fstest.MapFS{
		"gap": {
			"0001_a.sql": {Data: []byte("x")},
			"0003_c.sql": {Data: []byte("x")},
		},
		"does not start at 1": {
			"0002_b.sql": {Data: []byte("x")},
		},
		"duplicate version": {
			"0001_a.sql":     {Data: []byte("x")},
			"0001_a_bis.sql": {Data: []byte("x")},
		},
		"bad name": {
			"1_a.sql": {Data: []byte("x")},
		},
		"uppercase": {
			"0001_Tenancy.sql": {Data: []byte("x")},
		},
		"empty file": {
			"0001_a.sql": {Data: []byte("  \n")},
		},
	}
	for name, fsys := range bad {
		if _, err := ParseMigrations(fsys); err == nil {
			t.Errorf("%s: expected error", name)
		} else if !strings.HasPrefix(err.Error(), "migrate: ") {
			t.Errorf("%s: error lacks package prefix: %v", name, err)
		}
	}
}
