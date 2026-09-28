# Automation Testing Platform

Nền tảng tổng hợp và triage kết quả test E2E, reporter-first cho Playwright.

> **"Bộ nhớ dài hạn cho bộ test E2E của bạn"** — Playwright cho biết *lần chạy này* fail thế nào; platform cho biết *test này* đã hành xử ra sao qua 500 lần chạy gần nhất, có flaky không, fail lần này giống lần nào trước đó, và giả thuyết nguyên nhân là gì.

## Trạng thái: Phase 0 (validation) song song P1-min (QĐ-11)

| | |
|---|---|
| Spec | **v3.28** — 12 ADR đã chốt (QĐ-9 sửa lần hai 14-09: client Vue 3; giao diện Tailwind + shadcn-vue 21-09; **QĐ-12 21-09: P2 Test Management khởi công**), ba plane đã đặc tả, 64 phát hiện N đã xử lý; tiến độ build ở §0.5 |
| Phase 0 | Khởi động lại **15-09-2026** → Gate-0 **26-10-2026**. Lần chốt 26-08 không diễn ra (0 outreach sau 17 ngày); chốt lại ngày 11-09 theo đường C |
| Code sản phẩm | **P1-min đã đủ** theo QĐ-11 (11-09 → 18-09): `backend/` (Go, mọi module §6.9.11 trừ billing), `reporter/playwright/`, `frontend/` (Vue), `backend/deploy/` (image + compose staging + CI). Gate kỹ thuật: T0 T1 T3 T4 ✅, T2 dashboard ✅, T5 harness 2 h sạch — xem [`backend/GATES.md`](backend/GATES.md); còn cần hạ tầng cho soak 24 h, ingest trên server, pentest. **P2 Test Management** (QĐ-12, từ 21-09): backend `internal/testmgmt` xong — case/suite/requirement/plan/execution, link tới `TestIdentity`, importer CSV, release gate 5 rule + API cho CI; UI đủ 4 màn hình (cases, requirements, plans, releases); **22-09: bằng chứng thủ công** — file đính kèm execution đi qua `Artifact` (`manual_evidence`, migration 0014), upload multipart qua api, thumbnail trên trang plan; **RBAC 6 role §11.3** (migration 0015, ma trận là dữ liệu + test, `actions[]` trên project cho UI); **27-09: quản lý member** — màn hình Members (thêm/đổi/gỡ role cấp project theo email, thấy role kế thừa), 3 route sau `member.manage`, audit mọi thay đổi (migration 0016); **27-09: MFA TOTP** — bắt buộc cho org owner (§11.2), đăng nhập hai bước, trang Security (bật/tắt, recovery code), migration 0017; **27-09: màn hình Team** — quản lý member ở tầng workspace/org, chỉ owner đụng được role owner, tổ chức luôn giữ một owner; **27-09: đăng nhập Google/GitHub** — bật/tắt từng provider ở backend (`OAUTH_GOOGLE`/`OAUTH_GITHUB`, mặc định off), PKCE, chỉ tài khoản đã có và đã verify, MFA vẫn áp dụng; **28-09: mời qua email** — `MAIL=smtp` (mặc định off), thêm email chưa có tài khoản sẽ gửi link mời một lần, hạn 7 ngày, người nhận đặt mật khẩu và vào thẳng; trang Security có "Connected accounts"; **quên mật khẩu** qua email (cùng `MAIL=smtp`) |
| Prototype identity | Đạt — precision **1,0000** · recall **0,9920** trên corpus tổng hợp |

## Đọc từ đâu

| Bạn muốn | Mở |
|---|---|
| Hiểu toàn bộ quyết định và thiết kế | [`SPEC.md`](SPEC.md) — **nguồn sự thật duy nhất** |
| Biết Phase 0 đang ở đâu, phải làm gì | [`phase0/README.md`](phase0/README.md) |
| Xem thuật toán test identity chạy thật | [`prototype/identity/README.md`](prototype/identity/README.md) |
| Tài liệu gửi ra ngoài | [`docs/external/`](docs/external/) — chạy `verify.sh` trước khi gửi |
| Học Go qua chính codebase này | [`docs/go/README.md`](docs/go/README.md) |

`SPEC.md` dài, nhưng §0.2 (nhật ký 63 phát hiện), §0.5 (tiến độ build) và §4.10 (bức tranh tổng) là ba chỗ vào nhanh nhất.

## Chạy thử

```bash
# Prototype test identity — bằng chứng §9.6, giữ nguyên
cd prototype/identity
go test ./...                    # mọi phát hiện đều có test khóa
go run ./cmd/protoident          # phép đo §9.6
go run ./cmd/protoident -visibility   # N39/N40: độ chính xác theo tầm nhìn run

# Backend P1-min — QĐ-11, xây song song Phase 0
cd backend
go run ./cmd/api                 # :8080, curl localhost:8080/healthz
go test ./...                    # gồm test kiến trúc §6.9.3

# Tài liệu đối ngoại — exit code là cổng, chạy trước khi gửi cho ai
cd docs/external
npm install && npm run build
./verify.sh                      # checklist §4.7, hiện 11/11
```

## Bốn luật của repo này

1. **`SPEC.md` là nguồn sự thật duy nhất.** Mọi thứ trong `docs/` là lịch sử. Tài liệu nào nói khác SPEC → tài liệu đó sai.
2. **Code sản phẩm ở `backend/` chỉ được viết trong ngày đã gửi outreach** (QĐ-11, lật §5.7 ngày 11-09). Gate-0 26-10 vẫn là cổng: KILL thì `backend/` là bài học, không phải sản phẩm.
3. **Không điền dữ liệu Phase 0 bằng phỏng đoán.** Ghi chú phỏng vấn, tracker outreach và sổ baseline TTRC phải đến từ người thật — điền hộ là chế ra đúng thứ bằng chứng mà Gate-0 tồn tại để cân.
4. **Ba file trong `docs/v3.0/` bị cấm gửi ra ngoài** cho tới khi regenerate theo §4.7 (§0.4). Chúng vẫn còn pitch agent-first, trái QĐ-1.

## Cấu trúc

```text
SPEC.md              nguồn sự thật — quyết định, kiến trúc, schema, thuật toán, gate
phase0/              bộ công cụ thi hành: outreach, phỏng vấn, baseline TTRC, biên bản Gate-0
backend/             Go monolith P1-min theo §6.9 — QĐ-11; test kiến trúc cưỡng chế §6.9.3; GATES.md = bằng chứng T0–T5
backend/deploy/      image distroless, compose staging (Caddy TLS, Prometheus, backup), openapi.yaml, alert rules
reporter/playwright/ npm reporter §7.5 — batch, retry, spool, artifact, fingerprint §9.2 khớp Go
frontend/            dashboard Vue 3 (P1-min UI đủ; QĐ-9 chọn Flutter, chủ dự án chọn Vue 14-09 — PLAN.md)
.github/workflows/   CI: go test trên Postgres 17 + restore drill, govulncheck, image, lint OpenAPI/alert, npm test/audit, e2e compose
prototype/identity/  Go — thuật toán test identity §9, có loader Playwright thật
docs/external/       bản đối ngoại + pipeline sinh .docx + verify.sh gác checklist §4.7
docs/go/             hướng dẫn học Go qua codebase này
docs/v3.0/           lịch sử, đã lỗi thời — không gửi ra ngoài
docs/v3.1/           lịch sử, nội dung đúng nhưng rời rạc
```
