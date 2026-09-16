# Gate kỹ thuật T0–T5 (SPEC §14) — bằng chứng, không phải ý định

Mỗi gate ở đây ghi **cái đã chạy thật**, ngày, lệnh, kết quả, và cách tái lập.
Chưa chạy = chưa đạt, dù code đã có. Thứ tự §14: Correctness → Reliability →
Parallelism → Observability → Security → Scale; T0, T1, T4 phải xong **trước
partner đầu tiên** (§14.4).

Nhắc lại QĐ-11 #2: những gì dưới đây **không được nhắc tới** tại Gate-0.

| Gate | Trạng thái | Ngày | Bằng chứng |
|---|---|---|---|
| **T0** core flow end-to-end | ✅ đạt | 16-09-2026 | §T0 dưới |
| **T1** không mất dữ liệu, idempotency, retry | ⏳ một phần — unit test có, chưa bắn vào process thật | — | §T1 |
| **T2** load 100 request đồng thời, p95 < 1 s | ✗ chưa chạy | — | — |
| **T3** failure recovery + observability | ✗ chưa chạy | — | metrics §6.4 đã có, chưa có kịch bản |
| **T4** security + tenant isolation | ✅ tenant isolation đạt (mọi route); phần security còn mục mở | 16-09-2026 | §T4 |
| **T5** soak 24–72 h | ✗ chưa chạy | — | — |

## T0 — reporter đẩy run thật, dashboard hiện đúng ✅ 16-09-2026

**Ba lớp bằng chứng, mỗi lớp thật hơn lớp trước:**

1. **`cmd/api/api_test.go` — `TestDashboardContractEndToEnd`.** Chạy *đúng handler
   binary phục vụ* (`buildHandler`) trên PostgreSQL 17.5 thật (`pgtest`, schema
   riêng mỗi test). Reporter giả lập đẩy 2 run qua `/v1/runs`, `/events`,
   `/finish`; worker in-process drain tier-2 + flaky; rồi browser giả lập đi hết
   hợp đồng `frontend/API_CONTRACT.md`: bootstrap → login → projects → runs
   (cursor, `q`, `status`, `environment`, `INVALID_SORT`, `INVALID_CURSOR`) →
   run detail → results (fail trước) → result detail (attempts + events) →
   triage-events (201, 200 dedupe, 400 type lạ) → identities / `view=flaky` /
   identity runs → logout.
2. **Reporter thật.** `reporter/playwright/example/` chạy `npx playwright test`
   qua reporter npm thật → backend thật trên `:8080`: 5 event, 6 artifact
   (presigned PUT vào `memory` store), worker resolve 3 identity, run id
   `2ff02be9-…` (14-09 → 16-09, DB `atp_test`).
3. **Dashboard thật, không mock.** Vue app `frontend/` chạy Vite dev
   (`/v1` proxy → Go), Chrome headless (Playwright, `chrome` channel), script
   `live-check.mjs` (scratchpad phiên 16-09): sign-in sai → lỗi chung; sign-in
   đúng; projects hiện `checkout` / `Payments`; runs hiện run `staging`; mở
   run → results; mở result → detail có attempts; bấm nút triage từ UI →
   `POST …/triage-events` 201; `/tests` hiện `declines expired card`,
   `accepts valid card`; `/flaky` render. **18 call `/v1`, tất cả 200/201**,
   401 duy nhất là mật khẩu sai cố ý. Không lỗi console.

**Tái lập (Windows, PostgreSQL 17 đã cài, không cần mật khẩu):**

```bash
# 1. Postgres rác, locale UTF-8 (pg_trgm §9 cần — README "Postgres cho identity")
PG="/c/Program Files/PostgreSQL/17/bin"
"$PG/initdb.exe" -D /tmp/atp-pgdata -U postgres --auth=trust -E UTF8 --locale=en-US
"$PG/pg_ctl.exe" -D /tmp/atp-pgdata -o "-p 5499 -c listen_addresses=127.0.0.1" -l /tmp/atp-pg.log start
"$PG/psql.exe" -h 127.0.0.1 -p 5499 -U postgres -c "CREATE DATABASE atp"
export DATABASE_URL="postgres://postgres@127.0.0.1:5499/atp?sslmode=disable"

# 2. Lớp 1: integration test
cd backend
TEST_DATABASE_URL="postgres://postgres@127.0.0.1:5499/atp?sslmode=disable" GOTOOLCHAIN=local go test -count=1 -run 'TestDashboardContractEndToEnd|TestEveryRouteRefusesCrossTenant' -v ./cmd/api/

# 3. Lớp 2 + 3: process thật
go run ./cmd/migrate
go run ./cmd/admin org create -name Acme -slug acme            # → org id
go run ./cmd/admin ws create -org <org> -name Payments          # → ws id
go run ./cmd/admin project create -ws <ws> -name checkout       # → project id
go run ./cmd/admin user create -email alice@example.com -password 'correct horse battery staple' -verified
go run ./cmd/admin role grant -user <user> -level project -scope <project> -role member
go run ./cmd/admin token issue -project <project> -name local   # → atp_ing_…  (in một lần)
COOKIE_SECURE=false go run ./cmd/api &                          # :8080
METRICS_ADDR=127.0.0.1:9091 go run ./cmd/worker &               # bắt buộc — tier-2 resolve chạy ở đây
cd ../reporter/playwright && npm ci && npm run build
ATP_URL=http://127.0.0.1:8080 ATP_TOKEN=atp_ing_… npm run example
cd ../../frontend && npm ci && npm run dev                      # http://localhost:5173, đăng nhập alice
```

Điểm chốt để không lặp lỗi: worker **phải** chạy — không có worker thì run
`finished` nhưng identity/flaky trống, dashboard "đúng" nhưng thiếu. `memory`
store mất artifact khi tắt api — chỉ để dev.

## T1 — không mất dữ liệu ⏳

Có (unit/integration test trên Postgres thật):

| Kịch bản §14.3 | Test | Ghi chú |
|---|---|---|
| Duplicate event / retry cùng `(localRunId, sequence)` | `internal/ingest` `TestIngestEndToEnd` (batch gửi lại → `duplicates: 3`, finish hai lần) | §7.1, không nhân đôi |
| Hai run song song cùng fingerprint lạ | `internal/identity` `TestRaceOnUnknownFingerprint` | một `TestIdentity` |
| Reporter mất mạng → spool, run khách vẫn xanh | `reporter/playwright/test/client.test.ts`, `reporter.test.ts` | spool `.jsonl`, không bao giờ fail run |
| S3 timeout khi upload → artifact retry được, run hợp lệ | `internal/artifact` với `memory.Fail` injection | §10.4 |
| Worker crash giữa job → lease hết hạn, job được nhận lại | `internal/platform/queue` `TestLeaseExpiryRecoversCrashedWorker`, `TestBackoff` | `FOR UPDATE SKIP LOCKED` |

**Chưa có — và đây là phần T1 thật sự:** kill `cmd/worker` / `cmd/api` *process
thật* giữa chừng, restart Postgres, queue mất 30 s — rồi kiểm invariant bằng
truy vấn DB, không phải bằng mock. Cần một harness `test/failure/` chạy các
binary thật. Chưa viết.

## T4 — tenant isolation ✅ 16-09-2026 · security còn mục mở

**Tenant isolation, mọi endpoint:** `cmd/api/tenant_walk_test.go` —
`TestEveryRouteRefusesCrossTenant`. Router của binary (`httpx.Router`) ghi lại
**mọi** pattern đã mount; test đi qua từng pattern, **không có danh sách tay**:
route mới không được phân loại → test fail. Với mỗi route có resource
(13/20 pattern; 7 pattern còn lại là session/healthz/list/create và được khẳng
định riêng), ba principal lạ:

1. member của org B trên dashboard, id toàn của org A → 404;
2. member của org B, **đúng project của mình** nhưng id run/result/identity của
   org A (IDOR kinh điển) → 404;
3. ingest token của org B với `localRunId` / `artifactId` của org A → 404;

cộng nhầm loại principal: cookie session lên route reporter → 401, bearer token
lên route dashboard → 401. Body gửi kèm luôn hợp lệ để 400 không thể "đỡ" cho
tenant check. Kết quả 16-09: **100% 403/404**, sau khi test bắt được một lỗi
thật — `GET …/runs/{runId}/results` với run của org khác trả `200 {items: []}`
(không lộ dữ liệu, nhưng §11.3.3 đòi 404) → đã sửa trong `internal/query`.

Ngoài ra `(project, localRunId)` là duy nhất *theo project*: org B dùng lại
`localRunId` của org A tạo run riêng, org A vẫn thấy đúng 1 run.

**Security còn mở (trước partner đầu tiên):** TLS/`COOKIE_SECURE=true` khi
deploy thật; `TOKEN_PEPPER` riêng cho staging/prod (config đã từ chối giá trị
dev); chưa có audit log truy cập; chưa có rate limit cho dashboard GET (chỉ có
cho login và ingest); chưa có review dependency (`govulncheck`, `npm audit`).
