# @atp/playwright-reporter

Playwright reporter đẩy run, kết quả và artifact lên platform theo SPEC §7. **Bất biến §7.5: reporter không bao giờ làm fail test run của người dùng** — mất mạng, server sập, cap artifact… tất cả thành một dòng cảnh báo và (khi không gửi được) một file spool.

## Cài vào project Playwright

```ts
// playwright.config.ts
export default defineConfig({
  reporter: [
    ["list"],
    ["@atp/playwright-reporter", { environment: "staging" }],   // url/token đọc từ env
  ],
});
```

```bash
ATP_URL=https://api.example.com ATP_TOKEN=atp_ing_… npx playwright test
```

| Tùy chọn | Env | Mặc định | |
|---|---|---|---|
| `url` | `ATP_URL` | — | thiếu → reporter tự tắt, in một cảnh báo, test vẫn chạy |
| `token` | `ATP_TOKEN` | — | ingest token `atp_ing_…` (`admin token issue`) |
| `environment` | `ATP_ENVIRONMENT` | `local` | `environmentKey` §7.3 |
| `repoRoot` | | `rootDir` của Playwright | bỏ prefix khỏi `filePath` (§9.2) để dev và CI ra cùng fingerprint |
| `spoolDir` | | `<tmp>/atp-reporter` | batch không gửi được ghi vào `atp-spool-<localRunId>.jsonl` |
| `uploadArtifacts` | | `true` | screenshot/video/trace/HAR/log |
| `stripHar` | | `true` | bỏ `Authorization`/`Cookie`/`Set-Cookie` khỏi HAR **trước khi upload** (§11.1) |
| `debug` | `ATP_DEBUG=1` | `false` | |

## Cái reporter làm (và không làm)

- `onBegin` → `POST /v1/runs` (localRunId UUID do reporter sinh; trigger tự phát hiện GitHub Actions / GitLab CI, **không fetch gì**).
- `onTestEnd` → một event `test.finished` **mỗi attempt** (`attempt = result.retry`), `status` từ `result.status`, `outcome` từ `test.outcome()` **mang nguyên** (N36: flaky là Playwright quyết, reporter không suy diễn lại). Batch mỗi **5 s hoặc 100 event**, `sequence` tăng dần, tối đa 500/batch.
- Attachment → `POST /v1/artifacts` (sha256 tính ở client, cap đọc từ `limits` server trả về — vượt cap thì bỏ qua và ghi warning, không declare) → PUT presigned hoặc multipart ≤ 4 part song song → `complete`.
- `onEnd` → flush, chờ upload, `POST …/finish`, in một dòng tổng kết.
- Retry: 5xx và timeout backoff 1/2/4/8 s tối đa 5 lần; 4xx không retry trừ 429 (theo `Retry-After`); timeout 30 s.
- Fingerprint §9.2 tính ở client: `test/fingerprint.test.ts` khóa 3 vector sinh từ Go (`backend/internal/identity/internal/match`) — separator `U+0000` tạo bằng `String.fromCharCode(0)` vì escape đã bị pipeline phá ba lần (N22).

## Dev

```bash
npm install
npm test          # tsc + node --test: 14 test (retry policy, batching, spool, HAR, fingerprint parity, end-to-end với server giả)
npm run example   # chạy example/ qua reporter thật — cần ATP_URL/ATP_TOKEN trỏ vào backend đang chạy
```

Đã chạy thật 16-09 với `example/` (3 test: pass, flaky có chủ đích, fail) vào `backend` local: 5 event, 6 artifact verified, worker resolve 3 identity, run `failed` với `flaky_count 1`, `final_status` = passed / flaky_pass / failed đúng bảng N41.
