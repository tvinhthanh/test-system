# backend — Go monolith, P1-min

> Khởi công **11-09-2026** theo **QĐ-11** (SPEC §1.11): xây P1-min song song Phase 0.
> Bố cục theo SPEC §6.9. Luật phụ thuộc §6.9.3 được `test/arch/` cưỡng chế — không phải code review.

## Chạy

```bash
cd backend
go run ./cmd/migrate             # áp migration còn thiếu rồi thoát (§8.10)
go run ./cmd/api                 # HTTP :8080 + /metrics ở 127.0.0.1:9090
METRICS_ADDR=127.0.0.1:9091 go run ./cmd/worker   # tier-2 resolve + auto-close; bắt buộc chạy cùng api
go run ./cmd/admin org create -name Acme -slug acme      # rồi ws create, project create, token issue
curl -i localhost:8080/healthz   # {"status":"ok"} + X-Request-Id
curl localhost:9090/metrics      # Prometheus text format (§6.4)
go test ./...                    # gồm test kiến trúc; test cần Postgres tự skip nếu thiếu TEST_DATABASE_URL
TEST_DATABASE_URL=postgres://user:pw@localhost:5432/atp_test?sslmode=disable go test ./...
```

Bề mặt HTTP công khai: [`deploy/openapi.yaml`](deploy/openapi.yaml) (OpenAPI 3.1; test
so với router thật, lint trong CI). Staging đầy đủ bằng container (Postgres, migrate, api, worker, Prometheus, Caddy TLS,
backup hằng ngày): [`deploy/README.md`](deploy/README.md) — `docker compose -f
deploy/compose.yml --env-file deploy/.env up -d --build`. CI: `.github/workflows/ci.yml`.

Biến môi trường (`internal/platform/config`): sai giá trị → thoát ngay lúc boot, mã 2.

| Biến | Mặc định | Ghi chú |
|---|---|---|
| `APP_ENV` | `dev` | `dev\|staging\|prod`; `prod` từ chối `sslmode=disable` |
| `HTTP_ADDR` | `:8080` | API |
| `METRICS_ADDR` | `127.0.0.1:9090` | listener riêng cho `/metrics` — label theo project là dữ liệu nội bộ, không đi chung cổng với tenant |
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/atp?sslmode=disable` | `cmd/api` ping lúc boot, không có DB thì không lên |
| `TOKEN_PEPPER` | *(pepper dev, ≥ 32 byte)* | khóa HMAC cho hash token (§11.4). `staging`/`prod` bắt buộc đặt riêng, từ chối giá trị dev |
| `OBJSTORE` | `memory` | `s3` cho staging/prod (`OBJSTORE_ENDPOINT`, `OBJSTORE_REGION` mặc định `auto` cho R2, `OBJSTORE_BUCKET`, `OBJSTORE_ACCESS_KEY`, `OBJSTORE_SECRET_KEY`). `memory` chỉ dev: mỗi process một bucket riêng, mất khi tắt |
| `OBJSTORE_MEMORY_ADDR` | *(trống → cổng loopback ngẫu nhiên)* | chỉ với `memory`: bucket giả nghe ở địa chỉ cố định (vd `:9100`) và `OBJSTORE_ENDPOINT` là URL công khai của nó (vd `https://localhost/_objstore`) để reverse proxy chuyển PUT từ ngoài container vào — `deploy/compose.yml` + Caddy dùng cho dev/CI không có bucket. Vẫn chỉ dev |
| `DASHBOARD_ORIGINS` | `http://localhost:5173,http://127.0.0.1:5173,http://localhost:3000,http://localhost:8080` (5173 = Vite dev của `frontend/`) | allowlist Origin/Referer cho request đổi trạng thái bằng cookie (§11.2); không bao giờ `*` |
| `COOKIE_SECURE` | `true` | `false` chỉ dev (§11.5 TLS mọi nơi) |
| `DASHBOARD_RATE_PER_MINUTE` | `600` | trần request dashboard mỗi user đã đăng nhập, burst 2× (§11.6 API4); `0` tắt — chỉ cho test |
| `JOB_RETENTION` | `168h` | job queue đã `done` giữ bao lâu trước khi worker xóa (sweep mỗi phút, tối đa 5 000 dòng/lần; job `dead` không xóa) — bảng `jobs` không được lớn theo lịch sử (T5). Tối thiểu `1m`; `test/soak` đặt `2m` để nhìn thấy vòng prune |
| `TEST_DATABASE_URL` | *(trống)* | chỉ cho test; mỗi test tạo schema riêng `t_xxxx`, chạy migration, xóa khi xong (`pg/pgtest`) |

## Điều kiện QĐ-11 — đọc trước khi viết code

1. **Ngày nào không gửi outreach thì ngày đó không code.** Cột `Code?` trong `phase0/07-outreach-messages.md`. Tuần 0 (12→14-09): ≥ 8 tên vào tracker thay cho "đã gửi". 11-09 là ngoại lệ duy nhất, ghi ở QĐ-11.
2. **Gate-0 (26-10) giữ nguyên.** Code ở đây không phải lý lẽ tại Gate-0. KILL → thư mục này là bài học Go.
3. Không cắt: identity + resolver, artifact cap phía server, tenant isolation.
4. `test/arch/` phải xanh trước mỗi commit; luật mới thêm vào đó trước, module thêm sau.
5. Không demo cho người phỏng vấn.

## Module P1-min (§6.9.11) — cái gì đã có

| Module | Trạng thái | Ghi chú |
|---|---|---|
| `platform/config` | ✅ | env → struct, validate, fail fast. Không in URL có mật khẩu |
| `platform/log` | ✅ | `log/slog`; text ở dev, JSON còn lại. Import với alias `platlog` |
| `platform/httpx` | ✅ | request id (`X-Request-Id`), access log, `/healthz`, metric theo **route pattern** (không theo path — tránh nổ cardinality), `WriteError` envelope §7.1, `RateLimit` token bucket theo key (§7.4; in-process — phải dời sang Postgres/Redis khi có >1 replica, ghi trong code). Origin/CSRF guard (N18) đi cùng login |
| `platform/metric` | ✅ | Counter / Gauge / Histogram có label, text exposition 0.0.4, stdlib thuần. Module nhận `*metric.Registry` tường minh, không có registry toàn cục |
| `platform/pg` | ✅ | pool (pgx v5), `WithTx` truyền tx qua ctx (§6.9.6, gọi lồng thì nhập vào tx ngoài), `Q(ctx)`, `Migrate` (advisory lock, mỗi file một tx, từ chối drift), `IsUniqueViolation`… |
| `platform/pg/pgtest` | ✅ | Postgres thật cho test, schema riêng mỗi test, skip khi thiếu `TEST_DATABASE_URL` |
| `platform/objstore` | ✅ | interface 9 verb; `s3/` = SigV4 tự viết trên `net/http` (không SDK), **khớp 4 ví dụ trong tài liệu AWS** (presign GET, GET+Range, PUT+storage-class, lifecycle query); `memory/` = bucket giả bằng `httptest` phục vụ đúng URL presigned (pin content-type, hết hạn) để test end-to-end. Chọn bằng `OBJSTORE=s3|memory` |
| `platform/queue` | ✅ | bảng `jobs`, nhiều `kind`; claim bằng `FOR UPDATE SKIP LOCKED`; lease → worker chết giữa chừng thì job được claim lại (§14.3); backoff mũ, `dead` sau `max_attempts`; dedupe theo `(kind, key)` khi còn sống; handler chạy **trong cùng transaction** với `Complete` → ghi + hoàn thành commit chung, panic = một lần thử hỏng. Metric `queue_jobs_total`, `queue_pending`, `queue_oldest_pending_seconds` |
| `platform/outbox` | gộp vào `queue` | `queue.Add` tham gia transaction của caller: job commit cùng thay đổi nghiệp vụ hoặc không gì cả — đúng bảo đảm outbox §6.9.6 mà không cần bảng thứ hai |
| `platform/idem` | không cần | mọi endpoint ghi của P1 có khóa tự nhiên: run `(project, localRunId)`, event `(run, sequence)`, artifact `(run, clientTestId, occurrence, attempt, name)`. Tạo khi có endpoint không có khóa tự nhiên |
| `tenancy` | ✅ | Org → Workspace → Project → Environment (§8.2). Hợp đồng ở `tenancy.go`; `EnsureEnvironment` idempotent dưới tải đồng thời (ingest gọi mỗi `POST /v1/runs`); `ProjectScope` cho authz walk-up (§8.8). Chưa có HTTP handler |
| `authz` | ✅ | **login §11.2 P1-min**: email + password argon2id (PHC, m=19 MiB t=2 p=1), email phải verified, session cookie `httpOnly+Secure+SameSite=Lax` lưu HMAC, đổi password → revoke session khác, rate limit **trong bảng** `login_failures` theo IP **và** account (5/15 phút → backoff nhân đôi, sống qua restart), một thông báo 401 cho mọi nguyên nhân + hash giả cho email lạ (chống enumeration cả bằng timing), CSRF hai lớp (Origin/Referer allowlist + double-submit `X-CSRF-Token`). Token `atp_ing_` + 64 hex, lưu prefix 8 + HMAC-SHA-256(pepper, secret) (§11.4); `Authenticate` (401 một kiểu cho mọi lỗi, `last_used_at` throttle 5 phút); `Authorize(principal, resource, action)` **một hàm duy nhất** (§11.3.3) — walk-up cây bằng một query, ma trận §11.3.2 gập về 3 role ở `rules.go`; `ErrNoAccess` → 404, `ErrForbidden` → 403. `Middleware` cho bearer. Users chưa có password/session (login là use case sau) |
| `ingest` | ✅ | ba endpoint §7.2 + `jobs.go`: `/finish` **enqueue** `run.finished` trong cùng tx; worker chạy tier 2 (`ResolveRun`); `Sweep` đóng run mở > 6 h thành `error RUN_TIMED_OUT` (§7.4) rồi vẫn resolve identity cho nó; metric `runs_autoclosed_total` (`POST /v1/runs`, `/events`, `/finish`) sau bearer + rate limit; token phải scope project; payload §7.3 validate tại biên, lỗi trả `details.field` + `index`; cap 500 event / 5 MB → 413; `X-Protocol-Version` 1.x, khác → 426; cross-tenant → 404; `identityMap` qua interface `Resolver` phía consumer (nil tới khi có `identity`); metric §6.4 theo project. Không có bảng |
| `identity` | ✅ | **hai tầng** (§6.9.10): `ResolveInline` chỉ exact alias, không tạo gì; `ResolveDeferred` tại finish chạy đủ §9.3 (ứng viên bằng SQL + `pg_trgm` với `SET LOCAL similarity_threshold = 0.4`, chấm điểm và ngưỡng ở Go trong `internal/match`). Port từ prototype **kèm test khóa**: N22 separator, N34 presence (recall 0,992 → sập khi tắt), N39/N40 tầm nhìn 50 % → recall 0,300 nhưng precision 1,0, N49 trigram so với `similarity()` thật (206/206 cặp, lệch 0), N43 `SplitFrom` để 29/29 ngày sai vs `UndoMerge` 0/29, race §9.7 (8 goroutine → 1 identity). Review Queue có bảng, chưa có UI (P1-min = CSV qua `cmd/admin`). Merge/undo/split đều ghi `identity_audit` |
| `results` | ✅ | `runs` / `test_results` / `test_result_attempts` + `run_events` (dedupe theo `(run, sequence)`). `CreateRun` idempotent theo `(project, localRunId)`; `AppendAttempts` khóa run `FOR UPDATE`, attempt đến lộn xứ vẫn đúng, `finalStatus` = bảng N41 (`FinalStatusOf`, có test từng dòng), `failure_observed_at` ghi một lần; `FinishRun` suy ra status + `flakyCount` (QĐ-7 B), gọi lần hai không đổi; event sau finish → `ErrRunFinished`. `stable_test_id` NULL chờ `identity` |
| `artifact` | ✅ | §7.7 trọn vẹn: declare → presigned PUT (< 8 MB) hoặc multipart 8 MB/part có **resume** (re-declare trả part đã nhận) → complete: **HEAD** đối chiếu size + content-type, **GET stream qua SHA-256**, lệch → 422 `SIZE_MISMATCH`/`CHECKSUM_MISMATCH` + xóa object; cap §10.4 → 413, **run vẫn hợp lệ**; retention theo plan tính lúc declare; link về attempt khi event đến (§7.6 #3) qua sweep; sweep retention + abort multipart bỏ dở 24 h; store hỏng → 503 và artifact vẫn retry được (§14.3). Key do server sinh |
| `flaky` | ✅ | `flaky_signals` theo (identity, environment); công thức §8.6 nguyên văn ở `Compute` (test từng ca: N05 chặn chia 0, skipped không phải bằng chứng, `flaky_pass` là pass trong chuỗi flip, fail cũ phai theo cửa sổ 30 ngày); tính lại **incremental thật** (`0010`): finish chỉ fold kết quả của run đó vào trạng thái đã lưu (`last_outcome`), không đọc lại lịch sử — trước 17-09 mỗi finish đọc cả cửa sổ 30 ngày của mọi identity, 1M dòng/finish ở 1 000 run × 1 000 test (`test/load`); `Sweep` mỗi phút rebuild chính xác các dòng `rebuilt_at` > 24 h để kết quả rời cửa sổ rớt khỏi đếm; `TestIncrementalFoldMatchesExactRebuild` giữ hai đường bằng nhau; `quarantine_suggested` khi ≥ 0,8 và ≥ 10 run `[ASSUMPTION]` |
| `triage` | ✅ | `triage_events` append-only (nguồn sự thật TTRC §8.3) + `ai_hypotheses` để trống cho P1.5; `FAILURE_SEEN` dedupe user+result/24 h; `HYPOTHESIS_ACCEPTED` kèm category §8.7 → ghi `failure_category` qua hợp đồng `results`; P1-min ghi bằng `cmd/admin triage record` |
| `query` | ✅ | read model dashboard v0: run list có đếm, kết quả một run (kèm verdict flaky, số artifact, đã triage), lịch sử một test, trend theo category §8.7, **metric §8.3** (TTRC median, `untriaged_rate`, `triage_coverage`, time-to-first-view), top flaky có tên. **Chỉ đọc — `test/arch` quét mọi string literal trong `internal/query`, gặp INSERT/UPDATE/DELETE là fail** (đã thử cắm một `UPDATE` giả) |
| `migrations/` | ✅ | `0001_tenancy.sql` … `0012_authz_audit.sql` (0004 cài `pg_trgm` vào `public`; 0011 index cho prune/backlog của queue; 0012 `authz_audit`). Tên file `NNNN_name.sql`, liên tục từ 0001, header ghi module sở hữu (§6.9.5) |
| `cmd/api` | ✅ | lifecycle, ping DB lúc boot, `/healthz`, `/metrics` listener riêng; mount ingest + artifact + `authz.AuthRoutes` + **`dashboard.go`** (HTTP đọc trên `query` + triage write, mỗi handler đi qua session → `ProjectScope` → `Authorize`; ở `cmd/` vì `query` không được import `authz`/`triage`). Route mount lên `httpx.Router` (ghi lại mọi pattern) để `tenant_walk_test.go` đi qua **mọi** endpoint với principal lạ — §14.3, xem `GATES.md`. `health_metrics.go`: gauge đọc DB lúc scrape (backlog/oldest/dead job, run kẹt quá 6 h, review queue) — thứ worker chết không tự báo được. Mọi 5xx đi qua `httpx.Cause` → access log một dòng ERROR có `request_id` + `cause` |
| `cmd/migrate` | ✅ | áp migration rồi thoát; mã 0 xong, 1 migration lỗi, 2 config/kết nối |
| `cmd/admin` | ✅ | script vận hành P1-min: `org/ws/project create`, `user create [-password -verified]`, `user set-password/verify`, `role grant/revoke`, `token issue/revoke/list` (revoke cần `-actor`, ghi `authz_audit` §11.7), `audit list`, `identity show/merge/undo-merge/split`, `review list [-csv]/decide`, `flaky list`, `triage record/list`, `dash runs/run/history/triage/trend`. Cách duy nhất có ingest token — không có UI để demo (QĐ-11 #5) |

`GATES.md` — bằng chứng gate kỹ thuật T0–T5 (§14): cái gì đã chạy thật, ngày, lệnh tái lập. T0 ✅, T1 ✅ (`test/failure/`: binary thật bị kill/đứt DB, invariant kiểm bằng SQL), T4 tenant isolation ✅ (16-09); T2 đo bằng `test/load/` (`LOAD_TEST=1`, thêm `LOAD_SCALE=10` cho 1M result) — dashboard đạt, ingest 100 đồng thời chưa đạt trên laptop; T3 ✅ (`test/observe/` + `deploy/prometheus/alerts.yml`: mỗi kịch bản hỏng phải hiện ở `/metrics`, log không lộ bí mật); T5: `test/soak/` (`SOAK_TEST=1`, `SOAK_DURATION=24h` trên host riêng với S3; 2 giờ trên laptop 18-09 sạch: 5 400 run, 0 lỗi, mọi trend phẳng — hôm trước nó tìm ra bảng `jobs` không bao giờ được dọn và gauge backlog quét cả bảng). Security §11.6: bảng bằng chứng trong `GATES.md` §T4; `deploy/routes.txt` là inventory route (test so với mux thật); `govulncheck` + `npm audit` chạy 17-09. Backup/restore §11.5: `deploy/backup/backup.sh` (pg_dump hằng ngày, tự kiểm `--list`) + `restore.sh` (vào DB **mới**), drill thật `test/restore/` (dump → DB mới → checksum mọi bảng bằng nhau, api trả đúng byte, worker tiếp job pending, RTO đo được). `test/harness/` là phần dùng chung: build binary thật, Postgres thật, kill/đo.
| `cmd/worker` | ✅ | cùng code, process thứ hai (§6.9.8 bước 2): pool `run.finished` ×4 (tier 2 → flaky, cùng tx, ghép ở `cmd/` theo §6.9.3 luật 3) + sweep mỗi phút (auto-close run, link artifact, retention, abort multipart bỏ dở); `/metrics` + `/healthz` riêng |
| `test/arch` | ✅ | 6 test (thêm `TestQueryNeverWrites`): tên package cấm, luật tầng, fan-out ≤ 3, chu trình, graph không rỗng. Bỏ qua `_test.go` (external test package không phải self-import). **Đã chứng minh bắt được vi phạm giả** — kiểm lại 13-09 sau khi thêm `tenancy` |

**Cột P1-min của §6.9.11 đã đủ module** (trừ `billing`, cố ý trống), login + HTTP dashboard đã có, **reporter Playwright** ở [`reporter/playwright/`](../reporter/playwright/) đã đẩy một run thật end-to-end (16-09). Còn lại để có T0: Flutter client trên API dưới đây. Rồi T1–T5 theo §14.3. Login email+password (§11.2) là use case riêng, mở rộng bảng `users` khi làm. Tenant isolation (T4) phải xong **trước** partner đầu tiên, không xếp sau.

## Quy ước đã chốt trong code

- **Transaction:** handler/use case gọi `db.WithTx(ctx, fn)`; store gọi `db.Q(ctx)` mỗi câu. Store không tự `Begin`.
- **Lỗi của module:** sentinel `ErrNotFound` / `ErrConflict` / `ErrInvalid` ở file hợp đồng, bọc bằng `%w`; driver error không lọt ra ngoài module.
- **ID:** kiểu riêng cho từng cấp (`tenancy.ProjectID` ≠ `tenancy.WorkspaceID`), giá trị là uuid dạng text. ID sai dạng → `ErrNotFound`, không phải lỗi driver.
- **Validate hai lớp:** regex trong Go (từ chối trước round trip) và `CHECK` trong SQL (giữ bất biến khi ai đó ghi thẳng DB) — cùng một biểu thức.

## Không có ở đây, cố ý

- `internal/execution/` (P3), `internal/testmgmt/` (P2) — §6.9.2 đặt chỗ, chưa tạo.
- `billing` — cột P1-min của §6.9.11 để trống.
- Package `common` / `models` / `utils` — `test/arch` từ chối ngay khi xuất hiện.
- Thư viện metric bên thứ ba — §6.4 cần ba loại metric và một endpoint text; tự viết ~300 dòng rẻ hơn kéo cây phụ thuộc và registry toàn cục.

## Postgres cho test — cách nhanh nhất trên máy đã cài PostgreSQL

Không cần biết mật khẩu của instance đang chạy. Dựng một cluster rác, trust auth, chỉ nghe loopback:

```bash
PG="/c/Program Files/PostgreSQL/17/bin"
"$PG/initdb.exe" -D /tmp/atp-pgdata -U postgres --auth=trust -E UTF8 --no-locale
"$PG/pg_ctl.exe" -D /tmp/atp-pgdata -o "-p 5499 -c listen_addresses=127.0.0.1" -l /tmp/atp-pg.log start
"$PG/psql.exe" -h 127.0.0.1 -p 5499 -U postgres -c "CREATE DATABASE atp_test"

export TEST_DATABASE_URL="postgres://postgres@127.0.0.1:5499/atp_test?sslmode=disable"
go test -count=1 ./...
"$PG/pg_ctl.exe" -D /tmp/atp-pgdata stop      # khi xong
```

Đã chạy thật 13–14-09 trên PostgreSQL 17.5: toàn bộ test PASS (11 package); `cmd/migrate` áp 0001–0004; `cmd/admin` tạo org → ws → project → token; `cmd/api` bằng curl: run 1 `identityMap {}` → finish tạo identity; run 2 tier 1 trả đủ id; run 3 đổi tên leaf → finish auto-match về identity cũ, alias `exact,auto_match`, `stable_test_id` của cả ba run trỏ cùng một id.

## Giao thức ingest như đã cài (§7.2–7.4) — cho người viết reporter

```
POST /v1/runs                       Authorization: Bearer atp_ing_…   X-Protocol-Version: 1.1
  {localRunId, environmentKey, trigger{type,provider,ref,commitSha,ciRunUrl}, framework{name,version},
   reporter{name,version,protocolVersion}, startedAt}
  201 {runId, limits{maxEventsPerBatch, maxBatchBytes, artifactBytes{kind→bytes}}, identityMap{}}
  200 cùng body nếu localRunId đã có

POST /v1/runs/{localRunId}/events   ≤ 500 event, ≤ 5 MB
  {events:[{sequence (≥ 1, duy nhất trong run — khóa idempotency cùng localRunId), type:"test.finished", clientTestId, occurrenceIndex, attempt,
            identity{framework,filePath,titlePath[],paramSignature},
            status: passed|failed|skipped|timed_out|interrupted,
            outcome: expected|unexpected|flaky|skipped,        ← verdict của framework, N36/N41
            durationMs, error{message,stack,snippet}, workerId, startedAt, finishedAt}]}
  200 {accepted, duplicates, identityMap{clientTestId→stableTestId}}
  400 INVALID_PAYLOAD {details.index, details.field} · 413 BATCH_TOO_LARGE · 409 RUN_FINISHED

POST /v1/runs/{localRunId}/finish   body tùy chọn {finishedAt}
  200 {runId, status: passed|failed, flakyCount, finishedAt}  — gọi lần hai trả y nguyên

Mọi lỗi: {"error":{"code","message","details"}}. Sai token → 401. Run của project khác → 404 RUN_NOT_FOUND.
Rate limit 60 req/phút, burst 120, theo token → 429 + Retry-After.
```

## Postgres cho identity — hai điều kiện triển khai

1. **Locale UTF-8 cho database** (`en_US.UTF-8`, `en-US` trên Windows, hoặc ICU). `pg_trgm` phân loại ký tự theo `LC_CTYPE` của DB: dưới `C`/`POSIX` nó làm việc trên **byte**, mọi chữ có dấu thành dấu cắt từ và title tiếng Việt vỡ vụn — Go không thể khớp được. Test `TestTrigramParityWithPostgres` in cảnh báo và chỉ so cặp ASCII khi gặp locale C; trên `en-US` so đủ 206/206 cặp, lệch 0. Tạo DB test đúng: `CREATE DATABASE atp_test TEMPLATE template0 ENCODING 'UTF8' LC_COLLATE 'en-US' LC_CTYPE 'en-US'`.
2. **Role chạy migrate cần `CREATE` trên database** để cài `pg_trgm` (trusted extension từ PG 13, không cần superuser).

Phát hiện khi port: prototype cho `TrigramSimilarity("", "") = 1`, Postgres trả 0 — đã sửa theo Postgres (chuỗi rỗng không bao giờ là leaf title, nên §9.6 không đổi).

## Giao thức artifact như đã cài (§7.7)

```
POST /v1/artifacts                          Authorization: Bearer atp_ing_…
  {localRunId, clientTestId, occurrenceIndex, attempt, name, kind, contentType, sizeBytes, sha256}
  201 {artifactId, status:"pending", expiresAt, limits{kind→bytes},
       upload:{method:"PUT", url, headers{Content-Type}, expiresAt}          ← sizeBytes < 8 MB
     | multipart:{uploadId, partSize, maxParallel, parts:[{number,url}], received:[…], expiresAt}}
  200 cùng artifact nếu đã declare (resume: parts chỉ còn phần thiếu) · 200 status:"verified" không kèm upload nếu đã xong
  413 ARTIFACT_TOO_LARGE {details.limit} — run không bị ảnh hưởng · 404 RUN_NOT_FOUND · 410 ARTIFACT_GONE · 503 STORE_UNAVAILABLE

PUT <url>   Content-Type đúng như headers (đã ký) — sai → bucket trả 403. Part: không cần header.

POST /v1/artifacts/{artifactId}/complete    body tùy chọn {sha256, parts:[{partNumber, etag}]}
  200 {artifactId, status:"verified", sizeBytes, linked, expiresAt}
  409 UPLOAD_INCOMPLETE · 422 SIZE_MISMATCH | CHECKSUM_MISMATCH | CONTENT_TYPE_MISMATCH (object đã xóa; declare lại được) · 503 STORE_UNAVAILABLE (retry)
```

Bucket S3/R2 nên có lifecycle rule tự dọn multipart bỏ dở sau 1 ngày (§7.7); worker cũng abort sau 24 h phía mình.

## Dashboard API (cookie session) — hợp đồng duy nhất: `frontend/API_CONTRACT.md`

```
GET  /v1/session                      bootstrap (ẩn danh vẫn 200): {user: null | {id,name,email}, csrfToken} + cookie atp_csrf
POST /v1/session  {email, password}   Origin trong DASHBOARD_ORIGINS + X-CSRF-Token = cookie → cookie session, csrfToken xoay
POST /v1/session/logout {}            → 204

GET  /v1/projects?limit&cursor                                   {items:[{id,name,workspaceName}], nextCursor}
GET  /v1/projects/{p}/runs?limit&cursor&q&status&environment&sort=-startedAt   Run DTO: id, branch, commitSha, status, environmentName, startedAt, durationMs|null, totalCount, passedCount, failedCount, flakyCount
GET  /v1/projects/{p}/runs/{run}
GET  /v1/projects/{p}/runs/{run}/results?limit&cursor            {id, displayName, filePath, finalStatus, attemptCount, durationMs, stableTestId|null}
GET  /v1/projects/{p}/results/{result}                           {result, runId, attempts:[{attempt,status,durationMs,error{message,stack}|null}], events:[{id,type,actorName,occurredAt}]}
POST /v1/projects/{p}/results/{result}/triage-events {type,payload}   FAILURE_SEEN (viewer) · MARKED_FLAKY | MARKED_KNOWN_ISSUE | RESOLVED (member+)
GET  /v1/projects/{p}/identities?view=flaky&limit&cursor         {id, canonicalTitle, canonicalFilePath, verdict, flakyScore|null, totalRuns, recentStatuses[]}
GET  /v1/projects/{p}/identities/{id}
GET  /v1/projects/{p}/identities/{id}/runs?limit&cursor          Run DTO page

Mọi list: {items, nextCursor}; cursor opaque, keyset (startedAt DESC, id DESC), gắn với project + filter + sort → dùng sai → 400 INVALID_CURSOR.
Không có membership → 404 PROJECT_NOT_FOUND; role thấp → 403 FORBIDDEN; POST thiếu CSRF/Origin lạ → 403 CSRF.
```

`cmd/api/api_test.go` chạy đúng handler của binary qua toàn bộ hợp đồng trên Postgres thật (kể cả cross-tenant, viewer 403, CSRF). 16-09 đã chạy **live**: Vue app thật (Vite dev, không mock) → backend thật: sign-in, projects, runs, run detail, result detail + nút triage, identities, flaky view — 18 call `/v1` đều 200/201 (trừ 401 cố ý khi sai mật khẩu).

**Go toolchain:** `go.mod` ghim `go 1.25.5`. Chạy `GOTOOLCHAIN=local` khi `go get` để không bị tự kéo toolchain mới (x/crypto mới nhất đòi Go 1.26; đang ghim `v0.45.0`).
