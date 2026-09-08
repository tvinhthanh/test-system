# Học Go qua Automation Testing Platform

Bộ hướng dẫn tiếng Việt dành cho người mới học Go, đặc biệt nếu đã biết JavaScript/TypeScript hoặc NestJS. Mục tiêu: đọc được code hiện tại, chạy prototype, sửa một thay đổi nhỏ có kiểm chứng và hiểu hướng xây backend.

## Bắt đầu ở đâu?

| Bạn muốn | Đọc |
|---|---|
| Cài Go, chạy thử project, xử lý lỗi môi trường | [01 — Cài đặt và sử dụng](01-setup-and-usage.md) |
| Hiểu cú pháp Go bằng code trong repo | [02 — Go căn bản qua project](02-go-through-project.md) |
| Biết file nào làm gì, theo dấu một lần resolve | [03 — Đọc prototype identity](03-identity-walkthrough.md) |
| Hiểu các module backend và quy ước phát triển | [04 — Kiến trúc và workflow](04-architecture-and-workflow.md) |
| Học theo từng buổi, có bài tập và tiêu chí hoàn thành | [05 — Lộ trình thực hành](05-learning-path.md) |

## Trạng thái thật của project

Đối chiếu code ngày **08-09-2026**:

- Go module hiện có: [`prototype/identity`](../../prototype/identity/go.mod), khai báo `go 1.25.5`, chưa có dependency bên ngoài trong `go.mod`.
- Có thư viện identity, CLI `cmd/protoident`, unit test, example, benchmark và fixture Playwright JSON.
- Chưa có backend production với HTTP API, database, đăng nhập hoặc các module `ingest`, `artifact`, `auth`, `rbac`, `runs`, `projects`.
- CLI hiện đánh giá corpus tổng hợp. Loader đọc report thật có trong thư viện, nhưng CLI chưa có cờ nhập đường dẫn report.
- Các số đo prototype không thay thế kiểm chứng trên dữ liệu partner hoặc tải production.

[`SPEC.md`](../../SPEC.md) là nguồn quyết định sản phẩm và kiến trúc. [README prototype](../../prototype/identity/README.md) ghi các phát hiện thực nghiệm; [Phase 0](../../phase0/README.md) ghi phạm vi hiện tại. Tài liệu học này không thay đổi gate hay phê duyệt triển khai backend.

## Cách dùng bộ docs

Đọc 01 → 02 → 03 trước khi sửa code. Đọc 04 khi muốn hiểu monolith module hóa, rồi dùng 05 để thực hành. Các đường dẫn trong docs là tương đối để dùng được khi chuyển repo sang máy khác.

Các lệnh được ghi để bạn chủ động chạy. Nếu nhờ agent chạy, áp dụng chính sách terminal của project; việc thêm tài liệu không đồng nghĩa đã chạy các lệnh đó.
