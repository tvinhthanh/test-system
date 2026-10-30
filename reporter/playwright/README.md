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
| `flushTimeoutMs` | | `30000` | sau test cuối, chờ platform tối đa chừng này. Upload có phần ngân sách này trừ một khoản để dành cho finish (5 s, hoặc 1/3 nếu ngân sách ngắn hơn 15 s): upload chưa xong lúc đó bị bỏ, `finish` vẫn gửi, nên run đóng đúng trạng thái. Hết cả ngân sách → phần chưa gửi vào spool, bỏ upload/finish, run kết thúc (kết quả test không đổi) |
| `uploadArtifacts` | | `true` | screenshot/video/trace/HAR/log |
| `stripHar` | | `true` | bỏ `Authorization`/`Cookie`/`Set-Cookie` khỏi HAR **trước khi upload** (§11.1) |
| `debug` | `ATP_DEBUG=1` | `false` | |

## Cái reporter làm (và không làm)

- `onBegin` → `POST /v1/runs` (localRunId UUID do reporter sinh; trigger tự phát hiện GitHub Actions / GitLab CI, **không fetch gì**).
- `onTestEnd` → một event `test.finished` **mỗi attempt** (`attempt = result.retry`), `status` từ `result.status`, `outcome` từ `test.outcome()` **mang nguyên** (N36: flaky là Playwright quyết, reporter không suy diễn lại). Batch mỗi **5 s hoặc 100 event**, `sequence` tăng dần, tối đa 500/batch.
- Attachment → `POST /v1/artifacts` (sha256 tính ở client, cap đọc từ `limits` server trả về — vượt cap thì bỏ qua và ghi warning, không declare) → PUT presigned hoặc multipart ≤ 4 part song song → `complete`.
- `onEnd` → flush, chờ upload tới hạn của chúng, `POST …/finish`, in một dòng tổng kết (`artifacts X ok / Y over cap / Z failed`, kèm `/ N not uploaded in time` khi có). Object store hỏng hay treo không giữ run mở (§10.4: artifact không bao giờ làm fail run). Đây là thay đổi của 0.1.1; trước đó (0.1.0) run phải chờ server tự đóng sau 6 giờ với trạng thái `error`.
- Retry: 5xx và timeout backoff 1/2/4/8 s tối đa 5 lần; 4xx không retry trừ 429 (theo `Retry-After`); timeout 30 s. URL upload không parse được (object store cấu hình sai) thì fail ngay, không retry, và không in query (grant upload). `ATP_URL` không phải http(s) thì reporter tự tắt với một warning.
- **Platform sập giữa run:** request đầu tiên dùng hết lượt retry vì timeout/đứt kết nối/5xx thì coi platform down cho cả run — các request sau fail ngay, batch vào thẳng spool (không mỗi batch tự chờ 5 × 30 s). Không tạo được run → batch vào spool, không gọi mạng. PUT presigned lên object store không tính. Thời gian thêm vào CI của khách bị chặn bởi `flushTimeoutMs` sau test cuối — `test/outage.test.ts` (`ATP_OUTAGE_TEST=1`) chạy `playwright test` thật với cổng đóng và server treo để đo.
- Fingerprint §9.2 tính ở client: `test/fingerprint.test.ts` khóa 3 vector sinh từ Go (`backend/internal/identity/internal/match`) — separator `U+0000` tạo bằng `String.fromCharCode(0)` vì escape đã bị pipeline phá ba lần (N22).

## Dev

```bash
npm install
npm test          # tsc + node --test: 24 test (retry policy, batching, spool, HAR, fingerprint parity, end-to-end với server giả, upload hỏng/treo vẫn finish); test outage cần ATP_OUTAGE_TEST=1
npm run example   # chạy example/ qua reporter thật — cần ATP_URL/ATP_TOKEN trỏ vào backend đang chạy, và `npx playwright install chromium` (một test dùng browser thật)
```

Đã chạy thật 16-09 với `example/` (3 test: pass, flaky có chủ đích, fail) vào `backend` local: 5 event, 6 artifact verified, worker resolve 3 identity, run `failed` với `flaky_count 1`, `final_status` = passed / flaky_pass / failed đúng bảng N41.
Từ 18-09 `example/` có thêm một test Chromium thật cố ý fail (`receipt.spec.ts`)
với `trace/video/screenshot: retain-on-failure` — một run demo mang đủ mọi
kind artifact mà dashboard xử lý phía client (video, trace, screenshot): 7 event,
17 artifact. Text lỗi được bỏ mã màu ANSI trước khi gửi (`src/text.ts`) —
Playwright tô màu message khi nghĩ mình đang in ra terminal.
