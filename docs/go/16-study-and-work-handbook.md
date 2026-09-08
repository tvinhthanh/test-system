# 16 — Cách học, tra cứu và áp dụng khi đi làm

[Mục lục](README.md)

## Học ngay trên project

| Chặng | Đọc | Thực hành | Sản phẩm học tập |
|---|---|---|---|
| Làm quen | 01–03, 06 | Chặng 0–1 | Hiểu protoident/loader, kiểm chứng test identity hiện có |
| Feature backend | 04, 08–09, 11 | Chặng 2–3 | API, persistence, migration/test environment |
| Quyền và reliability | 07, 09–10 | Chặng 4–6 | Tenant isolation, idempotency, worker, identity |
| File và tải | 10–11 | Chặng 7–8 | Artifact lifecycle, load report, shutdown |
| Vận hành | 12 | Chặng 9 và capstone | CI, rollout, alert, restore, postmortem |

Một buổi học: đọc kiến thức → mở file tương ứng trong repo → theo dấu behavior → chạy/đọc test hiện có → bổ sung đúng khoảng trống nếu có → ghi điều đã hiểu. Không tạo module hoặc ứng dụng khác để hoàn thành chặng; phần backend chưa có đi cùng roadmap P1. Môi trường test/staging tách dữ liệu nhưng chạy cùng code sản phẩm. Dùng đáp án sau khi đã thử. Nếu chỉ có thời gian đọc, ghi “đã đọc”, không đánh dấu thực hành hoàn thành.

Chương 05 là tuần nhập môn ban đầu, không phải toàn bộ giáo trình. Không ép lộ trình nâng cao vào 7 ngày; thời gian tùy kinh nghiệm, môi trường và độ sâu evidence.

## Bản đồ kiến thức → code trong project

| Học gì | Đọc/thực hành ở đâu |
|---|---|
| Struct, method, slice, normalization | [fingerprint.go](../../prototype/identity/fingerprint.go), [tests](../../prototype/identity/fingerprint_test.go) |
| Config copy, zero value, thời gian | [config.go](../../prototype/identity/config.go), [gate tests](../../prototype/identity/gate_test.go) |
| JSON, error, traversal | [playwright.go](../../prototype/identity/playwright.go), [retry tests](../../prototype/identity/retries_test.go) |
| Scoring và decision | [resolve.go](../../prototype/identity/resolve.go), [score.go](../../prototype/identity/score.go) |
| Mutex và atomic behavior | [concurrent.go](../../prototype/identity/concurrent.go), [tests](../../prototype/identity/concurrent_test.go) |
| Benchmark | [bench_test.go](../../prototype/identity/bench_test.go) |
| HTTP, quyền, persistence chưa có | SPEC §6.9, §7.2; các đường dẫn đích ở chương 13 |
| Worker, artifact, vận hành chưa có | SPEC §6.9, §7.7, §14; các đường dẫn đích ở chương 14 |

## Khi nhận một task thật

Đọc acceptance criteria và tìm luồng đang có trước. Xác định module sở hữu, rủi ro dữ liệu/quyền và version đang dùng. Với thay đổi nhỏ, chọn giải pháp nhỏ đủ rõ; với thay đổi nhiều boundary, ghi hai phương án và tradeoff trước khi code.

Ưu tiên vertical slice nhỏ review được. PR mô tả vấn đề, behavior trước/sau, kiểm chứng và giới hạn; không kể toàn bộ lịch sử thử nghiệm. Đừng thay cả kiến trúc chỉ để hoàn thành một CRUD. Khi chưa chắc, đưa giả thuyết cùng cách kiểm chứng, không trình bày suy đoán thành kết luận.

Trước bàn giao, người khác phải tìm được cách chạy, config cần thiết, expected output và cách rollback nếu có. Chỉ cung cấp secret qua cơ chế đã thống nhất, không trong README.

## Bảng tra nhanh

| Gặp vấn đề | Đọc lại |
|---|---|
| Nil bất ngờ, slice bị sửa, interface khó hiểu | [06](06-language-foundations.md) |
| Treo, goroutine leak, cancel không dừng | [07](07-concurrency-and-lifecycle.md) |
| Body sai, status code, cursor | [08](08-http-and-contracts.md) |
| Duplicate, transaction, mất job | [09](09-persistence-and-consistency.md) |
| Cross-tenant, signed URL, retry worker | [10](10-security-and-background-jobs.md) |
| Test flaky, latency, memory | [11](11-testing-debugging-performance.md) |
| Deploy/alert/restore | [12](12-delivery-and-operations.md) |

## Tài liệu chính thức để học sâu

- [A Tour of Go](https://go.dev/tour/): bài học ngôn ngữ tương tác.
- [Language specification](https://go.dev/ref/spec): quy tắc chính xác; kiểm tra language version.
- [Effective Go](https://go.dev/doc/effective_go): idiom nền tảng; không thay tutorial modules/generics hiện đại.
- [Generics tutorial](https://go.dev/doc/tutorial/generics): type parameters và constraints.
- [Memory model](https://go.dev/ref/mem): điều kiện đồng bộ dữ liệu giữa goroutine.
- [Context](https://pkg.go.dev/context), [net/http](https://pkg.go.dev/net/http), [database/sql](https://pkg.go.dev/database/sql): API chuẩn; so với toolchain dùng thật.
- [Transactions](https://go.dev/doc/database/execute-transactions): quản lý transaction trong Go.
- [Fuzzing](https://go.dev/doc/tutorial/fuzz), [race detector](https://go.dev/doc/articles/race_detector), [diagnostics](https://go.dev/doc/diagnostics): công cụ kiểm chứng và phân tích.
- [Go security](https://go.dev/doc/security/): tài nguyên bảo mật hệ sinh thái Go.

Tham chiếu web được đối chiếu khi viết ngày 08-09-2026; trang online có thể đi trước Go 1.25.5 của prototype. Ví dụ trong docs dùng khái niệm/API nền tảng, không yêu cầu nâng version. Khi thực hành chọn package mới, ghi lại phiên bản và kiểm chứng trước khi dùng snippet từ tài liệu mới nhất.

## Duy trì bộ docs

Khi triển khai feature của project, bổ sung link code và lệnh chạy thật vào đúng chương, giữ nhãn phần chưa có. Khi module/cờ CLI/SPEC thay đổi, cập nhật mục trạng thái và walkthrough. Chỉ ghi benchmark/test pass kèm lần chạy có evidence; thay đổi docs không tự tạo bằng chứng production.
