package identity

import "testing"

// TestRetryAndFlakyMapping locks down finding N41 against a REAL Playwright
// report containing retries, a genuinely flaky test, a timeout and a skip.
//
// Fixture generated with retries: 2 and timeout: 2000.
func TestRetryAndFlakyMapping(t *testing.T) {
	tests, err := LoadPlaywrightReport("testdata/playwright-report-retries.json")
	if err != nil {
		t.Fatal(err)
	}

	byTitle := map[string]LoadedTest{}
	for _, x := range tests {
		byTitle[x.Identity.Leaf()] = x
	}

	cases := []struct {
		title        string
		wantFinal    string
		wantAttempts []string
	}{
		{"flaky: fails once then passes", "flaky_pass", []string{"failed", "passed"}},
		{"always fails", "failed", []string{"failed", "failed", "failed"}},
		{"always passes", "passed", []string{"passed"}},
		{"skipped on purpose", "skipped", []string{"skipped"}},
		// The case that exposed the bug: Playwright reports test.status
		// "unexpected", but the attempts are timedOut. §8.4 keeps timed_out
		// distinct from failed, and §8.7's taxonomy needs that distinction.
		{"times out", "timed_out", []string{"timed_out", "timed_out", "timed_out"}},
	}

	for _, c := range cases {
		got, ok := byTitle[c.title]
		if !ok {
			t.Errorf("%q missing from loaded report", c.title)
			continue
		}
		if got.FinalStatus != c.wantFinal {
			t.Errorf("%q: finalStatus = %q, want %q", c.title, got.FinalStatus, c.wantFinal)
		}
		if len(got.AttemptStatus) != len(c.wantAttempts) {
			t.Errorf("%q: %d attempts, want %d", c.title, len(got.AttemptStatus), len(c.wantAttempts))
			continue
		}
		for i := range c.wantAttempts {
			if got.AttemptStatus[i] != c.wantAttempts[i] {
				t.Errorf("%q attempt %d: %q, want %q", c.title, i, got.AttemptStatus[i], c.wantAttempts[i])
			}
		}
	}
}

// TestFlakyComesFromPlaywrightNotUs: the reporter must carry Playwright's own
// flaky verdict through rather than re-deriving it from the attempt pattern.
// FlakySignal (§8.6) owns cross-run aggregation; this is the within-run call.
func TestFlakyComesFromPlaywrightNotUs(t *testing.T) {
	tests, err := LoadPlaywrightReport("testdata/playwright-report-retries.json")
	if err != nil {
		t.Fatal(err)
	}
	flaky := 0
	for _, x := range tests {
		if x.FinalStatus == "flaky_pass" {
			flaky++
			// Sanity: a flaky_pass must show at least one failed attempt
			// followed by a pass, or the mapping is lying.
			if len(x.AttemptStatus) < 2 {
				t.Errorf("flaky_pass with %d attempts is not credible: %v", len(x.AttemptStatus), x.AttemptStatus)
			}
			if x.AttemptStatus[len(x.AttemptStatus)-1] != "passed" {
				t.Errorf("flaky_pass whose last attempt is %q", x.AttemptStatus[len(x.AttemptStatus)-1])
			}
		}
	}
	if flaky != 1 {
		t.Errorf("want exactly 1 flaky_pass in the fixture, got %d", flaky)
	}
}
