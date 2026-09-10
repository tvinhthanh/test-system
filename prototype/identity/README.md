# Test Identity Prototype — round 1

Hướng dẫn tiếng Việt cho người mới: [Học Go qua project](../../docs/go/README.md) — cài đặt, chạy CLI/test, đọc code và kiến trúc backend dự kiến.

> The only code SPEC §5.7 permits during Phase 0 (see §4.3, week 3).
> Run **2026-08-26**, week 1 — two weeks ahead of schedule.
> **Synthetic corpus.** §9.6 item 1 still requires >= 500 **real** test results from >= 2 design partners in week 3.

## Running

```bash
go test ./...                                  # every finding below is locked by a test
go run ./cmd/protoident                        # the §9.6 measurement
go run ./cmd/protoident -diag                  # why auto-match did or did not fire
go run ./cmd/protoident -visibility            # N39/N40: accuracy vs run visibility
go run ./cmd/protoident -age                   # N45: staleness vs decision
go run ./cmd/protoident -scale                 # N47: fuzzy-path cost vs project size
go run ./cmd/protoident -recency-gate=30       # N45: try the staleness gate
go run ./cmd/protoident -suite=2000 -seed=99   # parameter sweep
```

The sweeps used to be `_test.go` files that only called `t.Log`. They asserted
nothing, so they were not tests — they borrowed the test runner as a CLI. They
are flags now.

## Tuning

Every parameter in §9.3 lives in a `Config` value:

```go
cfg := identity.DefaultConfig().WithRecencyGate(30)
r := &identity.Resolver{Aliases: a, Candidates: c, Config: cfg}
```
Runnable version, checked by the compiler: `ExampleConfig` in `example_test.go`.A fenced block in a README can rot silently; an Example cannot.

These were package-level variables until the layout was cleaned up. That was a
defect, not a style question: tests wrote to them and restored with `defer`, so
a test failing part-way left its thresholds applied to whatever ran next, and
nothing could run in parallel. `Config` is passed, never mutated.

## Result: both thresholds met

```
PRECISION : 1.0000   (§9.6 needs >= 0.98)  PASS  — zero wrong merges
RECALL    : 0.9920   (§9.6 needs >= 0.80)  PASS
```

Stable across every variable swept. Re-measured 2026-09-09; every row below
is reproducible with the command beside it, because a table nobody can
regenerate is a table that rots (N53).

| Variable | Range | precision | recall | Reproduce with |
|---|---|---|---|---|
| Gap threshold | 0.02 → 0.20 | 1.0000 | **0.9920, identical at all five** | `-gap=0.02` … `-gap=0.20` |
| Seed | 1 · 7 · 99 · 12345 | 1.0000 | 0.9880 – 0.9960 | `-seed=99` |
| Density | 11 → 285 tests/file | 1.0000 | 0.9750 – 1.0000 | `-suite=77 -mutate=8` … `-suite=1995 -mutate=200` |
| **Rename rate** | **1% → 80% of corpus** | **1.0000** | **0.9867 – 1.0000** | `-suite=896 -mutate=9` … `-mutate=717` |

The corpus spans 7 spec files, so density is `-suite / 7`. Rename rate is
`-mutate / -suite` — which is why the two bottom rows have to be swept
*separately*: the old sweep held `-mutate=50` fixed while `-suite` varied, so
its "density 11" row was also "65% of the corpus renamed at once". Two
variables moving together, reported as one.

Swept apart, neither shows a trend — the movement is seed noise. That makes
the real claim stronger than the one this table used to make: identity
resolution is insensitive to density **and** to rename rate, and precision
holds at 1.0000 even when 80% of the suite is renamed in a single run. That
last case is the large-refactor scenario, which is exactly when identity is
most likely to break and most expensive to get wrong.

## N34: the presence signal is load-bearing

The signal *"candidate still appears in this run under its old fingerprint -> score x0.3"* (§9.3) is **not a minor tweak**. It is what makes the algorithm work at all:

| Configuration | recall |
|---|---|
| Signal disabled | **0.128** |
| Signal active | **0.992** |

Mechanism: when test X is renamed, every test that resembles X **still runs** under its old fingerprint and takes the penalty, dropping to ~0.28. Only X is absent, so only X earns the +0.05 bonus. The right candidate stands clear and `MinGap` stops being the binding constraint.

Without it, same-file tests score within 0.027 of each other and `MinGap >= 0.10` blocks 87% of cases.

**Consequence:** this is an invariant of §9.3. Do not remove or weaken it during later performance work. `presence_test.go` locks it.

## Note on the first measurement round

The first run reported recall 0.128 and I wrongly concluded §9.3 had a design flaw (filed as N32/N33). The real cause: the harness set `PresentInRun = false` for **every** candidate, modelling a world where all 500 tests vanished at once. With presence modelled correctly — only the renamed test absent — the result reversed completely. N32 and N33 are retracted in SPEC.

Worth remembering, because it will recur: **when a prototype produces an implausibly bad result, suspect the harness before the design.**

## Playwright loader — built and checked against real output

`playwright.go` reads the Playwright JSON reporter. The schema came from **real reports**, not guesswork: two fixtures in `testdata/`, produced by running Playwright and then renaming one test and moving one file.

Three ways the real schema differs from what the spec assumed:

### N35 — parameters live in the title, not in `projectName`

Playwright's most common idiom:

```ts
for (const amount of [10, 20]) {
  test(`applies discount for ${amount}`, ...)
}
```

produces two specs with **different titles** and an **identical** `paramSignature`. §9.2 does not capture this, and §9.5's `TemplateKey` (hash of `filePath + titlePath`) cannot group them either, because the titles differ. So "dynamic values in titles", which §9.7 files as a v2 backlog item, **is the standard idiom, not a rare edge case.**

**Fix implemented and measured** (`dynamic_title.go`): normalise dynamic runs — integers, decimals, UUIDs, hex, ISO dates, emails — to placeholders, and key the template off the normalised title. On the real fixture:

```
distinct TemplateKey           : 7
distinct TemplateKeyNormalized : 6   <- the two loop variants now group
distinct Fingerprint           : 14  <- identity untouched
```

**The boundary that must never move:** this normalisation applies to the template key only, **never to the fingerprint**. The two loop variants are genuinely different tests; they need separate identities and separate flaky histories. Folding them together at fingerprint level would be precisely the wrong-merge failure §9.6 ranks worst.

Known trade-off: `handles 404` and `handles 500` also group under `handles <num>`. For dashboard grouping that is usually wanted; if a suite relies on numeric distinctions being meaningful, this will surprise. Verify on partner data before promoting it out of the prototype.

### N36 — different status vocabulary

Playwright uses `expected | unexpected | skipped | flaky`, not `passed | failed`. `mapStatus()` converts to the §8.4 enum. The important part: **Playwright determines `flaky` itself** within a run. Responsibilities must stay split — Playwright decides flakiness *within* a run, `FlakySignal` (§8.6) aggregates *across* runs. The reporter carries the value through; it must not re-derive it.

### N37 — `spec.id` is not a durable identity

Shape: `<file-hash>-<test-hash>`. Measured:

```
rename one test + move one file  ->  spec.id broke on 6/14 entries
                                     fp1 fingerprint broke on 6/14 entries
```

Exactly as fragile as a naive fingerprint. The one difference: our fingerprint has a **recovery path** through §9.3 steps 2-4; `spec.id` has none. Recorded because it is the first thing anyone reaches for — *"Playwright already has an id."*

One more detail worth keeping: the file-level suite carries the **file path as its title, with backslashes on Windows**. Read `file`, not `title`, or the fingerprint becomes OS-dependent.

## N39: partial run visibility forces two-stage resolution

The N34 signal asks *"is this candidate still present in the run?"*. Answering it needs to know which tests ran — but §7.6 resolves identity **inline with ingest at p50 < 1s**, long before the run finishes. At that moment a test that has **not reported yet** is indistinguishable from one that has **disappeared**.

Three real situations produce a partial view: sharding (`--shard=1/4`), filtered runs (`--grep`, `--project`, changed-files-only), and the 500-event batching in §7.2 itself.

Measured (`visibility.go`):

```
  visible     precision    recall     auto    WRONG   review
  1.00           1.0000    0.9920      248        0        2
  0.90           1.0000    0.7520      188        0       62
  0.75           1.0000    0.5200      130        0      120
  0.50           1.0000    0.3000       75        0      175
  0.25           1.0000    0.1920       48        0      202
  0.10           1.0000    0.1400       35        0      215
```

Re-measured 2026-09-10 at the §9.6 rename set of 50. The four partial-visibility
rows each moved by exactly one case from what this table used to show — 0.75,
0.50, 0.25 and 0.10 all gained one auto-match. That is the N49 trigram fix, which
landed after this table was written and was never re-run against it (N57).

The direction is the point. N49 predicted the old copy scored **lower** than
`pg_trgm`, so correcting it lets one more candidate clear the 0.4 filter — at
every level, in the same direction. N49 recorded this as a single row moving
"one case in 200" and read it as rounding noise. It is not noise; it is the
systematic shift N49's own mechanism predicts, and seeing it on four rows is
better evidence for that analysis than the one row it reported.

### N40: the gap rule turns out not to be redundant

The striking column is precision: **1.0000 at every visibility level, including 10%.** Zero wrong merges anywhere.

That is `MinGap` doing work nobody credited it for. After N34 it looked vestigial — gap thresholds from 0.02 to 0.20 gave identical results at full visibility. Under partial visibility it becomes the safety net: when many candidates all look absent they all score alike, none stands clear of the runner-up, and auto-match declines to fire.

So the two mechanisms are complementary, not overlapping:

| Mechanism | Provides |
|---|---|
| Presence signal (x0.3) | **Discrimination** — separates the right candidate from its near-twins |
| `MinGap` rule | **Safety** — refuses to guess when presence information is unreliable |

Neither may be removed.

### The fix: resolve in two stages

Partial visibility costs automation, not safety. But the cost is severe — at 50% visibility 175 of 250 renames land in the Review Queue, far past the alert threshold of 50 in §6.4.

Measured share of each path:

```
exact-match (inline, needs no presence) : 500/550 = 90.91%
fuzzy       (needs full-run presence)   :  50/550 =  9.09%
```

And that 9% is a stress figure — the corpus was given a 9% rename rate. Real suites rename far less often, so the hot path absorbs well over 99% in practice, matching §9.3's own claim.

So deferring only the fuzzy path is nearly free:

- **Stage 1 — inline, p50 < 1s:** exact alias match only. No presence information required.
- **Stage 2 — at `run.finish`:** fuzzy matching from §9.3 steps 2-4, once the run is fully visible.

Confirmed by measurement: `inline (50% visible) recall 0.2720 -> deferred (run complete) recall 0.9880`.

This fits the "backfill" language §7.6 already uses; it only had to say *when* the backfill happens for the fuzzy path. SPEC §7.6 now specifies both stages.

## N41: two status vocabularies, and the spec only mapped one

Generating a report with `retries: 2` and a genuinely flaky test exposed a bug in my own loader.

Playwright reports status at **two levels, with different enums**:

```
test.status      : expected | unexpected | skipped | flaky
results[].status : passed | failed | timedOut | skipped | interrupted
```

§8.4's `finalStatus` includes `timed_out` and `interrupted` — and **neither exists at test level**. Real output:

| title | test.status | results[] | correct finalStatus |
|---|---|---|---|
| flaky: fails once then passes | `flaky` | failed → passed | `flaky_pass` |
| always fails | `unexpected` | failed ×3 | `failed` |
| times out | `unexpected` | **timedOut ×3** | **`timed_out`** |
| skipped on purpose | `skipped` | skipped | `skipped` |

My first `mapStatus` read only `test.status`, so the timeout came out as `failed`:

```
old mapping: "failed"  ->  fixed mapping: "timed_out"
```

### Why this mattered more than it looks

§8.7's failure taxonomy has a `flaky_timing` category, and telling a timeout apart from an assertion failure is core product function — it is most of what distinguishes "the app is slow" from "the app is broken". Collapsing timeouts into `failed` at ingest means AI triage (§1.4) never sees that signal, and no amount of downstream cleverness recovers it.

The fix reads both levels and also models attempts individually, as §8.4 requires: `AttemptStatus []string` carries one entry per retry. `retries_test.go` pins the mapping against the real fixture; `regression_test.go` demonstrates the old mapping's failure so nobody reintroduces it.

### N36 confirmed empirically

Playwright genuinely does compute `flaky` itself — `test.status = "flaky"` with attempts `failed → passed`. The reporter carries that verdict through and never re-derives it. `FlakySignal` (§8.6) owns cross-run aggregation; this is the within-run call and it belongs to Playwright.

## N47: the candidate filter has no index behind it

§9.3 step 2 filters candidates on three conditions — same `canonicalFilePath`, same leaf title, or trigram similarity >= 0.4 — and §8.9's index list covers **none of them**.

Measured in this prototype, which has no index at all. Three runs of
`go run ./cmd/protoident -scale` on 2026-09-09, full range across all three:

```
  project size   per unknown fp          50 renames
  500            3.3 -   5.0ms           163 -   252ms
  2000          13.7 -  21.7ms           683ms -  1.09s
  10000         70.8 - 136.2ms           3.54 -  6.81s
  25000        165.1 - 610.2ms           8.25 - 30.51s
```

**Read the magnitude, not the digits — and this README used to say that while
quoting digits far tighter than the data supports.** The previous table gave
`25000 -> 18.7s (16.9-19.7)` and claimed the numbers "move by about a sixth
between runs". They move by **3.7x** at that size. Filed as N55.

The growth exponent is not stable enough to state either. The 500-to-25000
ratio per unknown fingerprint came out **51x, 121x and 125x** across the three
runs. The old claim of "superlinear, about 107x" is one draw from that spread,
and `scale_sweep.go` was simultaneously printing "Cost is linear in project
size" — the tool and the document describing it disagreed. The tool now states
the mechanism (a linear scan over every active identity) and shows the three
observed ratios without asserting an exponent.

None of this moves the conclusion, which is the point of recording it honestly:
at 8.3s or at 30.5s, §8.9 is still missing the indexes §9.3 depends on.

Being honest about what this does and does not show: the prototype does a linear scan, so these are an **upper bound**, not a prediction of production behaviour. With a GIN trigram index Postgres does an index lookup instead. The finding is not "this will be slow" — it is "§8.9 is missing exactly the indexes §9.3 depends on, and here is the cost of that omission."

Two consequences went into the spec:

- Three indexes added to §8.9, plus a `canonicalLeafTitle` column in §8.5. A GIN index cannot address one element of a `text[]`, so the leaf has to be denormalised into a real column.
- §9.3's 50-candidate cap limits **scoring** cost, not **filtering** cost — the cap applies after the scan. Only an index removes the scan.

## N48: the hot path is comfortably inside its budget

§7.6 puts p50 < 1s on **stage 1 only** — the inline exact-match path. Stage 2 runs at `run.finish` and carries no deadline; that separation is the whole point of what N39 forced.

```
suite of 10000 tests, 10 renames
  stage 1 (inline, exact match) : 7-17ms for 10000 events (0.7-1.7 µs/event)
  stage 2 (run.finish, fuzzy)   : ~750ms for 10 events (~75 ms/event)
```

Stage 1 clears its budget by two orders of magnitude, which is what makes the two-stage design affordable.

### The measurement itself had a bug

This section used to report a flat `0.8 µs/event, 8ms`. Across four runs the real figures are 0.7, 0.9, 1.4 and 1.7 µs/event — a 2.4x spread reported as a constant.

Worse, the assertion behind it summed both stages and compared the total against stage 1's 1s budget. Measured, that total sat at 758-796ms. A gate with 20% headroom over a number that swings ±15% between runs is a flaky test — inside the prototype for a product about test flakiness.

`TestTwoStageLatencyBudget` now asserts them separately: stage 1 against §7.6's 1s, and stage 2 against a deliberately loose 5s that only trips on an algorithmic regression, since ~780ms is the unindexed upper bound N47 already characterised.

## N49: the trigram function was not the one Postgres runs

§9.3 filters candidates on `trigram similarity >= 0.4` and names `pg_trgm` as the implementation. This prototype had its own copy of that function, and the copy was wrong in a way that is easy to miss: it lowercased the string and padded **the whole string** with two leading and one trailing space before slicing 3-grams.

PostgreSQL's `generate_trgm()` cuts the string into words first. Anything that is not a letter or a digit is a separator, and each word is padded on its own:

```
  "two words"
    pg_trgm    "  t" " tw" "two" "wo "   "  w" " wo" "wor" "ord" "rds" "ds "
    old copy   "  t" " tw" "two" "wo "   "o w" " wo" "wor" "ord" "rds" "ds "
                                          ^^^^^ straddles the space
                                          and "  w" is missing entirely
```

A trigram spanning a separator stops matching as soon as the two strings differ in word count — which is exactly the situation a rename detector is built for. Measured across 100,000 leaf-title pairs from the §9.6 corpus:

```
  mean |difference|                     0.0126
  max  |difference|                     0.0544
  pairs that changed side of the 0.4 filter    12  (0.012%)
```

The sign matters more than the size. The old copy scored **lower** than `pg_trgm` on the pairs where they disagreed, so a threshold tuned here would admit **more** candidates once it ran against real Postgres, not fewer. File paths are the worst case, since `/` and `.` are both separators.

Rewriting `trigrams()` to match `generate_trgm()` left the headline numbers alone — precision 1.0000, recall 0.9920, exact-match share 90.91%, deferred recall 0.9880 — with half-visibility recall moving one case, 0.2960 → 0.3000. So 0.4 survives as a threshold. (It moved the 0.75, 0.25 and 0.10 rows by one case each too, in the same direction — this section originally reported only the half-visibility row, and the visibility table above went un-regenerated for it. See N57.) What changed is that the number now describes something the database will actually compute.

`similarity_test.go` pins the generator against hand-computed trigram sets and asserts the specific defect is gone: no trigram may straddle a separator, and every word must get its own leading padding.

## N50: two of the three scoring functions cannot run in Postgres

§2 justifies Go for the backend and adds that §9.3's "Levenshtein/Jaccard/trigram run in Postgres via `pg_trgm` + `fuzzystrmatch`". Two of those three do not.

**Jaccard has no Postgres function here.** `pg_trgm.similarity()` is Jaccard — over the *trigrams of one string*. `S_title` needs Jaccard over the *set of enclosing describe blocks*, which is a different thing on a `text[]`. Postgres ships nothing for it.

**`fuzzystrmatch.levenshtein()` errors past its length limit** rather than degrading. The whole scoring statement fails, taking every candidate in it. Measured against realistic titles:

```
  typical short title        21 runes   21 bytes
  long Vietnamese title     131 runes  183 bytes
  data-driven JSON title    195 runes  195 bytes
  two fixture rows          391 runes  391 bytes   over 255 either way
```

The data-driven title is the N35 idiom — `test(\`rejects order ${JSON.stringify(row)}\`)` — and it already sits at 76% of the limit. A matrix test over two payloads clears it outright.

**What I could not verify here:** the documentation says 255 *characters*, the error message in the source says *bytes*, and the check is applied to a byte length. For the Vietnamese title above that is the difference between 255 characters of headroom and about 85. Settling it needs a real Postgres, so it belongs in CI alongside the `-race` run from N46.

**The measurement that makes the question moot.** §9.3 step 3 scores at most 50 candidates, and §9.7 expects under 10 unknown fingerprints in a normal run:

```
  scoring the full 50-candidate cap in Go : 348 - 587µs   (6 runs)
    per candidate                         : 7.0 -  11.7µs
    10 unknown fingerprints in a run      : 3.48 - 5.87ms
```

This block used to read `463µs / 9.3µs / 4.6ms` as three flat constants. Those
are real — one run lands on 465µs / 9.3µs / 4.654ms almost exactly — but the
spread is **1.7x**, so a single sample presented as a constant is the same
defect N48 already caught one section above, left uncorrected here. Filed as
N54.

At the worst end of the range, 5.9ms against §7.6's 1s budget. There is no performance argument for expressing step 3 as SQL, so the split is: **step 2 filters in Postgres** — that is where N47's GIN index earns its place — and **step 3 scores in the application**. `pgparity_test.go` holds both measurements and fails if scoring a run ever costs more than 50ms.

## N56: the harness that kept regenerating the number N51 deleted

N51 found `recall 0.9950` copied into five places in SPEC, replaced all five with
the measured `0.9920`, and wrote a rule: §9.6 is the only place that holds a
headline number. What it did not find was the thing still producing 0.9950.

`visibility_test.go` called `VisibilitySweep(500, 40, 42, "")`. §9.6 item 2 fixes
the rename set at **50** tests × 5 mutations = 250 trials, and `cmd/protoident`
defaults to exactly that. The test used 40 — 200 trials — and 199 of 200 is
**0.9950**:

```
-mutate=40 (what the test hardcoded)     1.00   1.0000   0.9950   199   0    1
-mutate=50 (what §9.6 specifies)         1.00   1.0000   0.9920   248   0    2
```

So every `go test ./...` printed `full visibility recall 0.9950` while the CLI
next to it printed 0.9920 for the same row. Two harnesses, two denominators, one
spec number.

N51 diagnosed the 0.9950 as the density sweep's number "lifted into the
headline". That was wrong, and being wrong about the source is why the round
removed every copy and left the generator running. The pair in SPEC read
`0.995 → 0.295`; N51 concluded it "never matched the prototype's own table",
but `0.9950 → 0.3000` matches this harness exactly. The pair was not invented —
it was read off a sweep nobody realised was measuring something else.

`gate_test.go` had the same 40, under a comment calling it "the main §9.6
benchmark".

**Fix:** `SpecRenameSetSize = 50` in `synth.go`, used by all three call sites, so
the §9.6 rename set is named once and a sweep that varies it has to say so.
After it, the tests print what the spec says:

```
full visibility recall 0.9920 -> half visibility recall 0.3000
30-day gate on fresh candidates: precision 1.0000 recall 0.9920 — unchanged
```

That half-visibility 0.3000 is the number SPEC's N39 already carried, now
produced by a harness measuring the set §9.6 defines rather than agreeing with
it by coincidence.

## Week 3 (once partners are on board)

- [ ] Collect Playwright JSON reporter exports from >= 2 partners, >= 500 test results
- [ ] Point `LoadPlaywrightReport` at the real files instead of `BaseSuite`
- [ ] Confirm the two-stage split holds on real traffic: what share of events actually hit the exact-match path?
- [ ] Check the load-bearing assumption: in a real run, does a renamed test genuinely **disappear**? If teams use `test.skip` instead of deleting, the N34 signal weakens
- [ ] Measure the real rename rate per month — if it is very low, a manual Review Queue suffices and elaborate auto-matching is not worth it
- [ ] Check whether `TemplateKeyNormalized` over-groups on real suites
- [ ] Record the outcome in the Gate-0 decision record

## Layout

| File | Contents |
|---|---|
| `config.go` | Every §9.3 tunable, passed as a value rather than mutated globally |
| `fingerprint.go` | §9.2 — normalisation and fingerprint. `U+0000` separator, locked by test (N22) |
| `similarity.go` | Rune-wise Levenshtein, Jaccard, and `pg_trgm` trigram similarity (N49) |
| `score.go` | §9.3 step 3 — four components plus two reinforcement signals |
| `resolve.go` | §9.3 steps 1-4 |
| `dynamic_title.go` | N35 fix — dynamic-value normalisation for template grouping |
| `visibility.go` | N39/N40 — accuracy vs how much of a run the resolver can see |
| `synth.go` | §9.6 item 2 — five-mutation rename corpus; `SpecRenameSetSize` pins the rename set (N56) |
| `eval.go` | Precision/recall measurement |
| `diag.go` | Diagnosis when auto-match does not fire |
| `playwright.go` | JSON reporter loader, schema verified against real output |
| `testdata/` | Three real Playwright fixtures: before/after rename+move, plus a retries+flaky+timeout run |
| `fingerprint_test.go` | Separator and normalisation invariants |
| `presence_test.go` | N34 — the load-bearing signal |
| `playwright_test.go` | N35, status mapping, template grouping on real data |
| `pwid_test.go` | N37 — `spec.id` breaks on rename/move |
| `example_test.go` | Compiler-checked versions of the API shown in this README |
| `visibility_test.go` | N39/N40 — partial visibility is safe but useless |
| `concurrent.go` + `concurrent_test.go` | N45/N46 — mandatory race test, staleness behaviour |
| `merge_split.go` + test | N43/N44 — merge, split, and a real undo |
| `bench_test.go` + `scale_sweep.go` | N47/N48 — latency budget and candidate-filter scaling |
| `age_sweep.go` | N45 — how candidate staleness moves the decision |
| `store_helper.go` | Shared corpus builder for the sweeps and benchmarks |
| `hotpath_test.go` | Share of events the exact-match path absorbs |
| `retries_test.go` | N41 — status mapping across both levels, on a real retry run |
| `regression_test.go` | Demonstrates the timeout-losing bug the fix removed |
| `pgparity_test.go` | N50 — where the prototype and Postgres compute different things |
| `similarity_test.go` | N49 — trigram generation pinned to `generate_trgm()` semantics |
