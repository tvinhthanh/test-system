// Generates the outward-facing .docx from Markdown.
//
// SPEC 4.7 rule 2: one source, many formats. Hand-copying Markdown into Word is
// forbidden — that is what produced the literal ** characters in the v3.0 deck
// (finding F16). This script is the pipeline that rule requires.
//
// Usage: npm install && npm run build

import { readFileSync, writeFileSync } from 'node:fs';
import { marked } from 'marked';
import HTMLtoDOCX from 'html-to-docx';

const SRC = 'Automation-Testing-Platform-Summary.md';
const OUT = 'Automation-Testing-Platform-Summary.docx';

const md = readFileSync(SRC, 'utf8');

// Strip the leading blockquote metadata block; it is repo bookkeeping, not
// something a design partner should read.
const body = md.replace(/^# .*\n\n(> .*\n)+/, (m) => m.split('\n')[0] + '\n\n');

const html = `<!DOCTYPE html><html><head><meta charset="utf-8"><style>
  body { font-family: Calibri, sans-serif; font-size: 11pt; line-height: 1.45; }
  h1 { font-size: 20pt; } h2 { font-size: 15pt; margin-top: 18pt; }
  h3 { font-size: 12pt; }
  table { border-collapse: collapse; width: 100%; }
  th, td { border: 1px solid #bbb; padding: 5pt 7pt; text-align: left; vertical-align: top; }
  th { background: #f2f2f2; }
  code { font-family: Consolas, monospace; background: #f5f5f5; }
  blockquote { border-left: 3px solid #ccc; margin-left: 0; padding-left: 12pt; color: #444; }
</style></head><body>${marked.parse(body)}</body></html>`;

const buf = await HTMLtoDOCX(html, null, {
  orientation: 'portrait',
  margins: { top: 1134, right: 1134, bottom: 1134, left: 1134 },
  title: 'Automation Testing Platform',
});
writeFileSync(OUT, buf);
console.log(`wrote ${OUT} (${buf.length} bytes)`);
