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
| **T3** failure recovery + observability | ✅ đạt — alert rules §6.4 trên tên metric thật, gauge phía api, 4 kịch bản hỏng nhìn thấy được ở `/metrics`, log không lộ bí mật | 17-09-2026 | §T3 |
| **T4** security + tenant isolation | ✅ tenant isolation (mọi route) 16-09; security §11.6 có bằng chứng từng dòng 17-09 (govulncheck, npm audit, CORS/headers, rate limit dashboard, SSRF rule, route inventory); **restore drill ✅** (`test/restore/`: dump → DB mới → 24/24 bảng checksum bằng nhau, api trả đúng byte, worker tiếp job pending, RTO ≈ 2 s ở cỡ test); audit revoke ✅ (`authz_audit`); còn mở: pentest | 17-09-2026 | §T4 |
| **T5** soak 24–72 h | ⏳ harness `test/soak/` chạy **2 giờ** trên laptop 18-09: 5 400 run, 545k result, 87k request, **0 lỗi**, mọi trend phẳng (worker heap, goroutine, pool, backlog, `jobs`, latency), heap api chỉ lớn theo payload của store dev; tìm ra và sửa: `jobs` không bao giờ dọn, gauge backlog quét cả bảng, grant store dev không xóa; heap api đã profile: chỉ payload của store dev lớn lên (`io.ReadAll` ← `memory.Store.serve`), không có holder khác. **24–72 h cần host riêng + S3 staging** | 17-09-2026 | §T5 |

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
   **`frontend/scripts/live-check.mjs`** (lặp lại được; 16-09 chạy từ scratchpad,
   17-09 chạy lại bản đã commit: 10 bước, **30 call `/v1`, tất cả 2xx** trừ 401
   cố ý, console sạch — và bắt được 2 lỗi UI: sign-out remount trang bảo vệ
   → 2 GET 401, run không có branch thì link rỗng không bấm được; đã sửa):
   **Từ 17-09 job `e2e` trong CI dựng compose thật (Caddy TLS, bucket giả qua
   `/_objstore`), đẩy run bằng reporter npm rồi chạy đúng script này mỗi push.** sign-in sai → lỗi chung; sign-in
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
| Artifact: upload fail < 2 %, checksum mismatch, không `unlinked` > 1 h | `test/soak` 17-09 (store `memory`): 1 710 artifact upload đủ ba bước, **0 lỗi**; 90 artifact bị lật bit trên đường đi → 90/90 `422 CHECKSUM_MISMATCH`, run vẫn hợp lệ; `artifacts_unlinked_overdue` = 0 suốt bài | ✅ trên store dev; S3/R2 thật cần đo lại ở staging (latency PUT ngoài) |

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

## T3 — failure recovery + observability ✅ 17-09-2026

"Đủ để biết mình đang hỏng" được hiểu là: mỗi kịch bản hỏng của §14.3 phải
**hiện ra ở `/metrics`** dưới đúng tên mà alert rule đang nhìn, và log phải nói
*tại sao* — không bao giờ nói *bí mật*. Ba thứ đi cùng nhau:

1. **`deploy/prometheus/alerts.yml`** — mọi ngưỡng bắt buộc của §6.4 viết thành
   Prometheus rule trên **đúng tên metric code đang export** (ingest error rate
   > 1 %/15 phút theo project; auto-close > 2/ngày/project; resolver p99 > 5 s;
   artifact fail > 2 %; unlinked > 1 h; identity churn > 3× baseline; review
   queue > 50; API 5xx và p99), cộng những thứ §6.4 không gọi tên nhưng không có
   thì mù: worker chết (`up`), worker treo (heartbeat), backlog già, job dead,
   api không đọc được DB.
2. **Số đo mới** (harness T3 phát hiện thiếu):
   - `queue_job_latency_seconds{kind}` — độ trễ enqueue → done, chính là "độ trễ
     resolver" của §6.4; trước chỉ có thời gian handler.
   - `queue_worker_last_poll_timestamp_seconds{kind}` — nhịp tim worker; treo
     khác chết.
   - **Gauge phía api** đọc DB lúc scrape (`cmd/api/health_metrics.go`):
     `queue_backlog_jobs`, `queue_backlog_oldest_seconds`, `queue_running_jobs`,
     `queue_dead_jobs`, `runs_open_over_limit`, `review_queue_pending`. Lý do:
     worker chết thì `/metrics` của nó chết theo — backlog phải nhìn được từ
     process còn sống. Đọc lỗi → `-1`, để "DB không với tới" khác "queue rỗng".
   - Gauge/counter không nhãn hiện `0` ngay từ lúc đăng ký: "vắng" không bao
     giờ đồng nghĩa "bằng 0" (harness bắt được `artifacts_unlinked_overdue` vắng
     mặt cho tới lần sweep đầu).
3. **`test/observe/`** — 4 bài trên binary thật (≈ 9 s, 17-09 chạy 4 lần đều pass):

| Kịch bản | Điều operator phải thấy | Đã thấy |
|---|---|---|
| Worker bị giết, 5 run finish, worker bật lại | api: `queue_backlog_jobs` 0 → 5 → 0, `queue_backlog_oldest_seconds` ≥ 1; worker mới: `queue_jobs_total{done}` = 5, `queue_job_latency_seconds_count` = 5 với sum ≥ 1 (job đã đợi), heartbeat < 10 s | ✅ |
| Token sai (401), batch 501 event (413), rồi đứt mọi kết nối DB của api → 503 | `ingest_requests_total{endpoint="events",status="5xx"}` ≥ 1 và `http_requests_total{route,status="503"}` ≥ 1; 401 và 413 nằm ở `4xx`, **không** lọt vào tỉ lệ 5xx | ✅ — sau khi sửa lỗi dưới |
| Run mở 7 giờ không finish; artifact upload xong nhưng event không bao giờ tới, 2 giờ trước | trước sweep: api `runs_open_over_limit` = 1; sau khi worker boot (sweep ngay): `runs_autoclosed_total{project}` = 1, run thành `error/RUN_TIMED_OUT`, `artifacts_unlinked_overdue` = 1, `review_queue_pending` = 0 | ✅ |
| Traffic đủ loại (ingest tốt, token sai, login sai/đúng, 503 do đứt DB, job worker) rồi đọc **toàn bộ log** api + worker | không có token ingest, mật khẩu, `atp_sess_`, hay token bị từ chối trong log; mọi dòng ERROR/WARN mức request có `request_id` | ✅ |

**Lỗi thật harness bắt được:**

- **Ingest error rate mù với lỗi trước handler.** `ingest_requests_total` chỉ đếm
  khi handler chạy; 503 từ auth (DB mất) và 429 từ rate limit không được đếm →
  alert §6.4 "ingest > 1 %" sẽ **im lặng đúng lúc DB sập** — đúng cái §6.4 sợ
  ("không biết mình đang trượt"). Sửa: observation thành middleware bọc ngoài
  auth + rate limit; project = `-` khi chưa xác thực được.
- **5xx không có dòng log nào giải thích.** Access log ghi INFO mọi status; 503
  `AUTH_UNAVAILABLE` không log nguyên nhân (đúng §11.5 với client, nhưng
  operator cũng mù). Sửa: `httpx.Cause(ctx, err)` — middleware/handler ghi
  nguyên nhân vào request, access log in **một dòng ERROR** với `request_id`,
  route, status, `cause` cho mọi 5xx; client vẫn chỉ nhận thông điệp chung.
  Mọi chỗ trả 5xx (authz bearer/session/logout, ingest, artifact store, dashboard)
  đi qua đường này — hết log lẻ tẻ mỗi module một kiểu.

**Còn ngoài T3:** dashboard Grafana (chưa cần — alert rule là thứ đánh thức
người); runbook cho từng alert; log shipping. Chỉ số "Chi phí" và "Sản phẩm"
của §6.4 (storage/org, hypothesis LLM, `untriaged_rate`…) là Gate-1.5.

## T4 — tenant isolation ✅ 16-09-2026 · security còn mục mở

> 21-09: walk cross-tenant mở rộng theo P2 — 52 route (25 route Test Management mới, gồm `GET /v1/releases/{id}/gate` cho token CI: token org khác → 404, token chỉ `project:ingest` → 404, cookie → 401). Vẫn ba principal lạ mỗi route; route mới không phân loại = test fail.
> 22-09: 53 route — thêm `POST …/test-plans/{planId}/executions/{executionId}/evidence` (multipart). Tenant check chạy **trước** khi đọc body, nên walk gửi body JSON vẫn phải nhận 404 chứ không phải 400; `executionId` của org A là fixture mới. `api_test.go` kiểm thêm: viewer org khác → 404 cả upload lẫn `GET …/content`, HTML đổi tên `.png` → 400, plan closed → 409, header §11.5 trên byte trả về.
> 27-09: 56 route — thêm `GET|POST /v1/projects/{projectId}/members` và `DELETE …/members/{userId}`; fixture mới `userId` = user của org A. Thành viên org B → 404 trên project A; trên project của mình với user id của A → 403 (member không có `member.manage`); quản lý org B xóa user A khỏi project B → 404 vì không có hàng. `members_test.go`: member/maintainer 403, ws_admin liệt kê + phân trang (cursor gắn project), cấp/đổi/gỡ, role ngoài tầng project → 400, email lạ/chưa verify → 404 `USER_NOT_FOUND`, gỡ role kế thừa → 404, thiếu CSRF → 403, audit `role.grant`/`role.revoke` có actor.
> 27-09 (MFA): 61 route — 5 route `/v1/session/mfa…` vào danh sách miễn của walk vì chỉ tác động lên chính người đang đăng nhập (không mang id tenant); tính chất của chúng kiểm ở `cmd/api/mfa_test.go`: không phiên → 401, owner chưa bật → mọi route tenant 403 `MFA_ENROLLMENT_REQUIRED`, challenge dùng một lần, bước hai thiếu CSRF → 403.
> 27-09 (Team): 68 route, đi 55 route tài nguyên — `…/workspaces/{workspaceId}/members` và `…/organizations/{organizationId}/members` vào walk với fixture id của org A (thành viên org B → 404); `GET /v1/member-scopes` miễn (theo người dùng) và walk kiểm B không quản lý gì. `TestTeamMembers`: ws admin không chạm org hay workspace anh em (404), org admin → `403 OWNER_ONLY` khi cấp/đổi/gỡ owner, owner cuối → `409 LAST_OWNER` (cả qua `cmd/admin`), owner org khác → 404 cả đọc lẫn ghi.

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

**Security — checklist §11.6 với bằng chứng (17-09-2026):**

| # §11.6 | Bằng chứng phải có | Có gì |
|---|---|---|
| API1 BOLA | cross-tenant 100 % | `TestEveryRouteRefusesCrossTenant` — mọi route, 3 principal lạ ✅ |
| API2 AuthN | brute-force bị chặn | `internal/authz` login tests: argon2id, rate limit theo IP + account, session revoke ✅. MFA owner: chưa (P1-min không có UI quản trị) |
| API3 Property-level | gửi field không được phép → bị bỏ qua | `TestDashboardContractEndToEnd`: `triage-events` với `actorId`/`actorType`/`occurredAt`/`projectId` giả → actor là session, thời gian là server ✅. Không có endpoint sửa user/role trong P1-min (admin CLI) |
| API4 Resource | vượt trần → `413`/`429` | batch 501 event → 413, artifact quá cap → 413, ingest 60/phút/token → 429 (test cũ); **mới**: dashboard 600/phút/user (`DASHBOARD_RATE_PER_MINUTE`), `TestDashboardRateLimitPerUser` ✅ |
| API5 Function-level | viewer gọi endpoint maintainer → 403 | bob viewer → `triage-events` 403 ✅; member gọi merge/decide → 403 `FORBIDDEN` ✅ (18-09); **22-09 sáu role §11.3.1**: `TestMatrixMatchesSpec` chép sáu cột §11.3.2 kể cả hàng không bậc thang (`data.delete`: ws_admin ❌, maintainer ✅), `TestMembershipsAgainstPostgres` đi org_owner/org_admin/workspace_admin/maintainer + hợp membership; `api_test` kiểm `actions[]` của member/viewer/maintainer; migration 0015 chạy thật trên DB có hàng P1-min; **27-09** member/maintainer gọi `…/members` → 403 `FORBIDDEN`, ws_admin được (`members_test.go`) |
| API6 Business flow | đăng ký/mời/quên mật khẩu có rate limit | P1-min **không có** các flow này (user do admin CLI tạo) — N/A, ghi để không quên khi thêm |
| API7 SSRF | grep: không outbound fetch từ input | **mới**: `TestNoOutboundHTTPFromDomainModules` — `internal/` không dùng `http.Client`/`Get`/`NewRequest` ngoài `platform/objstore` ✅ |
| API8 Misconfig | checklist config | config từ chối `sslmode=disable`, pepper dev, `COOKIE_SECURE=false` ngoài dev ✅; container non-root/pin/healthcheck + TLS proxy + `.env.example` (`deploy/`, dựng thật 17-09) ✅; `httpx.BrowserPolicy` — `nosniff`, `no-store`, `Referrer-Policy`, `X-Frame-Options`, HSTS khi cookie Secure, CORS chỉ cho `DASHBOARD_ORIGINS` (không bao giờ `*`, preflight từ origin lạ → 403), `TestBrowserPolicyHeadersAndCORS` ✅ |
| API9 Inventory | chỉ `/v1/`, diff mỗi release, token `lastUsedAt` | **mới**: `deploy/routes.txt` = inventory, `TestRouteInventoryMatchesFile` fail nếu route mount khác file ✅; `api_tokens.last_used_at` cập nhật ≤ 5 phút/lần ✅. **`deploy/openapi.yaml`** (OpenAPI 3.1, 21 operation, cả hai nửa reporter + dashboard) là nguồn sự thật từ 17-09. Bản nháp đầu bị review (`docs/openapi-contract-reimplementation.md`) bắt 2 lỗi thật — cookie tên `atp_sess` (server đọc `atp_session`; `atp_sess_` chỉ là prefix của *giá trị*) và `sequence` khai `minimum: 1` trong khi validator nhận 0 — sửa 18-09: doc đúng, validator từ chối `sequence <= 0` (`TestEventSequenceBoundary`: thiếu/−1/0 → 400 `INVALID_PAYLOAD` `events[0].sequence`; 1 và 42 nhận; lặp 1 → duplicate, không lưu hai lần), test parse **cấu trúc** bằng `gopkg.in/yaml.v3` (không còn scanner theo indent): `TestOpenAPIMatchesMountedRoutes` hai chiều, `TestOpenAPIBindsSharedLiterals` (cookie = `authz.SessionCookie`, header = `authz.CSRFHeader`, `atp_csrf` được gọi tên, `Event.sequence` minimum 1 và required), `TestOpenAPIIsInternallyConsistent` (144 `$ref` resolve, 21 `operationId` duy nhất); mã lỗi trong doc đối chiếu từng handler (`INVALID_CREDENTIALS`, `AUTH_UNAVAILABLE`, `TOKEN_SCOPE`, `INVALID_QUERY`…); `redocly lint` xanh trong CI; `routes.txt` giữ lại làm inventory phẳng để diff nhanh |
| API10 Unsafe consumption | LLM trả rác → bị loại | P1.5 (chưa có LLM) — N/A |

**Dependency review 17-09:** `govulncheck ./...` — 1 lỗ hổng trong module
(`golang.org/x/text` 0.31 → đã lên 0.39) và **18 advisory stdlib** fix ở Go
1.25.6–1.25.13; laptop chạy 1.25.5 nên `go.mod` thêm `toolchain go1.25.13` —
CI/image (GOTOOLCHAIN=auto) build bằng bản đã vá, laptop pin `GOTOOLCHAIN=local`
không đổi. `npm audit` reporter + frontend (kể cả dev deps): 0. Lặp lại trước
mỗi release; kết quả ghi vào đây.

**Restore drill §11.5 / §14.3 hàng cuối ✅ 17-09-2026 — "backup chưa restore thử
= chưa có backup".** `test/restore/` (`RESTORE_TEST=1 TEST_DATABASE_URL=… go test
-v ./test/restore/`, ≈ 10 s, cần `pg_dump`/`pg_restore` — `PG_BIN` hoặc PATH):

1. Thế giới thật qua binary thật: 2 pipeline, 24 run × 100 test (10 % fail),
   4 artifact (2 verified, 1 rejected vì checksum, 1 pending), một member đăng
   nhập và triage một failure. **Worker bị giết trước 4 run cuối** → backup
   chứa 4 job `pending`.
2. Sự cố: giết api. Đọc **mọi bảng** của schema: số dòng + `md5(string_agg(row::text
   ORDER BY …))`.
3. `pg_dump --format=custom --no-owner --no-privileges` (đúng lệnh của
   `deploy/backup/backup.sh`) → `pg_restore --list` phải liệt kê được
   `TABLE DATA test_results`.
4. `CREATE DATABASE … TEMPLATE template0` **cùng encoding/collation** với nguồn
   (đọc từ `pg_database` — đổi collation là đổi thứ tự mọi index text, drill này
   tồn tại để bắt đúng loại khác biệt đó), `CREATE EXTENSION pg_trgm` (0004 cài
   vào `public`, dump theo schema không mang theo), `pg_restore --exit-on-error`
   (đúng `deploy/backup/restore.sh`).
5. Kiểm trên bản copy: **24/24 bảng bằng nhau** cả số dòng và checksum;
   `pg.Migrate` trên copy áp **0** migration (ledger `schema_migrations` có trong
   dump, không drift); api khởi động trên copy → **cookie cũ vẫn đăng nhập**, 9
   trang dashboard (projects, runs, run, results, result, identities, flaky,
   metrics/triage, metrics/trend) trả **đúng từng byte** như trước sự cố; worker
   khởi động trên copy → 4 job pending chạy xong, 0 result thiếu `stable_test_id`;
   ingest run mới qua token cũ → 201/200, xuất hiện đầu danh sách.
6. **RTO đo được trên laptop: dump 0,3 s (315 KB) + tạo DB 0,9 s + restore 0,4 s
   + boot api 0,1 s ≈ 2 s** (ngân sách §11.5: 4 h). Ở kích thước thật RTO là
   thời gian `pg_restore` — đo lại khi có dump production đầu tiên, ghi vào đây.

`deploy/backup/backup.sh`: dump hằng ngày, tự `--list` để không giữ file không
restore được, từ chối dump thiếu `schema_migrations`, giữ `BACKUP_KEEP_DAYS`
(14). `restore.sh`: **luôn vào DB mới**, không bao giờ ghi lên DB đang sống —
quyết định bỏ dòng mới hơn backup là của người, sau khi nhìn cả hai. Việc còn
lại là của vận hành, không phải của code: cron + copy dump ra khỏi host (object
storage có versioning) — backup nằm cùng đĩa với DB không phải backup. Artifact
bytes nằm ở object store (R2/S3 có durability riêng); dump chỉ chịu trách nhiệm
hàng `artifacts`.

**Audit hành động phá hủy §11.7 ✅ 17-09-2026:** `0012_authz_audit` — `token.revoke`
và `role.revoke` ghi cùng transaction với thay đổi (không có revoke không có
dòng, không có dòng của revoke đã rollback), có `actor` (bắt buộc — thiếu → 400,
không đổi gì), scope để lọc theo tenant, `details` (tên token / role bị bỏ).
Revoke lần hai không thêm dòng (không có gì đổi). `admin token revoke -actor`,
`admin role revoke`, `admin audit list [-scope]`. Test: `internal/authz`
(`TestTokensAgainstPostgres`, `TestMembershipsAgainstPostgres`). `identity_audit` (0004) vẫn là nơi của merge/undo/split.

**Hình dạng triển khai §11.5 ✅ 17-09-2026 — `deploy/`:** một image distroless
`nonroot` (19,8 MB, mọi binary, `HEALTHCHECK` bằng `cmd/healthcheck` vì không có
curl), `compose.yml` staging: Postgres 17.5 locale UTF-8, `migrate` một lần rồi
`api`/`worker` mới lên, Caddy là listener TLS duy nhất (cert tự động, SPA +
proxy `/v1`, API không bao giờ nhận HTML fallback), Prometheus nạp `alerts.yml`,
container `backup` chạy `backup.sh` mỗi ngày; `read_only`, `cap_drop ALL`,
`no-new-privileges`, mọi image pin tag, `.env.example` đủ biến, `deploy/.env`
bị ignore. **Đã dựng thật trên Docker Desktop**: healthy, ingest qua
`https://localhost` → worker resolve, header HSTS/nosniff/`Secure` qua proxy,
3 target Prometheus `up`, dump đầu tiên 12 KB có `schema_migrations` — chi tiết
`deploy/README.md`. CI `.github/workflows/ci.yml`: gofmt, vet, `go test ./...`
trên Postgres 17 thật (T1/T3/T4 + restore drill với pg_dump 17), govulncheck,
`docker build`, `compose config`, reporter/frontend test + `npm audit` — §11.6
"chạy trước mọi release" thành máy chạy.

**Vòng đời token §11.4 ✅ 17-09-2026:** prefix 8 ký tự + HMAC-SHA-256(pepper) để
verify, argon2id chỉ cho password, `lastUsedAt` throttle 5 phút, revoke tức thời
(có audit), expiry mặc định 1 năm — đã có từ trước. **Mới**: "nhắc trước 30 ngày"
thành gauge `api_tokens_expiring_30d` (api đọc DB lúc scrape) + alert
`IngestTokensExpiringSoon`; "alert khi volume một token tăng > 10× baseline tuần"
thành counter `ingest_events_by_token_total{project,token}` + rule
`IngestVolumeSpike` (so ngày hôm nay với trung bình 7 ngày trước, sàn 10k
event/ngày). `promtool check config` xanh (22 rule), chạy trong CI. Còn ngoài code:
đăng ký prefix `atp_ing_` với GitHub Secret Scanning khi có user ngoài.

**Còn mở (§11.5, §11.7):** prod cần Postgres có TLS (`APP_ENV=prod` từ chối
`sslmode=disable`; compose là staging); copy volume `backups` ra khỏi host;
pentest thuê ngoài trước khách trả phí đầu tiên.

## T5 — soak ⏳ 17-09-2026: harness chạy sạch 20 phút trên laptop; 24–72 h cần host riêng

**Harness `test/soak/`** (chung `test/harness/`): binary thật, Postgres thật, traffic
**đều** — không phải burst như T2 — lấy mẫu mỗi vài chục giây, cuối bài **assert xu
hướng**: tăng là fail, không phải "ghi nhận". Chạy:

```bash
SOAK_TEST=1 SOAK_DURATION=24h SOAK_SAMPLE=1m SOAK_JOB_RETENTION=1h SOAK_OUT=soak.csv \
TEST_DATABASE_URL=… go test -count=1 -v -timeout 80h ./test/soak/
```

Traffic mỗi run (6 pipeline trên 3 project, một run/8 s/pipeline — dưới trần
60 req/phút/token của §7.4): create → 2 batch × 100 event (1/25 fail, 1/40 flaky:
fail rồi pass ở attempt 1) → 2 artifact qua presigned PUT thật, **1/10 bị lật một
bit trên đường đi** (server phải hash lại và trả 422 `CHECKSUM_MISMATCH`, run vẫn
hợp lệ) → run thứ 7 gửi lại batch đầu (phải dedupe đủ) → finish. Một browser
đăng nhập đi 9 route dashboard mỗi 2 s, triage một failure mỗi 30 s. Mỗi mẫu:
RSS/heap/goroutine/pool của api và worker (`metric.Process`, `db.Metrics` — mới),
số backend Postgres, backlog/oldest/dead/done của queue, số dòng và kích thước
bảng `jobs`, run kẹt, result thiếu `stable_test_id`, artifact unlinked, heartbeat
worker, p50/p95 của `/events` trong cửa sổ, tổng request/lỗi. CSV đầy đủ ở `SOAK_OUT`.

**Kết quả 2 giờ, 18-09 10:05–12:05 (laptop, cùng cấu hình, `SOAK_JOB_RETENTION=5m`, 240 mẫu / 30 s) — PASS mọi kiểm tra:**

| Điều đo | Thấy gì |
|---|---|
| Traffic | **5 400 run, 545 400 result, 87 365 request, 0 lỗi, 0 5xx**; 540 mismatch cố ý → 540/540 `422`; 774 replay dedupe đúng; 238 triage |
| Sau khi dừng | backlog 0; 0 dead / 0 retry; 0 run `running`; 0 result thiếu id; 0 unlinked quá hạn; heartbeat < 10 s |
| Worker | heap sống 1,6 → 1,4 MB, RSS 20 MB, 14 goroutine — **phẳng** suốt 2 giờ |
| api | goroutine 22 → 22; pool acquired 0; backend Postgres 12 → 11 (một idle đóng); heap sống **trừ payload store 8,0 → 15,9 MB** (dưới ngưỡng ×1,3 + 8; xem giải thích dưới); RSS 92 MB vs live 51 MB — trong pacing (≤ 3× + 48) |
| Queue | `jobs` phẳng **246 dòng, 0,29 MB** qua 5 400 run; backlog median 0, max 0 |
| Latency | `/events` p50 median 22 ms, p95 median 62 ms (⅓ đầu 58 → ⅓ cuối 62), p95 max một cửa sổ 605 ms |
| Bảng | `test_results` 290 MB cho 545k result (≈ 560 B/result kể cả index) |

Heap sống của api trừ `sum(actual_bytes)` vẫn lên 14 MB trong 2 giờ, **đúng 0,36×
lượng payload store thêm vào** (39 MB) ở mọi điểm lấy mẫu (300 s: 3,6/1,7; 2 400 s:
7,5/13,6; 4 800 s: 12,3/27,3; 7 200 s: 17,7/40,9). Đó là cách store dev giữ byte:
`io.ReadAll` trả slice có `cap` lớn hơn `len`, Go xếp 4,2 KB vào size class 4 864–5 376 B,
cộng entry map + etag mỗi object — hệ số ~1,3–1,36 so với `actual_bytes`. Worker
(không có store) phẳng tuyệt đối, nên phần dư này là của store, không phải của api.
Với `OBJSTORE=s3` đường này bằng 0 — lý do run 24 h phải chạy với bucket thật.

**Kết quả 20 phút, 17-09 (laptop, Postgres + api + worker + generator cùng máy):**

| Điều đo | Thấy gì |
|---|---|
| Traffic | **900 run, 90 900 result, 14 562 request, 0 lỗi, 0 5xx**; 90 mismatch cố ý → 90/90 `422 CHECKSUM_MISMATCH`; 132 replay → dedupe đúng số event; 40 triage |
| Sau khi dừng | backlog về 0 trong < 1 s; 0 job dead, 0 job phải retry (`last_error`), 0 run `running`, 0 result thiếu id, 0 artifact unlinked quá hạn, heartbeat worker < 10 s |
| Worker | RSS 19,6 → 19,9 MB, heap 4,3 → 4,5 MB, 14 goroutine — **phẳng** |
| api | 22 goroutine phẳng, pool acquired 0, 12 backend Postgres phẳng; RSS 31 → 41 MB (median ⅓ đầu → ⅓ cuối), heap 11,6 → 19,0 MB — xem ghi chú dưới |
| Queue | `jobs` dao động **90–120 dòng, 0,17 MB** suốt 900 run (`JOB_RETENTION=2m`): prune chạy; backlog median 0 |
| Latency | `/events` (100 event) p50 ≈ 32 ms, p95 median 65 ms, p95 ⅓ đầu 79 ms → ⅓ cuối 59 ms — **không trôi** |
| `test_results` | 47 MB cho 90 900 result (≈ 520 B/result kể cả index) |

**Lần chạy dài hơn, 17-09 tối (laptop, 2 h dự kiến):** máy ngủ sau **51 phút**
(102 mẫu / 30 s) và `go test` bị giết vì quá `-timeout` theo giờ tường — CSV cuối
không được ghi (đã sửa: giờ flush từng mẫu), số lấy từ log: **2 298 run,
232 098 result, 37 169 request, 0 lỗi**; goroutine api 22 → 22, worker 14; backend
Postgres 12 → 12; worker RSS 21 → 20 MB; `jobs` phẳng 228 dòng (`JOB_RETENTION=5m`);
backlog 0 suốt; `/events` p95 median 108 → 53 ms. api RSS 34 → 60 MB (trừ payload
store: 31 → 45) — chính điều khiến harness đổi thước đo sang heap sống sau GC
(xem ghi chú heap dưới); mẫu đầu sau khi máy thức: gauge DB trả −1 rồi hồi — đúng
như thiết kế ("không với tới DB" khác "rỗng"). Sau khi bị giết, api/worker con
thành orphan giữ 5 connection — dọn tay; ghi vào doc của harness.

**Lỗi thật harness bắt được (sửa cùng ngày):**

1. **`jobs` không bao giờ được dọn.** Mỗi run một job `done` nằm lại vĩnh viễn —
   đúng cái "phình queue" §14.1 gọi tên. Sửa: `queue.Prune` (worker sweep mỗi
   phút, tối đa 5 000 dòng/lần, giữ `dead` cho người đọc), `JOB_RETENTION` (mặc
   định 168 h), migration `0011` thêm index partial cho `done`/`dead`.
2. **Gauge backlog quét cả bảng ở mỗi lần scrape.** `Backlog` là một câu `count(*)
   FILTER` trên toàn bảng `jobs` — với lịch sử vài triệu job, mỗi scrape 15 s là
   một seq scan; ở 2 s deadline gauge trả −1 và alert `QueueBacklog` mù đúng lúc
   cần. Viết lại thành 4 subselect, mỗi cái đi index partial của status mình;
   thêm `queue_finished_jobs` (done chưa prune) để alert `JobsTableNotPruned`
   có thứ để nhìn.
3. **Store `memory` giữ grant presign mãi** — một grant mỗi lần declare, không
   bao giờ xóa. Chỉ ở store dev, nhưng là tăng không giới hạn: prune grant hết
   hạn khi map > 1 024.
4. Không có RSS/heap/pool trong `/metrics` — không thể nói "không rò" nếu không
   đo. `metric.Process` (RSS qua psapi trên Windows, `/proc` trên Linux;
   heap/sys/GC qua `runtime/metrics`, không stop-the-world), `db.Metrics`
   (pgxpool acquired/idle/max/wait). Alert `atp-soak` trong `alerts.yml`: RSS tăng
   > 256 MiB/ngày, goroutine tăng > 50/h, pool > 80 % 10 phút, `queue_finished_jobs`
   > 1M.

**Heap của api — đã profile, không phải rò:** `OBJSTORE=memory` giữ **mọi artifact
verified trong heap của api** theo thiết kế (store dev) — 900 run × 1,9 artifact ×
~4,1 KB ≈ 7 MB. Harness chấm rò bằng **heap sống sau GC** (`go_memstats_heap_live_bytes`
= `/gc/heap/live:bytes`, thêm 17-09) **trừ đi payload của store**
(`api_heap_live_adj_mb`, đọc `sum(actual_bytes)` từ bảng `artifacts`); RSS và
`heap_inuse` răng cưa theo GC pacing (target = 2× live) và lớn gấp đôi payload —
không phải rò — nên RSS chỉ bị chặn bởi `≤ 3× live + 48 MB`. Phần còn lại được kiểm bằng heap
profile thật thay vì đoán: gắn `/debug/pprof/` lên listener metrics (loopback,
`metric.Pprof`) của cả hai binary, chạy soak 8 phút lần hai, lấy `heap` ở 100 s và
420 s, `go tool pprof -sample_index=inuse_space -base early late`: **+5,1 MB in-use,
trong đó 2,57 MB là `io.ReadAll` ← `memory.(*Store).serve`** — đúng bằng 315 run ×
7,9 KB artifact đã lưu; mọi node khác là ±512 KB (một mẫu sampling của pgx/runtime:
stmtcache LRU, `allocm`, `malg` — kích thước cố định). Không có holder nào khác
lớn lên. RSS lên nhanh hơn heap là GC pacing (target = 2× live) cộng răng cưa của
mẫu `heap_inuse`. Kết luận: api không rò ngoài payload của store dev; với
`OBJSTORE=s3` đường này bằng 0.

**T5 chỉ đạt khi:** chạy `SOAK_DURATION=24h` (tối thiểu) trên host không phải
laptop, `OBJSTORE=s3` với bucket staging (để `api_heap_live_adj_mb` không còn
phải trừ), tất cả trend `ok`, và số liệu dán vào đây. Lệnh ở trên; `SOAK_SAMPLE=1m`
cho 1 440 mẫu; nếu `api_heap_live_adj_mb` vẫn lên, lấy profile qua `/debug/pprof/heap`
của `METRICS_ADDR` trước khi kết luận.
