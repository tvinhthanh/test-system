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

**Lỗi thật thứ hai, do CI bắt (28-09):** hai test trên đỏ chập chờn trên CI từ
22-09 (local ≈ 1/5 lần khi chạy cả `./...`). Harness giờ in, khi `WaitFor` hết
giờ, các job chưa xong (trạng thái, số lần, lease còn lại, `last_error`) và
đuôi log của từng process — nhờ vậy thấy ngay: `pg_terminate_backend` giết
connection giữa job, handler lỗi, rồi **`Queue.Fail` lấy phải một connection
chết khác trong pool** → `record failure … 57P01`, job nằm `running` đến hết
lease 2 phút. Không mất dữ liệu (lease cứu), nhưng một lần DB restart làm run
treo 2 phút chưa resolve. Sửa: worker ghi lỗi thử lại tối đa 4 lần (100–300 ms;
pool đã thay connection chết) — `queue.Worker.fail`. Test kill-worker có thêm
một race của chính test: câu `Claim` của worker vừa bị giết có thể vẫn đang
chạy trên server và commit lease mới *sau* khi test "tua" lease cũ. Mỗi process
của harness giờ có `application_name` riêng (`atp-<schema>-api|worker`) và
`Harness.Terminate(w)` kết thúc backend của worker chết (chờ tới 5 s) trước
khi tua. Sau sửa: `go test ./...` 8 lần liên tiếp xanh. Job `e2e` (bị skip suốt thời gian đỏ) chạy lại và bắt thêm một lỗi UI: sidebar hỏi `GET /v1/member-scopes` ngay sau sign out → 401 — giờ chỉ hỏi khi có user.

Unit/integration test cho phần còn lại (Postgres thật):

| Kịch bản §14.3 | Test | Ghi chú |
|---|---|---|
| Duplicate event / retry cùng `(localRunId, sequence)` | `internal/ingest` `TestIngestEndToEnd` (batch gửi lại → `duplicates: 3`, finish hai lần) | §7.1, không nhân đôi |
| Hai run song song cùng fingerprint lạ | `internal/identity` `TestRaceOnUnknownFingerprint` | một `TestIdentity` |
| Reporter mất mạng → spool, run khách vẫn xanh | `reporter/playwright/test/client.test.ts`, `reporter.test.ts`; **30-09 binary thật: `outage.test.ts`** (đoạn dưới) | spool `.jsonl`, không bao giờ fail run |
| S3 timeout khi upload → artifact retry được, run hợp lệ | `internal/artifact` với `memory.Fail` injection; **30-09 trên S3 thật: xem đoạn dưới** | §10.4 |
| Worker crash giữa job → lease hết hạn, job được nhận lại | `internal/platform/queue` `TestLeaseExpiryRecoversCrashedWorker`, `TestBackoff` | `FOR UPDATE SKIP LOCKED` |

**S3/R2 timeout trên binary thật ✅ 30-09-2026.** `test/failure`
`TestObjectStoreHangKeepsRunValid`: api + worker thật với `OBJSTORE=s3` trỏ
qua một proxy TCP tới **SeaweedFS 4.48** (server S3 thật; MinIO đã ngừng phát
binary). Reporter khai báo artifact, PUT thật lên store, rồi proxy chuyển sang
*treo* — nhận kết nối, không trả lời gì, kiểu hỏng tệ nhất vì không có gì fail
nhanh. `complete` → **503 `STORE_UNAVAILABLE` sau 8,0 s**, artifact vẫn
`pending`; run vẫn finish và được xử lý (`failed`, 1 result) khi store còn
treo; store hồi → gọi lại `complete` → `verified` và worker link vào attempt.
**Lỗi thật nó bắt:** client S3 chỉ có `http.Client.Timeout` 60 s cho cả request,
nên api giữ `complete` tới 60 s mỗi lần gọi store (tới 3 lần) — reporter bỏ
cuộc ở 30 s (§7.5) và không bao giờ nhận được 503; chạy lại test trên code cũ:
client test hết giờ ở 30 s không có câu trả lời. Sửa: `s3.HeaderTimeout` 8 s cho
dial + TLS + chờ header, 60 s vẫn là trần đọc body (cap lớn nhất §10.4 là
50 MB). Cùng server đó, `internal/platform/objstore/s3` có bộ conformance lần đầu
chạy trên S3 thật (trước chỉ có 4 ví dụ chữ ký của AWS): presigned PUT, pin
content-type (gửi type khác → 403), URL hết hạn → 403, multipart gửi part lệch
thứ tự + ListParts + Complete + Get đúng byte, Abort, Put phía api, Delete hai
lần, và `TestHungStoreFailsWithinHeaderTimeout` (không cần server). CI job
`backend` chạy SeaweedFS trong container (`test/s3/seaweedfs.json`, khóa chỉ để
test) nên cả hai chạy mỗi commit. Tái lập local: backend/README "S3 conformance".

**Reporter mất mạng trên binary thật ✅ 30-09-2026.** `reporter/playwright/test/outage.test.ts`
(`ATP_OUTAGE_TEST=1`, chạy trong CI job `reporter`) chạy `playwright test` thật —
4 test × 3 s, 1 worker — ba lần: không cấu hình platform (mốc), `ATP_URL` trỏ
vào cổng đóng, và vào một server TCP nhận kết nối rồi im lặng. Cùng exit code,
đủ 4 event trong spool. **Lỗi thật nó bắt:** trước sửa, server treo cộng
**+313 s** vào bộ test 13 s (createRun 5 × 30 s + backoff, rồi từng batch lại tự
chờ đủ vòng retry, `onEnd` đợi hết) — test vẫn xanh nhưng CI của khách chờ hơn
5 phút, còn bộ test dài thì mỗi batch 5 s cộng thêm ~165 s: bất biến §7.5 thủng
trên thực tế. Sửa: request đầu dùng hết retry vì timeout/đứt kết nối/5xx thì
platform coi là down cho cả run (request sau fail ngay, PUT lên object store
không tính); không tạo được run → batch vào thẳng spool; `onEnd` chờ tối đa
`flushTimeoutMs` (30 s) rồi spool phần còn lại, bỏ upload/finish. Sau sửa: cổng
đóng **+2,1 s**, server treo **+29,9 s**.

**Còn ngoài harness:** R2/S3 của nhà cung cấp thật vẫn chờ staging (khác biệt
nhỏ giữa các server S3 — vd R2 region `auto` — chỉ lộ ra ở đó); restore backup — Gate-1
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

5. **Tạo identity cho suite mới: 5 round trip mỗi test, tuần tự** (07-10) — tra
   alias, tìm ứng viên, INSERT identity, INSERT alias, tra lại. Giờ tra alias
   một lần cho cả batch, INSERT identity + alias + đọc lại gộp một statement
   (CTE): vòng làm nóng của bài này từ 105 713 lượt tra alias xuống 1 820, hai
   INSERT riêng (≈ 101k) thành ≈ 52k. Trên laptop lời về thời gian chưa đo ra
   được (DB cùng máy, round trip ≈ 0,1 ms); lời thật ở chỗ DB cách một chặng
   mạng. Lần đầu sửa đã làm **đổi nhãn** quyết định: hai job resolve cùng suite
   gần như cùng lúc → job sau bỏ lỡ alias vừa commit → 144 lần "auto_match"
   (identity vẫn đúng) mà code cũ ghi "exact" — bắt được nhờ so `pg_stat_statements`
   với code cũ; sửa: alias đã có chủ (`ON CONFLICT DO NOTHING`) là exact, không
   rename. `TestRaceOnUnknownFingerprint` nay assert đúng 1 "new" + 7 "exact".
6. **Index trigram tìm ứng viên không có `project_id`** (07-10, migration 0029) —
   `%` khớp tên test của mọi tenant; tìm ứng viên là 70 % thời gian tạo identity
   (2,5 ms/test ở 500 test, tuyến tính theo suite → bậc hai cho cả suite mới).
   GIN `(project_id, leaf gin_trgm_ops)` qua `btree_gin`: 31 → 14 ms ở 20 project
   × 5 000 identity.

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
> 27-09 (OAuth): 70 route — `GET /v1/session/oauth/{provider}/start|callback` miễn khỏi walk (điều hướng ẩn danh, không id tenant). `cmd/api/oauth_test.go` với provider giả kiểm PKCE thật (verifier khớp challenge), state giả/thiếu cookie → `state`, người từ chối → `denied`, email lạ/chưa verify → `no_account`, tài khoản Google thứ hai cùng email → `conflict`, redirect ra ngoài → `/`, provider tắt → `disabled`, MFA vẫn hỏi. `test/arch` `TestNoOutboundHTTPFromDomainModules`: nay cho phép đúng `platform/outbound` ngoài `platform/objstore`.
> 28-09: 72 route — `GET`/`DELETE /v1/session/identities…` miễn khỏi walk (chỉ của chính người gọi); `oauth_test.go`: người khác gỡ → 404, gỡ cách đăng nhập cuối → `409 LAST_SIGN_IN`, không phiên → 401.
> 28-09 (mời qua email): 80 route, đi 61 — `…/{projects|workspaces|organizations}/{id}/invitations[/{invitationId}]` vào walk với lời mời thật của org A (fixture `invitationId`); `POST /v1/invitations/lookup|accept` miễn (ẩn danh, theo token). `TestInvitations`: 202 + mail có link fragment, lookup/accept, mật khẩu yếu → 400, token dùng lại → 404, email đã có tài khoản → cấp role thẳng, rút lại → link chết, mail lỗi → không còn lời mời, org admin mời owner → `OWNER_ONLY`, tên org có CR/LF không vào header, 6 token sai → 429, audit đủ. `platform/mail`: header injection bị từ chối, STARTTLS bắt buộc khi cấu hình.
> 05-10 (xóa / khôi phục tổ chức, `org.manage`): 104 route — `DELETE /v1/organizations/{id}` và `POST …/{id}/restore` vào walk; `GET /v1/organizations/deleted` miễn (chỉ của người gọi; walk kiểm B nhận danh sách rỗng). `TestDeleteAndRestoreOrganization`: org admin 403, owner org khác 404, sai tên `CONFIRM_NAME`; xóa → project không còn trong danh sách, runs / settings / members org + workspace / meter → 404, tạo project trong workspace → 404, `member-scopes` không còn org, CI token → 404, xóa lần hai 404, org bên cạnh vẫn 200; chỉ owner thấy danh sách đã xóa, admin và người lạ khôi phục → 404; khôi phục → project và CI token trở lại; 2 dòng audit. `TestPurgeDeletedOrganization` (worker): ba tổ chức, một project xóa riêng trước với cửa sổ 90 ngày; đọc tenancy (org, workspace, scope, list) ẩn ngay; tenancy từ chối purge org sống và org còn project; trong cửa sổ không purge; sau cửa sổ: org, workspace, project, run, membership / lời mời tầng org + workspace, token project = 0 cho org bị xóa, hai org kia nguyên; 3 dòng audit purge; lần hai không làm gì.
> 05-10 (cấu hình project, `project.configure`): 101 route — `GET|PATCH /v1/projects/{id}/settings` vào walk (body PATCH hợp lệ: `retention.video = 1`). `TestProjectSettings`: member đọc được, sửa 403; video 8 ngày trên gói free (7) → `RETENTION_ABOVE_PLAN` và tên đi kèm **không** đổi; kind lạ 400, tên trùng 409, tên rỗng 400; đổi tên + nhánh + video 3 + trace 0 → 200; lặp lại cùng body không ghi audit (đúng 1 dòng, có `name` và `retention.video [7, 3]`). `TestProjectRetention` (module): screenshot được declare sau khi đặt 3 ngày hết hạn sau 3 ngày, evidence 30; override 120 dưới gói free vẫn là 90; `0` về gói; purge xóa override.
> 29-09 (xóa / khôi phục run, `data.delete`): 99 route. `TestDeleteAndRestoreRun`: member 403, **workspace admin 403** (§11.3.2), run đang mở 409 `RUN_OPEN`; xóa → danh sách chỉ còn run kia, chi tiết / kết quả / result / triage 404, xóa lần hai 404; khôi phục → quay lại; 2 dòng audit. `TestPurgeDeletedRun`: flaky signal 2 run/1 fail → sau khi xóa run fail và sweep: 1 run/0 fail; hết hạn → run, result, event, artifact (và object), triage của result đó về 0, run kia và identity nguyên vẹn, audit `run.purge`. live-check: xóa run JUnit, thấy trong "Recently deleted", khôi phục.
> 29-09 (xóa / khôi phục project, xóa mềm): 96 route — `DELETE /v1/projects/{id}`, `GET …/projects/deleted`, `POST …/projects/{id}/restore` vào walk. `TestDeleteAndRestoreProject`: maintainer không xóa được, sai tên → `CONFIRM_NAME`; xóa → runs 404, danh sách không còn, CI token ingest → 404, xóa lần hai 404; tên dùng lại được, khôi phục khi tên bị chiếm → 409; khôi phục → token ingest lại được; 2 dòng audit. `cmd/worker TestPurgeDeletedProject`: dữ liệu thật ở 12 bảng của 8 module + object trong bucket; project còn trong hạn không bị đụng; hết hạn → cả 12 về 0, object mất khỏi bucket, project hàng xóm và project đang chờ nguyên vẹn, lịch sử meter giữ, một dòng audit `project.purge`, lượt purge thứ hai không làm gì.
> 29-09 (tạo project): 93 route — `POST /v1/workspaces/{id}/projects` vào walk (tầng workspace). `TestCreateProject`: `member-scopes` cho workspace admin có `project.manage`; maintainer project → từ chối, workspace khác → từ chối, tên rỗng 400, trùng 409; tạo → 201, project mới trong danh sách của người tạo và có `token.manage`.
> 29-09 (ingest token trên dashboard): 92 route — `GET|POST …/tokens`, `DELETE …/tokens/{tokenId}` vào walk (fixture `tokenId` = token của org A). `TestProjectTokens`: member 403 (xem, phát, thu hồi), tên rỗng / quyền lạ / hạn 400 ngày → 400; maintainer phát → secret `atp_ing_…` ingest được thật (201), danh sách không chứa secret, prefix khớp; maintainer project khác thu hồi qua project của mình → 404; thu hồi → 204, token đó ingest → 401, danh sách ghi `revokedAt`, một dòng audit với email người làm. live-check trên compose: phát, thấy secret một lần, thu hồi, thấy "revoked".
> 29-09 (đổi password, §11.2): 89 route — `POST /v1/session/password` miễn khỏi walk (chỉ password của chính người gọi). `TestChangePassword`: sai current → 400 `WRONG_PASSWORD`, ngắn/giống cũ → 400, thiếu CSRF → 403, ẩn danh → 401; đổi xong session này còn, session kia mất, password cũ 401, mới 200, một dòng audit; 5 lần sai → 429 kể cả khi nhập đúng; tài khoản không password → `ErrNoPassword`. live-check chạy thật trên compose: sai → 400 hiện trong form, đổi, đổi lại, vẫn đăng nhập.
> 28-09 (shadow meter, QĐ-13): 88 route — `GET /v1/organizations/{id}/meter` vào walk (tầng org). `TestOrgMeter`: owner (có MFA) đọc, org admin 403, owner org khác 404, chưa ghi gì → rỗng + `ready: false` kèm lý do; sau một lần ghi như worker → đúng một ngày. `billing`: phán quyết §10.6 (28 ngày đều → ready; một đợt merge 40 identity → đúng ngày đó bị cờ, ngày sau không; merge rồi undo → cả hai ngày), ghi lại một ngày không nhân đôi, tổng theo org, org khác không thấy. `query.TestMeterFacts`: project không hoạt động vẫn có hàng 0; merge làm active giảm 2 → 1 trong khi raw giữ 2 — đúng chuyển động mà meter phải bắt.
> 28-09 (ROI, QĐ-13): 87 route — `GET …/roi`, `GET|POST …/roi/baselines` vào walk (body baseline hợp lệ, chỉ tenant check từ chối). `TestROIOverHTTP`: viewer đọc được, không baseline → `baseline`/`savedHours` null (không bao giờ 0 mặc định), member ghi → 403, maintainer → 201 + `enteredBy`, tháng sai/tương lai → 400. `testmgmt`: phép tính §6.8.3 (âm hiện âm, không chia 0), prospective thắng retrospective, 8 input sai → `ErrInvalid`, project B không thấy baseline của A; `query.TestROIFacts`: 3 failure, TTRC ≈ 2 h, 2 s retry (chỉ attempt fail), 1 marked flaky, cửa sổ trước và project khác = rỗng. live-check CI bắt được một lỗi thật: `<input type=number>` qua v-model trả số → `.trim()` nổ khi lưu baseline và khi gõ đơn giá — đã sửa, có unit test.
> 28-09 (JUnit, QĐ-13): 84 route, đi 62 — `POST /v1/projects/{id}/imports/junit` vào walk (tenant check trước khi đọc body); `POST /v1/imports/junit` miễn như `POST /v1/runs` (token nói project) và kiểm riêng: token B import với `localRunId` của A → 409 `RUN_CONFLICT` (id đó đã là run reporter của B), import bình thường → run của B, A vẫn thấy đúng 1 run. `internal/ingest`: parser (Surefire flaky/rerun, pytest `[...]`, Jest nested, XXE/billion-laughs bị từ chối), pipeline thật (tier 2 resolve 3/3, flaky_pass 2 attempt, re-import = 0 thêm, 0 job), 9 lỗi → mã đúng và 0 run lưu; `cmd/api` member 403, maintainer 201, thiếu CSRF 403, `importedBy`.
> 28-09 (quên mật khẩu): 82 route — `POST /v1/session/password/forgot|reset` miễn (ẩn danh). `TestPasswordReset`: email có/không có tài khoản nhận câu trả lời giống hệt byte-by-byte, chỉ một mail; yêu cầu mới vô hiệu link cũ; mật khẩu yếu không tiêu token; đặt lại → phiên cũ 401, mật khẩu cũ 401, mới 200; link dùng lại → 404; 6 yêu cầu cho một email không tồn tại → 429; tắt mail → `PASSWORD_RESET_OFF`.

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
| API8 Misconfig | checklist config | config từ chối prod không `sslmode=verify-full|verify-ca` (28-09: trước chỉ chặn `disable`, nên thiếu sslmode = `prefer` lọt qua), pepper dev, `COOKIE_SECURE=false` ngoài dev ✅; container non-root/pin/healthcheck + TLS proxy + `.env.example` (`deploy/`, dựng thật 17-09) ✅; `httpx.BrowserPolicy` — `nosniff`, `no-store`, `Referrer-Policy`, `X-Frame-Options`, HSTS khi cookie Secure, CORS chỉ cho `DASHBOARD_ORIGINS` (không bao giờ `*`, preflight từ origin lạ → 403), `TestBrowserPolicyHeadersAndCORS` ✅ |
| API9 Inventory | chỉ `/v1/`, diff mỗi release, token `lastUsedAt` | **mới**: `deploy/routes.txt` = inventory, `TestRouteInventoryMatchesFile` fail nếu route mount khác file ✅; `api_tokens.last_used_at` cập nhật ≤ 5 phút/lần ✅. **`deploy/openapi.yaml`** (OpenAPI 3.1, 21 operation, cả hai nửa reporter + dashboard) là nguồn sự thật từ 17-09. Bản nháp đầu bị review (`docs/openapi-contract-reimplementation.md`) bắt 2 lỗi thật — cookie tên `atp_sess` (server đọc `atp_session`; `atp_sess_` chỉ là prefix của *giá trị*) và `sequence` khai `minimum: 1` trong khi validator nhận 0 — sửa 18-09: doc đúng, validator từ chối `sequence <= 0` (`TestEventSequenceBoundary`: thiếu/−1/0 → 400 `INVALID_PAYLOAD` `events[0].sequence`; 1 và 42 nhận; lặp 1 → duplicate, không lưu hai lần), test parse **cấu trúc** bằng `gopkg.in/yaml.v3` (không còn scanner theo indent): `TestOpenAPIMatchesMountedRoutes` hai chiều, `TestOpenAPIBindsSharedLiterals` (cookie = `authz.SessionCookie`, header = `authz.CSRFHeader`, `atp_csrf` được gọi tên, `Event.sequence` minimum 1 và required), `TestOpenAPIIsInternallyConsistent` (144 `$ref` resolve, 21 `operationId` duy nhất); mã lỗi trong doc đối chiếu từng handler (`INVALID_CREDENTIALS`, `AUTH_UNAVAILABLE`, `TOKEN_SCOPE`, `INVALID_QUERY`…); `redocly lint` xanh trong CI; `routes.txt` giữ lại làm inventory phẳng để diff nhanh |
| API10 Unsafe consumption | LLM trả rác → bị loại | P1.5 (chưa có LLM) — N/A |

**Dependency review 17-09:** `govulncheck ./...` — 1 lỗ hổng trong module
(`golang.org/x/text` 0.31 → đã lên 0.39) và **18 advisory stdlib** fix ở Go
1.25.6–1.25.13; laptop chạy 1.25.5 nên `go.mod` thêm `toolchain go1.25.13` —
CI/image (GOTOOLCHAIN=auto) build bằng bản đã vá, laptop pin `GOTOOLCHAIN=local`
không đổi. `npm audit` reporter + frontend (kể cả dev deps): 0. Lặp lại trước
mỗi release; kết quả ghi vào đây.

**Dependency review 13-10:** CI 96607e6 đỏ ở govulncheck — 10 lỗ hổng *được gọi*
(stdlib `net/http`… + 1 module) mà nhánh 1.25 không còn nhận bản vá. Nâng
`toolchain go1.26.9` (dòng `go 1.25.5` giữ nguyên), `golang.org/x/text` 0.39 →
0.41, `x/sync` 0.21 → 0.22; CI `setup-go` + `golang:1.26.9-alpine` theo cùng;
govulncheck pin v1.1.4 → v1.9.0. Local với go1.26.9: `govulncheck ./...` → **0
được gọi** (18 advisory nằm trong module require nhưng code không gọi), build/vet/
gofmt/unit sạch.

**Dependency review 21-10:** lần rà soát cuối trước bàn giao, `govulncheck ./...`
trên go1.26.9 báo **1 lỗ hổng được gọi**: GO-2026-6809 (`crypto/x509` tin nhầm
chứng chỉ trung gian đã bị thu hồi), đường gọi `mail.SMTP.Send` →
`tls.DialWithDialer` → `x509.Certificate.Verify`. Vá ở go1.26.10: `toolchain
go1.26.10` (dòng `go 1.25.5` giữ nguyên), CI `setup-go` + `golang:1.26.10-alpine`
theo cùng; govulncheck vẫn v1.9.0. Local với go1.26.10: `govulncheck ./...` →
**0 được gọi** (18 advisory còn lại nằm trong module require nhưng code không
gọi); gofmt/vet/`go test ./...` trên Postgres thật sạch. `npm audit` reporter: 0. Frontend (phần của agent frontend):
`vite` 6.4.3 dính GHSA-698v-4r3x-3vcv / GHSA-r825-w8mm-5pxw / GHSA-587h-7wmj-h5pm
(launch-editor, high) — job frontend của CI sẽ đỏ ở `npm audit` cho tới khi nâng
lên vite 6.4.5.

**Đối chiếu CI ngày 15-10:** [run #42 / 38384680965](https://github.com/tvinhthanh/test-system/actions/runs/38384680965)
trên `76f6df0` đã completed/success. Backend pass govulncheck, test Postgres/S3,
Docker build, Compose config, promtool và OpenAPI; frontend/reporter/E2E đều
pass. Log E2E có cả `live-manual-history.mjs` và `live-case-bulk.mjs` thành công.
Scan lỗi cũ có **9 advisory stdlib + 1 x/text**, tổng 10, không phải 10 stdlib
cộng thêm 1 module. CI dùng memory artifacts nên không thay bằng chứng restart,
backup/restore database + artifact trên host demo/pilot. Preflight chỉ đọc mới
ở `server/atp.sh doctor --pilot` phát hiện Docker engine ngừng/memory/dev trên
laptop; fake-Docker test của lệnh không phải kiểm chứng deployment.

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

**Postgres TLS ✅ 28-09:** compose giờ chạy được `APP_ENV=prod` nguyên trạng.
`cmd/pgtls` (stdlib, trong image) tạo CA riêng + cert server (`postgres`,
`localhost`, `127.0.0.1`) vào volume `pgtls`, idempotent (giữ CA, cấp lại cert
khi còn < 30 ngày hoặc đổi tên); `deploy/postgres/pg_hba.conf` chỉ nhận
`hostssl` + SCRAM, `hostnossl … reject`; api/worker/migrate/admin/backup nối
`sslmode=verify-full&sslrootcert=/tls/ca.crt`. Kiểm trên Docker Desktop từ volume
rỗng: 6/6 connection của app là TLSv1.3 (`pg_stat_ssl`); `sslmode=disable` →
`pg_hba.conf rejects … no encryption`; `verify-full` tới IP container → "does
not match host name"; `backup.sh` chạy qua TLS; `migrate` dưới `APP_ENV=prod`
chạy, prod + `sslmode=disable` dừng ngay lúc boot. Lần kiểm đầu bắt một lỗi:
key `70:70 0600` làm `pgtls` (root không `CAP_DAC_OVERRIDE`) không đọc lại được
→ cấp cert mới mỗi lần `up`; giờ key là `root:70 0640` (dạng thứ hai Postgres
chấp nhận).

**Còn mở (§11.5, §11.7):** copy volume `backups` ra khỏi host;
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

## Rà soát bảo mật phần xác thực — 28-09-2026

Đọc lại toàn bộ bề mặt §11.2/§11.3 làm trong 22→28-09 (MFA, OAuth, lời mời, quên mật khẩu, luật owner, `platform/outbound`, `platform/mail`) tìm lỗ ở **chỗ các phần gặp nhau**. Mỗi phát hiện có test hồi quy.

| # | Mức | Phát hiện | Sửa | Test |
|---|---|---|---|---|
| 1 | Cao | **Open redirect sau OAuth**: `landing()` để lọt tab/xuống dòng (`/%09/evil.example`); trình duyệt bỏ chúng khỏi `Location` → `//evil.example`. Xác nhận bằng Chromium thật. Kẻ gian gửi link `…/oauth/google/start?redirect=…`, nạn nhân đăng nhập Google thật rồi bị đưa sang trang giả | từ chối mọi ký tự điều khiển và mọi `\`, rồi parse lại: không scheme/host/user | `TestLandingStaysInTheApp`, danh sách `evil` trong `TestOAuthSignIn` |
| 2 | Cao | **Giới hạn theo IP là một bộ đếm chung sau Caddy**: `clientIP` = `RemoteAddr` = Caddy cho mọi request → 5 lần sai từ bất kỳ ai khóa đăng nhập của **mọi người** (15 phút, gấp đôi tới 24 h); token lời mời sai cũng đếm chung. Có từ P1-min, các tính năng mới làm nặng thêm | `httpx.RealIP` + `TRUSTED_PROXIES` (compose đặt mặc định) — chỉ tin `X-Forwarded-For` từ proxy tin cậy, đi từ phải sang | `TestRealIPTrustsOnlyTheProxy`, `TestLoginLimitCountsEachClientBehindAProxy` |
| 3 | Trung bình | **argon2 trước rate limit** ở `accept` lời mời và `reset` mật khẩu: route ẩn danh, mỗi request tốn ~19 MiB + CPU trước khi bị chặn | băm sau khi qua giới hạn | `TestInvitations`: bị khóa thì cả mật khẩu yếu cũng nhận 429 |
| 4 | Thấp | Hai người mời cùng email cùng lúc → người thua nhận "tài khoản chưa verify" (sai) | `ErrAccountExists` tách khỏi `ErrConflict` → `409 CONFLICT` | build + test lời mời |
| 5 | Thấp | `forgot`: email có tài khoản tốn thêm vài ms tra/ghi DB trước khi trả lời (mail đã gửi nền) | **Sửa 28-09 (sau)**: handler chỉ đếm giới hạn rồi trả `202`; tra tài khoản, ghi token và gửi mail chạy sau câu trả lời (`CountPasswordResetRequest` / `IssuePasswordReset`) | `TestPasswordReset`: giữ SMTP vẫn trả 202 ngay; phần DB bảo đảm bằng cấu trúc handler |
| 6 | Chấp nhận (thiết kế §11.2) | Ai biết email cũng khóa được tài khoản đó 15 phút+ (giới hạn "theo account") | giữ; quên mật khẩu mở khóa | `TestPasswordReset` |

Đã kiểm và **không** có lỗ: CSRF trên mọi route ghi (kể cả ẩn danh: double-submit + Origin), login CSRF qua OAuth (state gắn cookie), phát lại mã TOTP, recovery code dùng một lần, token (mời/reset/challenge) chỉ lưu HMAC và không đi qua URL server thấy, access log chỉ ghi path, liên kết OAuth chỉ khi email verify **cả hai phía**, email đã có tài khoản không bị link chiếm, owner không tắt được MFA, owner mới bị giữ ở bước bật MFA, owner cuối không bị hạ (khóa hàng), gọi ra ngoài chỉ tới host lấy từ cấu hình, header mail không nhận CR/LF.

## Rà soát cuối API + frontend — 21-10-2026

Chạy lại mọi gate chạy được trên laptop (Postgres 17.5 thật, cụm initdb trong scratchpad): gofmt/vet sạch, `go test ./...` 29 package pass, 5 skip đều là test S3 cần SeaweedFS (CI chạy); reporter 20 pass + 1 opt-in (outage, CI chạy); `server/tests/doctor.sh` 11/11; frontend typecheck, 103/103 vitest, build + bundle gate 54,9 KiB gzip (build ra scratchpad, không đụng `frontend/dist`). Sau đó đọc phần backend viết 13–15-10 (testmgmt: lịch sử thực thi, phạm vi plan, audit case, taxonomy, CSV/export, specialized results, migration 0030–0034) và đối chiếu từng decoder của frontend với giới hạn thật của API.

| # | Mức | Phát hiện | Sửa | Test |
|---|---|---|---|---|
| 1 | Cao | Tạo/sửa plan và ghi execution đọc body bằng `decodeSmall` (16 KiB), trong khi hợp đồng cho `caseIds` tới 5000 (~200 KB) và 200 `stepResults` × 4000 byte: plan từ ~420 case, hoặc vài bước ghi "actual" dài (UI cho 4000 ký tự mỗi bước), nhận `400 malformed JSON` | `decodeBounded` theo route: 256 KiB cho plan, 2 MiB cho execution; vượt → `413 PAYLOAD_TOO_LARGE`, ghi trong openapi | `TestPlanAndExecutionBodiesUpToTheContract` (đỏ trước khi sửa: plan 500 case → 400) |
| 2 | Cao (gate CI) | govulncheck: GO-2026-6809 được gọi qua `mail.SMTP.Send` | toolchain go1.26.10 (dependency review 21-10 ở trên) | govulncheck: 0 được gọi |
| 3 | Trung bình | `executedAt` của execution do client gửi, không chặn tương lai; verdict của case là execution có `executed_at` mới nhất, nên một bản ghi đề năm 2099 (nhầm đồng hồ hay cố ý qua API) ghim verdict mãi mãi | quá hiện tại > 5 phút → `400` (lệch đồng hồ 5 phút như specialized report); ghi lùi ngày vẫn nhận | `TestExecutionSnapshotRetestAndHistory`: +1 h → `ErrInvalid` (đỏ trước khi sửa), −48 h nhận và không thành verdict |
| 4 | Trung bình, chờ quyết định | `GET /v1/releases/{id}/gate`: openapi bảo CI "dừng khi `gate.verdict == blocked` trừ khi có override", nên CI vẫn qua khi người quyết `blocked` mà gate `approved`, khi chưa evaluate (`gate: null`) và khi `no_evidence`. Đoạn `jq` trên trang release làm đúng như doc | Đề xuất: `status` approved/shipped → qua; `blocked` → dừng; `open` → chỉ qua khi verdict `approved`/`warn` | — |
| 5 | Thấp | Body case POST/PATCH giữ 512 KiB, dưới mức lý thuyết 200 bước × 4000 byte; openapi ghi `maxLength` (ký tự) nhưng server đếm byte UTF-8, nên chữ tiếng Việt gần ngưỡng bị từ chối dù UI cho nhập | giữ, ghi lại | — |

Giao cho agent frontend (không sửa ở đây): vite 6.4.3 → 6.4.5 (npm audit high, job frontend sẽ đỏ); ô chọn identity để link automation (và merge) chỉ có trang đầu 20 identity, lọc phía client; decoder đặt trần mà API không có (evidence mỗi execution 50, suites 500, link mỗi case 100, requirement mỗi case 500, tokens 500, lỗi import 5000), vượt trần là cả trang báo "unexpected data format"; danh sách test case dừng ở 500 mà không báo.

## QĐ-14 — tester không cần source — 23-10-2026

Environment có địa chỉ site (migration 0036) và plan chọn environment (0037), URL check (`internal/urlcheck`, 0038, service `checker/`), reporter phát từ `/downloads/`, trang hướng dẫn. Đã chạy trên laptop:

| Kiểm | Kết quả |
|---|---|
| `go test ./...` trên Postgres 17.5 | 30 package pass; gofmt/vet sạch; test kiến trúc pass (`urlcheck` chỉ import `platform/*`, outbound HTTP chỉ qua `platform/outbound`) |
| Tenant walk §14.3 | 7 route mới được phân loại và đi: environment, URL check (cả ảnh chụp) của org A không đọc/ghi được từ org B, kể cả khi B dùng project của mình với id của A |
| `TestEnvironments`, `TestURLChecks` | member thêm/sửa, viewer 403, URL có user:password/`javascript:`/thiếu host → 400, environment của project khác → 400, plan đóng không đổi environment (409), mọi thay đổi có audit; `URLCHECK=off` → 409 mà vẫn đọc check cũ; check thứ 6 đang chờ → 429; ảnh trả `image/jpeg` + CSP `sandbox` |
| `internal/urlcheck` | job chạy đúng một lần; checker lỗi → queue retry, lần cuối đóng `error` (không kẹt `queued`); chỉ lưu ảnh đúng JPEG; prune 90 ngày, purge theo project (thêm vào `cmd/worker` purge test) |
| `checker` (`node --test`, Chromium thật) | 8/8: trang sạch pass; trang lỗi bắt lỗi JS, ảnh 404, link 404, axe `image-alt`; fetch tới `10.1.2.3` và link tới `169.254.169.254` **bị chặn và không tính là lỗi trang**; `127.0.0.2` và `localhost` không mở được dù `127.0.0.1` được phép cho fixture |
| Image `checker` (Docker Desktop) | build 51 s; container `cap_drop ALL` + `no-new-privileges`: example.com → báo cáo 9 mục trong 5,6 s; `http://169.254.169.254/latest/meta-data/` → "Not checked: … link-local" |
| Stack local không Docker (api + worker + checker + Vite, Playwright lái UI) | tạo environment trong form plan → plan hiện địa chỉ; URL check của environment → báo cáo + 2 ảnh; check `http://10.0.0.1/admin` → mục "Not opened"; 0 lỗi console; rộng 390 px không cuộn ngang |
| Frontend | typecheck sạch, 107/107 vitest (thêm `environments-url-checks.test.ts`: decoder từ chối địa chỉ không phải http(s)) |
| `server/tests/doctor.sh`, `caddy validate`, `docker compose config` (có và không `--profile urlcheck`) | 11/11, hợp lệ, hợp lệ |

**Lỗi tìm ra khi chạy thật:** worker chết lúc khởi động khi có hai pool hàng đợi (`metric: "queue_jobs_total" registered twice`) — registry từ chối tên đăng ký hai lần, trước đây worker chỉ có một pool. Sửa: các `queue.Worker` cùng registry dùng chung series (nhãn `kind` đã phân biệt); test `TestWorkersShareMetrics`.

**Chưa kiểm:** `atp.sh up` với `URLCHECK=on` trên Docker (auto mode chặn lệnh deploy; cần chủ dự án chạy), URL check tới site LAN với `URLCHECK_ALLOW_PRIVATE=on`, `npm install` reporter qua `https://<host>/downloads/` với CA nội bộ.

## QĐ-15 — Start testing, case gợi ý (thư viện, bộ mẫu, AI), chạy từng case — 24-10-2026

Plan có loại test (0039), module `aisuggest` (0040), `platform/llm` (anthropic-sdk-go v1.84.0), `platform/secretbox`, checker `/outline`, audit `organization.ai_settings` (0041); frontend Start testing, màn hình chạy, AI settings. Model luôn chọn trên giao diện; key riêng của organization hoặc key server `LLM_API_KEY` (không có `LLM_MODEL`).

| Kiểm | Kết quả |
|---|---|
| `go test ./...` (Postgres 17.5) | 33 package pass; gofmt/vet sạch; test kiến trúc mới `TestLLMOnlyThroughPlatform` (SDK chỉ ở `platform/llm`) |
| `platform/llm` với server Anthropic giả | request đúng: model, `max_tokens` 16000, `output_config.format` json_schema + effort medium, `fallbacks: "default"` + beta `server-side-fallback-2026-07-01` (không gửi cho Haiku), không gửi `thinking`; 401→ErrKey, 404→ErrModel, refusal, max_tokens, answer không phải JSON |
| `aisuggest` | key riêng niêm phong (không có plaintext trong DB, pepper khác không mở), kiểm key trước khi lưu, chọn key server thì xóa key riêng; job dùng đúng key + model, outline + mô tả vào prompt, bản nháp cắt theo count/độ dài; key sai → error ngay, outage → retry rồi error; 2 chờ/project |
| API + tenant walk | 6 route mới: admin org lưu/xóa, tester 403 ở settings, viewer 403 khi xin gợi ý, 409 khi không có key, environment của project khác 400; walk phủ cả 6 |
| checker `/outline` | Chromium thật: tiêu đề/heading/link cùng site/form/nút; 127.0.0.2 bị chặn |
| govulncheck v1.9.0 (go1.26.10) | 0 được gọi (18 advisory trong module require, không gọi — như trước khi thêm SDK) |
| Stack local + Playwright | admin lưu key (key sai → thông báo rõ), chọn key server + Haiku; tester: Smoke → environment → bộ mẫu + 3 bản nháp AI (server Anthropic giả nhận outline, schema, fallback) → plan → màn hình chạy mở example.com, ghi passed tự sang case kế, URL check từ màn hình chạy; lần chạy lại: bản nháp trùng thư viện không tích sẵn; 0 lỗi console, 390 px không cuộn ngang |
| Frontend | typecheck sạch, 111/111 vitest |

**Chưa kiểm:** một lần gọi Claude thật (cần key của chủ dự án), `atp.sh up` trên Docker với stack mới.

## QĐ-16 — Auto test: case server tự chạy, AI soạn và sửa bước, quét site, chạy từ CI — 25-10-2026

Module `autotest` (0042: tài khoản test theo environment, spec, bản nháp, run, case của run), quét site trong `urlcheck` (0043), checker `/sessions` + `src/steps.mjs` (bước là dữ liệu, diễn giải bằng locator Playwright, trình duyệt giữ trong site), `llm.JSON` chuyển sang streaming (accumulate), quyền token `project:autorun`; frontend trang Auto test, Auto run, Site scan. Kết quả mỗi run vào plan (execution + ảnh) và test run (JUnit import nội bộ, case được link vào test).

| Kiểm | Kết quả |
|---|---|
| `go test ./...` (Postgres 17.5) | 34 package pass; gofmt/vet sạch; test kiến trúc giữ nguyên (autotest chỉ dùng platform/* + tenancy) |
| `autotest` (DB thật, trình duyệt/LLM/bộ ghi giả) | mật khẩu niêm phong và mở đúng, cập nhật không mất mật khẩu; spec từ chối biến không có, `{{username}}` không tài khoản, `javascript:`, bước sai hình; bản nháp: đăng nhập tìm bằng AI trước, đọc trang, bỏ bước/case không chạy được; **mật khẩu không bao giờ có trong prompt nào**; run: case pass, case sửa bước (AI thay "Send" bằng "Gửi"), case fail kiểm tra (ảnh chỗ fail, bước sau skipped), tổng kết, bộ ghi gọi đủ; ApplyHeal một lần; đăng nhập sai → case error, tài khoản failed; giới hạn 2 run; export Playwright không để chữ trong bước thành code |
| `llm` streaming | server Anthropic giả trả SSE (2 delta): request có `stream: true`; refusal, max_tokens, không phải JSON, 401 vẫn phân loại đúng |
| `urlcheck` quét site | theo tầng, cùng host, bỏ link đăng xuất/xóa/file, ≤ maxPages, ≤ 3 tầng, đóng khi hết trang chờ, đăng nhập mỗi trang, trang của scan không vào danh sách URL check |
| Bộ ghi (worker, ingest + testmgmt thật) | execution có bước, trạng thái, người chạy "ATP auto test · …", 2 ảnh bằng chứng; JUnit import → test run `failed` đúng `localRunId`; resolve tầng 2 rồi link case↔test, ghi lại lần hai không trùng link; plan đã đóng → bỏ qua |
| API + tenant walk | 22 route mới: viewer 403 khi ghi, checker tắt → 409, không key → 409, spec sai không để lại case (cùng transaction), lưu spec viết lại bước chữ của case, run tạo plan, token ingest không khởi động run (403), token autorun 202/200, run thứ ba 429, quét cần tài khoản đã đăng nhập được; walk phủ đủ (router 123 → 145 route) |
| checker (Chromium thật) | 15/15: site chat cần đăng nhập — đăng nhập, gửi tin, khớp chính xác trước ("Message" thắng "Messages"), lỗi locator/assert/input, rời site → fail và quay về, URL check đăng nhập + link cùng site |
| Stack local + Playwright | environment trên site chat (IP LAN, `URLCHECK_ALLOW_PRIVATE=on`), tài khoản test → AI tìm bước đăng nhập (model không thấy mật khẩu), AI soạn 3 case chạy được (case có bước `eval` bị bỏ), Keep and run → 3/3 pass, 1 bước AI sửa, giữ bản sửa vào case; plan + test run có đủ 3; export 74 dòng không có mật khẩu; quét site đăng nhập: 7 trang, link hỏng/404/ảnh thiếu đúng chỗ, không mở đăng xuất/xóa tài khoản; CI token autorun → run 3/3 pass, 0 bước phải sửa (bản sửa đã vào case); case viết tay + sửa + chạy; 0 lỗi console, 390 px không cuộn ngang |
| Frontend | typecheck sạch, 115/115 vitest trên phần commit, bundle gate: JS ban đầu 59,6 KiB gzip |

**Chưa kiểm:** một lần gọi Claude thật (soạn case, tìm đăng nhập, sửa bước), checker trong Docker với phiên trình duyệt, site thật ngoài mạng nội bộ.
