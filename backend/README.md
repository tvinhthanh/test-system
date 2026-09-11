# backend — Go monolith, P1-min

> Khởi công **11-09-2026** theo **QĐ-11** (SPEC §1.11): xây P1-min song song Phase 0.
> Bố cục theo SPEC §6.9. Luật phụ thuộc §6.9.3 được `test/arch/` cưỡng chế — không phải code review.

## Chạy

```bash
cd backend
go run ./cmd/api                 # HTTP server, mặc định :8080, APP_ENV=dev
curl -i localhost:8080/healthz   # {"status":"ok"} + X-Request-Id
go test ./...                    # gồm test kiến trúc
```

Biến môi trường: `HTTP_ADDR` (mặc định `:8080`), `APP_ENV` (`dev|staging|prod`; sai giá trị → thoát ngay lúc boot, mã 2).

## Điều kiện QĐ-11 — đọc trước khi viết code

1. **Ngày nào không gửi outreach thì ngày đó không code.** Cột `Code?` trong `phase0/07-outreach-messages.md`. Tuần 0 (12→14-09): ≥ 8 tên vào tracker thay cho "đã gửi". 11-09 là ngoại lệ duy nhất, ghi ở QĐ-11.
2. **Gate-0 (26-10) giữ nguyên.** Code ở đây không phải lý lẽ tại Gate-0. KILL → thư mục này là bài học Go.
3. Không cắt: identity + resolver, artifact cap phía server, tenant isolation.
4. `test/arch/` phải xanh trước mỗi commit; luật mới thêm vào đó trước, module thêm sau.
5. Không demo cho người phỏng vấn.

## Module P1-min (§6.9.11) — cái gì đã có

| Module | Trạng thái | Ghi chú |
|---|---|---|
| `platform/config` | ✅ | env → struct, validate, fail fast |
| `platform/log` | ✅ | `log/slog`; text ở dev, JSON còn lại. Import với alias `platlog` |
| `platform/httpx` | ✅ | request id (`X-Request-Id`), access log, `/healthz`. Auth / rate limit / Origin guard (N18): chưa |
| `platform/metric` | — | bắt buộc từ P1 (§6.4) — kế tiếp |
| `platform/pg` | — | pool, tx helper, `FOR UPDATE SKIP LOCKED` |
| `platform/objstore` | — | S3/R2 presigned + multipart (§7.7) |
| `platform/queue` | — | nhiều logical queue (§6.7.4) |
| `platform/outbox` | — | §6.7.8 |
| `platform/idem` | — | idempotency key (§7.1) |
| `tenancy` | — | Org → Workspace → Project → Environment (§8.2) |
| `authz` | — | token HMAC-SHA-256 (§8.8), 3 role (§4.4) |
| `ingest` | — | §7.2–7.4 hot path |
| `identity` | — | resolver hai tầng (§6.9.10). **Không copy `prototype/identity`** — prototype là bằng chứng §9.6, giữ nguyên; module này viết lại theo hợp đồng §6.9.4 và đọc lại prototype để lấy thuật toán |
| `results` | — | §8.4 |
| `artifact` | — | §7.7 upload, §10.4 cap |
| `flaky` | — | §8.6 |
| `triage` | — | `TriageEvent` thủ công (§8.3) |
| `query` | — | dashboard v0, chỉ đọc |
| `cmd/api` | ✅ | lifecycle + `/healthz`. Handler nghiệp vụ mount vào đây khi có |
| `cmd/worker`, `cmd/migrate` | — | tạo khi có queue / có schema |
| `test/arch` | ✅ | 5 test: tên package cấm, luật tầng, fan-out ≤ 3, chu trình, graph không rỗng. **Đã chứng minh bắt được vi phạm giả** trước khi commit |

Thứ tự gợi ý cho những module tiếp theo — theo §14 (Correctness → Reliability trước): `platform/pg` + `cmd/migrate` → `tenancy` → `authz` → `ingest` + `results` → `identity`. Tenant isolation (T4) phải xong **trước** partner đầu tiên, không xếp sau.

## Không có ở đây, cố ý

- `internal/execution/` (P3), `internal/testmgmt/` (P2) — §6.9.2 đặt chỗ, chưa tạo.
- `billing` — cột P1-min của §6.9.11 để trống.
- Package `common` / `models` / `utils` — `test/arch` từ chối ngay khi xuất hiện.
