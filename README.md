# Automation Testing Platform

Nền tảng tổng hợp và triage kết quả test E2E, reporter-first cho Playwright.

> **"Bộ nhớ dài hạn cho bộ test E2E của bạn"** — Playwright cho biết *lần chạy này* fail thế nào; platform cho biết *test này* đã hành xử ra sao qua 500 lần chạy gần nhất, có flaky không, fail lần này giống lần nào trước đó, và giả thuyết nguyên nhân là gì.

## Trạng thái: Phase 0 (validation), chưa build sản phẩm

| | |
|---|---|
| Spec | **v3.16** — 11 ADR đã chốt, ba plane đã đặc tả, 59 phát hiện N đã xử lý |
| Phase 0 | Khởi động lại **15-09-2026** → Gate-0 **26-10-2026**. Lần chốt 26-08 không diễn ra (0 outreach sau 17 ngày); chốt lại ngày 11-09 theo đường C |
| Code sản phẩm | **Đang xây P1-min ở `backend/`** theo QĐ-11 (11-09) — song song Phase 0, có điều kiện. `cmd/api` chạy, `/healthz`, test kiến trúc §6.9.3 |
| Prototype identity | Đạt — precision **1,0000** · recall **0,9920** trên corpus tổng hợp |

## Đọc từ đâu

| Bạn muốn | Mở |
|---|---|
| Hiểu toàn bộ quyết định và thiết kế | [`SPEC.md`](SPEC.md) — **nguồn sự thật duy nhất** |
| Biết Phase 0 đang ở đâu, phải làm gì | [`phase0/README.md`](phase0/README.md) |
| Xem thuật toán test identity chạy thật | [`prototype/identity/README.md`](prototype/identity/README.md) |
| Tài liệu gửi ra ngoài | [`docs/external/`](docs/external/) — chạy `verify.sh` trước khi gửi |
| Học Go qua chính codebase này | [`docs/go/README.md`](docs/go/README.md) |

`SPEC.md` dài, nhưng §0.2 (nhật ký 59 phát hiện) và §4.10 (bức tranh tổng) là hai chỗ vào nhanh nhất.

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
backend/             Go monolith P1-min theo §6.9 — QĐ-11; test kiến trúc cưỡng chế §6.9.3
reporter/playwright/ npm reporter §7.5 — batch, retry, spool, artifact, fingerprint §9.2 khớp Go
prototype/identity/  Go — thuật toán test identity §9, có loader Playwright thật
docs/external/       bản đối ngoại + pipeline sinh .docx + verify.sh gác checklist §4.7
docs/go/             hướng dẫn học Go qua codebase này
docs/v3.0/           lịch sử, đã lỗi thời — không gửi ra ngoài
docs/v3.1/           lịch sử, nội dung đúng nhưng rời rạc
```
