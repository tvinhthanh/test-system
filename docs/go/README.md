# Học Go qua Automation Testing Platform

Bộ hướng dẫn tiếng Việt dành cho người mới học Go, đặc biệt nếu đã biết JavaScript/TypeScript hoặc NestJS. Có hai track đi cùng nhau: **lý thuyết để tra cứu** và **thực hành ngay trên codebase sản phẩm**. Mục tiêu là năng lực làm việc có kiểm chứng; đọc xong không tự đồng nghĩa đã có kinh nghiệm production.

## Bắt đầu ở đâu?

| Bạn muốn | Đọc |
|---|---|
| Cài Go, chạy thử project, xử lý lỗi môi trường | [01 — Cài đặt và sử dụng](01-setup-and-usage.md) |
| Hiểu cú pháp Go bằng code trong repo | [02 — Go căn bản qua project](02-go-through-project.md) |
| Biết file nào làm gì, theo dấu một lần resolve | [03 — Đọc prototype identity](03-identity-walkthrough.md) |
| Hiểu các module backend và quy ước phát triển | [04 — Kiến trúc và workflow](04-architecture-and-workflow.md) |
| Học theo từng buổi, có bài tập và tiêu chí hoàn thành | [05 — Lộ trình thực hành](05-learning-path.md) |

## Track A — Kiến thức từ nền tảng đến production

| Chương | Nội dung |
|---|---|
| [06 — Nền tảng ngôn ngữ](06-language-foundations.md) | Kiểu, collection, ownership, interface, nil, error, defer, generics |
| [07 — Concurrency và lifecycle](07-concurrency-and-lifecycle.md) | Mutex/channel, worker, backpressure, context, shutdown |
| [08 — HTTP và contracts](08-http-and-contracts.md) | Handler/service, validation, errors, pagination, timeouts |
| [09 — Persistence và consistency](09-persistence-and-consistency.md) | Pool, transaction, idempotency, outbox, migration |
| [10 — Security và background jobs](10-security-and-background-jobs.md) | Auth/RBAC, tenant isolation, artifact, retry/lease |
| [11 — Testing/debugging/performance](11-testing-debugging-performance.md) | Unit/integration/fuzz/race, profiling, GC, load/failure tests |
| [12 — Delivery và operations](12-delivery-and-operations.md) | CI/CD, config, observability, rollout/rollback, backup/restore |

## Track B — Đóng góp trực tiếp vào project

| Chương | Nội dung |
|---|---|
| [13 — Chặng 0–4](13-labs-foundation-to-api.md) | Fingerprint/config → loader/protoident → /v1/runs → results → tenancy/authz |
| [14 — Chặng 5–9 và capstone](14-labs-reliability-and-operations.md) | Ingest/outbox → identity → artifact → tải/shutdown → vận hành |
| [15 — Đánh giá và lời giải](15-assessment-and-reference-solutions.md) | Rubric 0–3, đáp án tình huống, code review, mẫu evidence |
| [16 — Sổ tay học và đi làm](16-study-and-work-handbook.md) | Ghép hai track, cách nhận task, tra cứu, nguồn chính thức |

Chặng 0–1 học ngay trên prototype hiện có. Chặng 2–9 là công việc trên backend sản phẩm theo SPEC, **chưa triển khai hoặc chạy test/deploy**. Không tạo side project/module học hay API demo. Những snippet mới là minh họa chưa chạy; chỉ thêm vào code khi phục vụ yêu cầu thật và đúng module.

Mỗi bài theo cấu trúc: **kiến thức → file/module của project → hành vi cần hiểu/thay đổi → test và evidence**. Backend chưa có thì ghi rõ đường dẫn dự kiến, không yêu cầu dựng phiên bản mẫu để học. Xem chương 04 để ánh xạ tên module theo SPEC §6.9.

## Trạng thái thật của project

Đối chiếu code ngày **08-09-2026**:

- Go module hiện có: [`prototype/identity`](../../prototype/identity/go.mod), khai báo `go 1.25.5`, chưa có dependency bên ngoài trong `go.mod`.
- Có thư viện identity, CLI `cmd/protoident`, unit test, example, benchmark và fixture Playwright JSON.
- Chưa có backend production với HTTP API, database, đăng nhập hoặc các module `ingest`, `artifact`, `auth`, `rbac`, `runs`, `projects`.
- CLI hiện đánh giá corpus tổng hợp. Loader đọc report thật có trong thư viện, nhưng CLI chưa có cờ nhập đường dẫn report.
- Các số đo prototype không thay thế kiểm chứng trên dữ liệu partner hoặc tải production.

[`SPEC.md`](../../SPEC.md) là nguồn quyết định sản phẩm và kiến trúc. [README prototype](../../prototype/identity/README.md) ghi các phát hiện thực nghiệm; [Phase 0](../../phase0/README.md) ghi phạm vi hiện tại. Tài liệu học này không thay đổi gate hay phê duyệt triển khai backend.

## Cách dùng bộ docs

Đọc 01 → 02 → 03 trước khi sửa code. Đọc 04–05 để nhập môn project, sau đó ghép chương 06–12 với chặng 0–9 theo chương 16. Các đường dẫn trong docs là tương đối để dùng được khi chuyển repo sang máy khác.

Các lệnh được ghi để bạn chủ động chạy. Nếu nhờ agent chạy, áp dụng chính sách terminal của project; việc thêm tài liệu không đồng nghĩa đã chạy các lệnh đó.
