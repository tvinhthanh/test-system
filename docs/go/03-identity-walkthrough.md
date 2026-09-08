# 03 — Đọc prototype identity

[Mục lục](README.md) · [Tiếp: Kiến trúc](04-architecture-and-workflow.md)

## Bài toán trước khi đọc thuật toán

Test đổi tên hoặc chuyển file có thể đổi fingerprint, nhưng người dùng vẫn muốn giữ lịch sử của cùng một test. `stableTestId` là danh tính lâu dài; fingerprint là tín hiệu dùng để tìm danh tính đó. Match sai hai test khác nhau làm trộn lịch sử, vì vậy không được chỉ tối ưu recall.

## Thứ tự đọc code

| File | Câu hỏi cần trả lời |
|---|---|
| [`example_test.go`](../../prototype/identity/example_test.go) | Caller tạo config, resolver và nhận kết quả thế nào? |
| [`fingerprint.go`](../../prototype/identity/fingerprint.go) | Dữ liệu nào quyết định fingerprint, chuẩn hóa ra sao? |
| [`resolve.go`](../../prototype/identity/resolve.go) | Exact match, candidate và decision liên hệ thế nào? |
| [`config.go`](../../prototype/identity/config.go), [`score.go`](../../prototype/identity/score.go) | Threshold, gap và presence ảnh hưởng quyết định ra sao? |
| [`playwright.go`](../../prototype/identity/playwright.go) | Report được chuyển thành Identity và status thế nào? |
| [`eval.go`](../../prototype/identity/eval.go), [`synth.go`](../../prototype/identity/synth.go) | Harness tạo tình huống và tính kết quả ra sao? |
| [`merge_split.go`](../../prototype/identity/merge_split.go) | Chỉnh lại identity và undo được mô hình hóa thế nào? |

## Theo dấu một lần Resolve

1. Tạo fingerprint từ identity đã chuẩn hóa.
2. Tra `Aliases`. Có key thì trả `exact_match` ngay.
3. Không có key: quét candidates theo file, leaf title hoặc trigram; chọn tối đa 50 theo rough score.
4. Tính điểm chi tiết, sắp xếp và so top-1 với top-2.
5. Áp dụng threshold, `MinGap`, recency gate để trả decision.

| Decision | Ý nghĩa trong kết quả hiện tại |
|---|---|
| `exact_match` | Alias đã xác định stable ID |
| `auto_match` | Đề xuất tự ghép vào stable ID đạt điều kiện |
| `review_queue` | Candidate cần người xem; `MatchedID` là đề xuất, chưa phải liên kết được chấp nhận |
| `new_identity` | Không đủ bằng chứng ghép vào candidate cũ |

Tên decision thể hiện hành động caller cần thực hiện. Hàm `Resolve` hiện chỉ tính kết quả; nó không tạo bản ghi, gửi queue hoặc commit transaction. Candidates/aliases phải được caller giới hạn đúng project; prototype chưa enforce tenant isolation của backend.

## Hai giai đoạn là thiết kế tích hợp

Thiết kế mô tả trong README/SPEC: ingest xử lý exact alias, còn fuzzy resolution đợi `run.finish` với thông tin presence đủ tin cậy. “Chưa nhận event” chưa chứng minh test không xuất hiện; filtered/sharded run cũng cần xem xét phạm vi.

**API prototype `Resolve` vẫn chạy cả exact và fuzzy khi cần.** Không được gọi nó cho mọi event rồi kết luận đã triển khai đúng pipeline hai giai đoạn. Production cần lớp điều phối stage, trạng thái pending và xử lý hoàn tất/retry theo protocol.

## Các bất biến phải giữ

- Không chuẩn hóa giá trị động vào fingerprint: chỉ dùng cho template grouping. Xem [`dynamic_title.go`](../../prototype/identity/dynamic_title.go).
- Giữ separator giữa các phần `TitlePath`; nối chuỗi mơ hồ có thể làm hai test trùng fingerprint.
- Giữ cả presence signal lẫn `MinGap`; chúng bảo vệ các trường hợp khác nhau.
- Không dùng Playwright `spec.id` làm danh tính bền vững khi đổi tên/chuyển file.
- Phân biệt status cấp test với status từng attempt; giữ `timed_out`, `interrupted`, và verdict flaky của Playwright.
- Get-or-create phải atomic. Mutex in-memory là mô hình thử nghiệm; production cần uniqueness theo project và xử lý conflict ở database.

Các test tương ứng nằm trong `fingerprint_test.go`, `presence_test.go`, `visibility_test.go`, `pwid_test.go`, `retries_test.go`, `concurrent_test.go` và `regression_test.go` cùng thư mục prototype.

## Giới hạn khi đọc số đo

Candidate filtering hiện là quét in-memory. Thiết kế production đưa filtering sang PostgreSQL có index, scoring tối đa 50 candidate ở Go. Benchmark local không bao gồm network, DB, transaction, queue, HTTP hay tải nhiều tenant. Dùng số đo để tìm regression trong cùng điều kiện, không coi là SLA của backend chưa xây.
