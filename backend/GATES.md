# Gate kỹ thuật T0–T5 (SPEC §14) — bằng chứng, không phải ý định

Mỗi gate ở đây ghi **cái đã chạy thật**, ngày, lệnh, kết quả, và cách tái lập.
Chưa chạy = chưa đạt, dù code đã có. Thứ tự §14: Correctness → Reliability →
Parallelism → Observability → Security → Scale; T0, T1, T4 phải xong **trước
partner đầu tiên** (§14.4).

Nhắc lại QĐ-11 #2: những gì dưới đây **không được nhắc tới** tại Gate-0.

| Gate | Trạng thái | Ngày | Bằng chứng |
|---|---|---|---|
| **T0** core flow end-to-end | ✅ đạt | 16-09-2026 | §T0 dưới |
| **T1** không mất dữ liệu, idempotency, retry | ✅ đạt — 3 kịch bản kill/drop trên binary thật + unit cho phần còn lại; tìm ra và sửa 1 deadlock | 16-09-2026 | §T1 |
| **T2** load §14.2 | ⏳ dashboard 100k–1M ✅, resolver p99 ✅, ingest 100 đồng thời ✗ trên laptop (p95 ≈ 2 s; từ 8,4 s) — cần đo trên server | 17-09-2026 | §T2 |
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

## T1 — không mất dữ liệu ✅ 16-09-2026

**Harness `test/failure/`** — build `cmd/api` và `cmd/worker` từ cây nguồn, chạy
thật trên Postgres thật (schema riêng, `DATABASE_URL=…&search_path=t_xxxx`),
"crash" = `os.Process.Kill` (TerminateProcess, không có shutdown hook), invariant
kiểm bằng SQL. Chạy: `TEST_DATABASE_URL=… go test -count=1 -v ./test/failure/`
(≈ 9 s). Đã chạy 16-09, 4 lần liên tiếp đều pass.

| Kịch bản §14.3 | Test | Điều đã thấy |
|---|---|---|
| Kill API giữa batch ingest → retry cùng `(localRunId, sequence)` không nhân đôi | `TestKillAPIMidBatchThenReplay` — 4 reporter goroutine stream 40 batch × 25 event, giết api sau 120 ms, bật lại, replay **toàn bộ** 40 batch | kill rơi giữa dòng (2/40 batch ack, 50 event commit, "connection forcibly closed"); replay: `accepted + đã commit = 1000`, `duplicates = đã commit`; 1000 `run_events`, 1000 result, `attempt_count` = 1 tất cả; finish 200 |
| Kill worker giữa job → job được recover, không mất kết quả | `TestKillWorkerMidJobThenRecover` — 36 run × 12 test **cùng fingerprint**, giết worker khi đang drain (thấy `running: 4`), lease 2 phút được "tua" trong DB, bật worker mới | 36/36 job done, 0 dead; mọi result có `stable_test_id`; **đúng 12 identity** dù 4 worker song song + job chạy lại; re-run = đúng số job đang lease lúc chết |
| Queue không khả dụng → ingest vẫn nhận, xử lý trễ, không 5xx | nửa đầu test trên: 36 run ingest + finish khi **không có worker** | 108 request đều 201/200, 36 job pending, 0 result resolve sớm; worker lên là xử lý hết |
| Restart kết nối DB → không run nào kẹt `running` | `TestDBConnectionsDroppedUnderTraffic` — `pg_terminate_backend` mọi backend của api + worker, 4 lần, giữa lúc có traffic | request đầu sau khi đứt: 503 `AUTH_UNAVAILABLE` (đúng — 5xx để reporter retry, **không** 4xx đổ lỗi client), retry lần 2 là qua; worker sống, drain tiếp; 0 run `running`, 0 job dead; cả hai process còn sống |

**Lỗi thật harness bắt được (không có ở unit test):** 4 worker resolve song song
các run **cùng bộ test** — chính là ca thường gặp nhất (CI của một project chạy
suite giống nhau) — **deadlock Postgres 40P01** vì `ResolveRun` duyệt ref theo
thứ tự map ngẫu nhiên nên hai transaction insert cùng bộ unique key theo thứ tự
khác nhau. Queue retry che mất lỗi (job vẫn xong) nhưng mỗi lần là backoff 5 s —
vượt target resolver p99 < 5 s (§14.2) ngay ở tải nhỏ. Sửa: resolve theo thứ tự
fingerprint (`identity.ResolveDeferred`) và recompute flaky theo thứ tự identity
id (`flaky.RecomputeRun`) — cùng thứ tự thì transaction sau chỉ xếp hàng chờ rồi
đi đường `ON CONFLICT`. Harness giờ khẳng định `last_error IS NULL` cho mọi job:
re-run chỉ được đến từ lease hết hạn. Thời gian kịch bản 14–20 s → 4 s.

Unit/integration test cho phần còn lại (Postgres thật):

| Kịch bản §14.3 | Test | Ghi chú |
|---|---|---|
| Duplicate event / retry cùng `(localRunId, sequence)` | `internal/ingest` `TestIngestEndToEnd` (batch gửi lại → `duplicates: 3`, finish hai lần) | §7.1, không nhân đôi |
| Hai run song song cùng fingerprint lạ | `internal/identity` `TestRaceOnUnknownFingerprint` | một `TestIdentity` |
| Reporter mất mạng → spool, run khách vẫn xanh | `reporter/playwright/test/client.test.ts`, `reporter.test.ts` | spool `.jsonl`, không bao giờ fail run |
| S3 timeout khi upload → artifact retry được, run hợp lệ | `internal/artifact` với `memory.Fail` injection | §10.4 |
| Worker crash giữa job → lease hết hạn, job được nhận lại | `internal/platform/queue` `TestLeaseExpiryRecoversCrashedWorker`, `TestBackoff` | `FOR UPDATE SKIP LOCKED` |

**Còn ngoài harness:** S3 timeout chỉ ở mức unit (`memory.Fail`; store thật cần
staging); reporter mất mạng chỉ ở test của reporter; restore backup — Gate-1
(§11.5). "Queue không khả dụng" đọc là *worker không khả dụng* vì queue của P1
là bảng Postgres — queue chết = DB chết = kịch bản DB.

## T2 — load §14.2 ⏳ 17-09-2026: dashboard ✅, resolver p99 ✅, ingest 100 đồng thời ✗ trên laptop

**Harness `test/load/`** (chung `test/harness/` với T1): binary thật, Postgres thật,
số đo ghi đầy đủ và target §14.2 được *assert* — tụt là fail, không có chuyện số
lặng lẽ đổi trong tài liệu. Chạy: `LOAD_TEST=1 TEST_DATABASE_URL=… go test -count=1
-v -timeout 20m ./test/load/`; `LOAD_SCALE=10` cho 1M result. Máy đo: laptop
Windows, 12 CPU logic, **Postgres + api + worker + máy phát tải cùng một máy** —
đây là số "chỉ dấu", không phải số của môi trường triển khai.

| Target §14.2 | Đo được 17-09 | Kết luận |
|---|---|---|
| Ingest: 500 event/request, 100 request đồng thời, error < 1 %, p95 < 1 s | `TestIngest100ConcurrentReporters`: 100 project/token, mỗi reporter 1 run × 500 event, bắn cùng lúc. **0 lỗi**; events p50 1,2–1,4 s, **p95 1,7–2,3 s** (dao động giữa các lần chạy); cô lập một reporter: 30–50 ms/batch | **✗ p95** trên laptop. Cùng bài này lúc bắt đầu ngày là **8,4 s** — xem "đã sửa" dưới. Phần còn lại là CPU của một máy: 100 batch × ~40 ms = 4 s CPU Postgres chia cho ~6 nhân vật lý, cộng api + worker + generator. Cần đo trên server + máy phát tải riêng trước khi gọi là đạt |
| Resolver `clientTestId → stableTestId` p50 < 1 s, p99 < 5 s | 50 000/50 000 result của vòng đo resolve **inline lúc ingest** (tier 1, suite đã biết) — tức là xong trước cả finish. Đo khắt khe hơn: độ trễ job `run.finished` từ lúc enqueue đến done: **p50 1,1–1,5 s, p99 1,6–2,2 s** | p99 ✅; p50 sát ngưỡng (queue poll 1 s + 100 job dồn cùng lúc). Trước khi sửa flaky: p50 5,4 s / p99 9,8 s |
| Dashboard: 100k–1M result trong project, query thường dùng p95 < 500 ms | `TestDashboardQueriesAt100kResults`, 16 route hợp đồng + 2 metrics, 30 lần mỗi route. **100k**: mọi route p95 ≤ 160 ms. **1M** (`LOAD_SCALE=10`, 1 000 run trải 6 tháng): runs list 65 ms, run results 3 ms, result detail 4 ms, identities 24 ms, identity runs 58 ms, `q=`/`status=` 52 ms, metrics/trend 433 ms, metrics/triage 439 ms | **✅** cả hai cỡ. Lưu ý: hai route metrics tính cửa sổ 30 ngày lúc request; nếu 1M result **dồn hết vào một tháng** thì ~1,1 s — đã có index `test_results_failure_observed` (`0010`) và query dạng join thay subselect; sau đó là rollup theo ngày, chưa cần ở P1 |
| Queue: không mất job, retry backoff, duplicate-safe, worker crash recover | T1 | ✅ |
| Artifact: upload fail < 2 %, checksum mismatch, không `unlinked` > 1 h | chưa đo dưới tải (store `memory`) | ✗ chưa chạy |

**Đã sửa nhờ harness (mỗi mục đều là lỗi kiến trúc thật, không phải tinh chỉnh):**

1. **Ingest ghi từng event** — `AppendAttempts` chạy 5 statement *mỗi event* (2 500
   round trip cho batch 500) → viết lại thành 5 statement *mỗi batch* (`unnest`
   arrays; `ON CONFLICT` cần gom trùng key trong Go trước vì một statement không
   được chạm một hàng hai lần). p95 8,4 s → ~3 s.
2. **Update ngay sau insert** — tier 1 resolve chạy *sau* khi ghi rồi `UPDATE` 500
   hàng vừa insert (non-HOT, chạm mọi index), và aggregate cũng `UPDATE` lại 500
   hàng ấy. Đảo thứ tự: resolve trước, `stable_test_id` và aggregate đi cùng
   INSERT; chỉ hàng đã tồn tại (retry, attempt sau) mới recompute. p95 → ~1,8 s,
   batch cô lập 250 ms → 30 ms.
3. **Flaky không incremental** — §8.6 ghi "incremental, không full scan" nhưng mỗi
   finish đọc lại cả cửa sổ 30 ngày của mọi identity trong run: O(run × test) —
   ở 1 000 run × 1 000 test là **1M dòng mỗi finish**, queue 1M result *không bao
   giờ drain* (10 phút vẫn 673 pending). Sửa (`0010`): fold state lưu trong
   `flaky_signals.last_outcome`, finish chỉ `Apply` kết quả của run đó; `Sweep`
   mỗi phút rebuild chính xác các dòng `rebuilt_at` > 24 h để kết quả rời cửa sổ
   rớt khỏi đếm. Công thức vẫn một chỗ (`Stats.Apply` + `Finalize`, `Compute` là
   fold). `TestIncrementalFoldMatchesExactRebuild` giữ hai đường bằng nhau và
   kiểm sweep rớt đúng số run. 1M result: drain xong trong < 1 phút.
4. **Metrics subselect tương quan** — 2 index probe cho *mỗi* failure trong cửa
   sổ → join gộp một lần; index partial cho `failure_observed_at`.

**Điều harness cho thấy nhưng không sửa được ở đây:** thời gian statement dưới
100 đồng thời gấp 5–7 lần lúc cô lập (`pg_stat_statements`: mean vs min) — tranh
chấp CPU của một máy chạy cả bốn thứ. `synchronous_commit=off` không đổi gì (không
phải fsync); bỏ index `test_results_client` không đổi gì (đã hoàn tác). Cách đo
đúng: server, Postgres riêng, máy phát tải riêng, lặp lại hai bài trên.

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
