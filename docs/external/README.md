# Outward-facing documents

`Automation-Testing-Platform-Summary.md` is the only source. The `.docx` is
generated from it — never edited by hand.

```bash
npm install
npm run build     # regenerates the .docx
./verify.sh       # enforces the SPEC 4.7 checklist; exit code is the gate
```

## Why the build script exists

SPEC 4.7 rule 2 forbids copying Markdown into Word by hand. That is not
pedantry: it is exactly what produced literal `**` characters in the v3.0 deck,
which then sat unnoticed in the document meant for customers (finding F16).

`verify.sh` re-checks every item on the 4.7 pre-send list against the generated
file, including the ones that are about content rather than formatting — MVP
item 1 must be the Reporter SDK, the roadmap must show P0 through P6, and
Production Safety Guard must be absent (it moved to P3 under QĐ-8).

Run `verify.sh` before sending anything to a design partner. It exits non-zero
if the document is not fit to send.

## Not generated

`.pptx` has no source yet. A slide deck is a different document with a different
argument, not a reflow of this one — write it when Phase 0 interviews show what
partners actually ask about, and generate it from its own Markdown source under
the same rule.
