# deploy/ — hình dạng triển khai (SPEC §6.1, §11.5)

Một image, mọi binary (`Dockerfile`); một `compose.yml` dựng đủ staging:
Postgres 17.5 (locale UTF-8 — điều kiện của identity), `migrate` chạy một lần
rồi thoát, `api`, `worker`, Prometheus với rule §6.4 (`prometheus/alerts.yml`),
Caddy là listener TLS duy nhất (phục vụ `frontend/dist`, proxy `/v1`), và
container `backup` chạy `backup/backup.sh` mỗi ngày. Đã dựng và kiểm thật
17-09-2026 trên Docker Desktop (xem cuối file).

## Dựng staging

```bash
cd backend
cp deploy/.env.example deploy/.env        # điền POSTGRES_PASSWORD, TOKEN_PEPPER, DOMAIN, DASHBOARD_ORIGINS, OBJSTORE_*
(cd ../frontend && npm ci && npm run build)   # Caddy mount ../frontend/dist
docker compose -f deploy/compose.yml --env-file deploy/.env up -d --build
docker compose -f deploy/compose.yml --env-file deploy/.env ps      # api, worker, postgres: healthy

# tenant đầu tiên — admin là container chạy một lần
C="docker compose -f deploy/compose.yml --env-file deploy/.env run --rm admin"
$C org create -name Acme -slug acme                 # → ID
$C ws create -org <org> -name Payments              # → ID
$C project create -ws <ws> -name checkout           # → ID
$C user create -email alice@example.com -password '…' -verified
$C role grant -user <user> -level project -scope <project> -role member
$C token issue -project <project> -name github-actions   # Plaintext in một lần
```

Dashboard: `https://$DOMAIN`. Reporter: `ATP_URL=https://$DOMAIN`. Prometheus
chỉ nghe loopback của host (`127.0.0.1:9091`); `/metrics` của api/worker không
bao giờ ra khỏi mạng compose (label theo project là dữ liệu nội bộ).

Deploy lại: `up -d --build` — `migrate` chạy lại (idempotent), `api`/`worker`
chờ nó xong (`service_completed_successfully`) rồi mới lên.

## Những gì §11.5 đòi và ở đâu

| §11.5 | Ở đâu |
|---|---|
| TLS mọi nơi + HSTS | Caddy tự lấy cert cho `DOMAIN` (Let's Encrypt; `localhost` → CA nội bộ), http→https; HSTS do api phát (`httpx.BrowserPolicy`, khi `COOKIE_SECURE=true`) |
| CORS chỉ origin dashboard | `DASHBOARD_ORIGINS` — config từ chối `*` |
| Không leak stack/SQL | `httpx.Cause` → log một dòng ERROR, client nhận thông điệp chung |
| Secrets qua env, có `.env.example` | `deploy/.env.example`; `deploy/.env` bị `.gitignore` |
| Log có correlation id, không token | access log `request_id`; T3 kiểm log không lộ bí mật |
| Container non-root, image pin, healthcheck | distroless `nonroot` (uid 65532), mọi image pin tag, `HEALTHCHECK` bằng `cmd/healthcheck` (distroless không có curl); `read_only`, `cap_drop ALL`, `no-new-privileges` |
| Backup hằng ngày + restore thử | container `backup` (volume `backups`), `test/restore/` + `backup/restore.sh` |

**Prod khác staging ở đâu:** `APP_ENV=prod` từ chối `sslmode=disable` — compose
này nói plaintext với Postgres trong mạng riêng nên là `staging`. Prod cần
Postgres có TLS (managed, hoặc server bật `ssl=on`) và `DATABASE_URL` với
`sslmode=verify-full`; còn lại giữ nguyên. Volume `backups` phải được copy ra
khỏi host (object storage có versioning) — backup cùng đĩa với DB không phải
backup. WAF/bot management: §11.7, chưa cần ở quy mô design partner.

## Chạy gate T5 / T2 trên host

`test/soak` và `test/load` tự build binary và cần Postgres — chúng **không**
chạy trong compose mà chạy cạnh nó, trỏ vào Postgres của compose (mở tạm
`ports: ["127.0.0.1:5432:5432"]` cho service `postgres`, hoặc chạy trên cùng
host qua `docker compose exec`). Ví dụ soak 24 h với S3 thật:

```bash
export TEST_DATABASE_URL="postgres://atp:$POSTGRES_PASSWORD@127.0.0.1:5432/atp?sslmode=disable"
SOAK_TEST=1 SOAK_DURATION=24h SOAK_SAMPLE=1m SOAK_JOB_RETENTION=1h \
SOAK_OBJSTORE=s3 OBJSTORE_ENDPOINT=… OBJSTORE_BUCKET=… OBJSTORE_ACCESS_KEY=… OBJSTORE_SECRET_KEY=… \
go test -count=1 -v -timeout 80h ./test/soak/ | tee soak-24h.log
```

(`SOAK_OBJSTORE` đưa `OBJSTORE=s3` và các `OBJSTORE_*` vào binary mà harness
khởi động; không đặt thì harness dùng `memory` như trên laptop.) Máy phát tải
nên là máy khác với Postgres — số của T2 đo trên laptop là số chỉ dấu
(`GATES.md` §T2).

## Đã kiểm 17-09-2026 (Docker Desktop 29.0.1, Windows)

`docker build`: image 19,8 MB, `USER nonroot:nonroot`, healthcheck `/app/healthcheck`.
Config validation chạy trong container (`OBJSTORE=memory` với `APP_ENV=staging`
→ thoát mã 2 với thông điệp đúng). `compose up` với `APP_ENV=dev`/`memory`
(không có S3 trên laptop): `migrate` áp 12 migration rồi thoát 0; `api`,
`worker`, `postgres` **healthy**; `backup` chạy dump đầu tiên ngay khi lên
(12 KB, có `schema_migrations`); DB tạo với `en_US.UTF-8`. Qua Caddy
`https://localhost`: `/` → SPA 200, `/healthz` → ok, `/v1/session` → 200 với
`Strict-Transport-Security`, `X-Content-Type-Options: nosniff`, cookie
`Secure`. Prometheus: 3 target `up`, rule §6.4 + `atp-soak` nạp đủ. Tenant tạo
qua `run --rm admin`; ingest một run qua Caddy → 201/200/200; worker resolve
xong (`stable_test_id` có, job `done`); `sum(ingest_requests_total)` thấy
được từ Prometheus. `down -v` sạch.
