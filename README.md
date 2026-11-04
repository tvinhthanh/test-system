# Automation Testing Platform

Nền tảng tổng hợp và triage kết quả test E2E, reporter-first cho Playwright.

> **"Bộ nhớ dài hạn cho bộ test E2E của bạn"** — Playwright cho biết *lần chạy này* fail thế nào; platform cho biết *test này* đã hành xử ra sao qua 500 lần chạy gần nhất, có flaky không, fail lần này giống lần nào trước đó, và giả thuyết nguyên nhân là gì.

## Trạng thái

| | |
|---|---|
| Spec | **v3.54** (02-11-2026) — 18 ADR đã chốt, 65 phát hiện N đã xử lý; tiến độ build ở [`SPEC.md` §0.5](SPEC.md) |
| Đã build | **P1-min** (ingest, identity, flaky, triage, dashboard, reporter, deploy), **P2 Test Management** (case/suite/requirement/plan/execution, release gate, importer CSV + JUnit, ROI), RBAC 6 role, member ba tầng, MFA, OAuth, mời qua email, tự đăng ký có duyệt, **Start testing**, **Auto test**, URL check, AI của 8 nhà cung cấp |
| Gate kỹ thuật | T0 T1 T3 T4 ✅ · T2 T5 còn phần cần server (soak 24–72 h, ingest 100 đồng thời) · pentest chưa làm — [`backend/GATES.md`](backend/GATES.md) |
| Identity | precision **1,0000** · recall **0,9920** trên corpus tổng hợp (§9.6) |

## Đọc từ đâu

| Bạn muốn | Mở |
|---|---|
| Hiểu toàn bộ quyết định và thiết kế | [`SPEC.md`](SPEC.md) — **nguồn sự thật duy nhất**; vào nhanh qua §0.2 (nhật ký phát hiện), §0.5 (tiến độ), §4.10 (bức tranh tổng) |
| Dùng app (tester, admin, người viết automation) | [`HUONG_DAN_SU_DUNG.md`](HUONG_DAN_SU_DUNG.md) |
| Code và chạy backend | [`backend/README.md`](backend/README.md), bằng chứng gate ở [`backend/GATES.md`](backend/GATES.md) |
| Triển khai | [`server/README.md`](server/README.md) (máy cá nhân làm server), [`backend/deploy/README.md`](backend/deploy/README.md) (compose, image) |
| Hợp đồng API | [`backend/deploy/openapi.yaml`](backend/deploy/openapi.yaml), cách nó được kiểm ở [`docs/openapi-contract-reimplementation.md`](docs/openapi-contract-reimplementation.md) |
| Vẽ hoặc chỉnh giao diện | [`PROJECT.md`](PROJECT.md) + [`DESIGN.md`](DESIGN.md) + [`.stitch/rules.md`](.stitch/rules.md); quy tắc frontend ở [`frontend/FRONTEND_RULES.md`](frontend/FRONTEND_RULES.md) |
| Thuật toán test identity chạy thật | [`prototype/identity/README.md`](prototype/identity/README.md) |
| Đặt sản phẩm cạnh đối thủ, demo cho team QA | [`docs/competitive-comparison.md`](docs/competitive-comparison.md), [`docs/demo-refactor-identity.md`](docs/demo-refactor-identity.md) |
| Vì sao tự đăng ký làm như bây giờ | [`docs/adr-001-self-serve-signup.md`](docs/adr-001-self-serve-signup.md) |

## Chạy thử

```bash
# Backend — cần PostgreSQL 17 cho phần lớn test (backend/README.md)
cd backend
go run ./cmd/api                 # :8080, curl localhost:8080/healthz
go test ./...                    # gồm test kiến trúc §6.9.3

# Prototype test identity — bằng chứng §9.6
cd prototype/identity
go test ./...
go run ./cmd/protoident          # phép đo §9.6
```

Dựng cả stack trên một máy có Docker: `./server/atp.sh init`, rồi `up`, rồi `seed` — chi tiết ở [`server/README.md`](server/README.md). URL check và Auto test cần `URLCHECK=on`; AI cần `LLM_API_KEY` hoặc key do organization đặt trên giao diện.

## Luật của repo này

1. **`SPEC.md` là nguồn sự thật duy nhất.** Tài liệu nào nói khác SPEC → tài liệu đó sai.
2. **Mỗi thay đổi cập nhật tài liệu trong cùng commit:** README, `backend/GATES.md`, `.env.example`, `openapi.yaml`, `frontend/API_CONTRACT.md`, và SPEC §0.5 / §14.1 khi tiến độ đổi.
3. **Chỉ giữ tài liệu đang dùng.** Bản lịch sử (v3.0, v3.1, bản đối ngoại v3.12, `phase0/`, `docs/go/`) đã gỡ 02-11 — lấy lại bằng `git show b369287:<đường dẫn>` (SPEC §0.1).

## Cấu trúc

```text
SPEC.md              nguồn sự thật — quyết định, kiến trúc, schema, thuật toán, gate
HUONG_DAN_SU_DUNG.md hướng dẫn dùng app
backend/             Go monolith theo §6.9; test kiến trúc cưỡng chế §6.9.3; GATES.md = bằng chứng T0–T5
backend/deploy/      image distroless, compose (Caddy TLS, Prometheus, backup), openapi.yaml, alert rules
server/atp.sh        vận hành một máy làm server: init / up / seed / status / backup / ca / update / reset
reporter/playwright/ npm reporter §7.5 — batch, retry, spool, artifact, fingerprint §9.2 khớp Go
checker/             Chromium headless sau proxy chặn SSRF — URL check (QĐ-14) và bước Auto test (QĐ-16)
frontend/            dashboard Vue 3 + Tailwind + shadcn-vue (PLAN.md, API_CONTRACT.md)
prototype/identity/  thuật toán test identity §9, có loader Playwright thật
docs/                ADR-001, so sánh đối thủ, kịch bản demo, ghi chú hợp đồng OpenAPI
.github/workflows/   CI: Go trên Postgres 17 (+ -race cho identity), restore drill, govulncheck, image, npm, e2e compose
```
