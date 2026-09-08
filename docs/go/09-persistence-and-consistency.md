# 09 — Persistence, transaction và tính nhất quán

[Mục lục](README.md) · Tiếp: [Security và jobs](10-security-and-background-jobs.md)

Mục tiêu: giải thích được dữ liệu còn lại khi request lỗi ở từng bước. Chương này dạy cách thiết kế boundary và kiểm chứng; chưa cung cấp schema/migration production hoặc chọn PostgreSQL driver.

## Connection pool không phải một connection

`sql.DB` quản lý pool, có thể dùng đồng thời. Giữ một instance dùng chung với giới hạn phù hợp; không mở pool mỗi request. `Open` không đảm bảo kết nối thật thành công, readiness cần kiểm tra phù hợp. Xem [database/sql](https://pkg.go.dev/database/sql).

Ví dụ suy luận thực hành: 5 replica, mỗi replica tối đa 20 connection nghĩa là ứng dụng có thể chiếm 100 connection, chưa tính worker và quản trị. Tăng pool có thể làm DB quá tải thay vì giảm latency. Quan sát thời gian đợi pool bên cạnh query duration.

Query dùng tham số, không nối input thành SQL. Cột sort do client chọn phải ánh xạ qua allowlist; placeholder thường không thay được identifier. Khi đọc rows, đóng rows và kiểm tra lỗi sau vòng duyệt; trả transaction/connection sớm.

## Transaction bao quanh bất biến

Ví dụ ingest có hai tác động: lưu event và đánh dấu nó cần resolution. Nếu chỉ tác động đầu được commit, event có thể bị bỏ quên. Đặt các ghi phụ thuộc trong cùng transaction, hoặc mô hình trạng thái có recovery chứng minh được.

Với `database/sql`: caller/use case begin transaction, dùng các method trên `tx` cho toàn bộ thao tác, defer rollback để dọn khi lỗi, kiểm tra commit. SPEC §6.9.6 yêu cầu truyền transaction xuống qua context có helper tập trung, module nhận transaction từ caller thay vì tự mở. Đừng gọi `db.Exec` ở giữa rồi nghĩ nó thuộc transaction. Xem [transaction trong Go](https://go.dev/doc/database/execute-transactions).

Một request mất kết nối khi commit có thể có kết quả chưa xác định với caller. Không suy luận “nhận error thì chắc chắn không có dữ liệu”; dùng idempotency để tra và retry an toàn. Isolation level, conflict behavior và driver phải được xác nhận ở môi trường DB thật.

## Idempotency theo một bất biến cụ thể

Phân biệt event ID, batch request key và fingerprint: một test có thể phát nhiều event, nên fingerprint không phải khóa chống trùng mọi event.

Trước tiên dùng khóa trong SPEC §7.2: create run dùng localRunId, event dùng (localRunId, event.sequence), artifact dùng bộ trường định danh đã chốt; luôn áp scope project. Không thay chúng bằng khóa bài tập. Các chi tiết còn thiếu phải được làm rõ trong contract sản phẩm:

1. Phạm vi key: project/run/operation phù hợp, lấy scope từ principal đã kiểm tra.
2. Nội dung được hash theo encoding ổn định để phát hiện cùng key khác payload.
3. Lần đầu thành công lưu kết quả cùng transaction với side effect.
4. Retry cùng key/cùng nội dung trả kết quả cũ; khác nội dung trả conflict.
5. Retention key phải bao phủ retry window; xóa key quá sớm có thể tái tạo side effect.

Đọc-kiểm tra-rồi-insert ở application vẫn race. Uniqueness cần được enforce nơi ghi dữ liệu và loser cần đọc kết quả winner theo transaction semantics. Đừng coi mock repository hoặc mutex một process là bằng chứng chống duplicate giữa replica.

## Concurrent update và state machine

Run finish có thể đến đồng thời với event cuối. Định nghĩa rõ điều kiện đóng run, late event và lần finish lặp theo protocol. Dùng điều kiện chuyển trạng thái/phiên bản hoặc khóa phù hợp, rồi test hai caller thật; không cập nhật trạng thái vô điều kiện.

Một thao tác merge/split cần audit và undo semantics. Undo không đơn giản là đảo một field nếu đã có event mới; phải xác định dữ liệu nào thuộc lịch sử thao tác và chính sách conflict.

## Outbox và at-least-once

Trong cùng transaction, ghi domain change và bản ghi công việc cần publish. Dispatcher claim, publish, rồi đánh dấu đã gửi. Nếu chết sau publish nhưng trước đánh dấu, job có thể được publish lại. Vì vậy outbox giải quyết mất ý định publish, không tự tạo exactly-once processing.

Consumer ghi kết quả idempotently rồi ack. Nếu crash trước ack, delivery lặp không được tạo side effect mới. Lease hết hạn cần reclaim; poison job cần giới hạn retry và đường điều tra/replay. Xem chương 10 cho lifecycle.

## Migration và truy vấn

Production cần version migration, thứ tự rollout và compatibility với binary cũ. Expand-contract: thêm cấu trúc tương thích → deploy code chuyển tiếp → backfill có checkpoint → xác minh → bỏ cấu trúc cũ khi không còn caller. Không đặt destructive migration vào mỗi lần app startup.

Với identity, giữ split filtering ở DB/scoring ở Go theo SPEC. Khi học tối ưu, ghi query thật, cardinality và plan, rồi mới đổi index. Không dùng benchmark scan in-memory để kết luận query đã đạt SLA. Các quyết định schema/index cụ thể cần review riêng theo phiên bản PostgreSQL được chọn.

## Bài tập và đáp án định hướng

Trong backend/test/integration khi ingest được triển khai, cho hai caller gửi cùng (localRunId, event.sequence) trong cùng project tại một barrier. Kiểm tra: một side effect, hai response tương thích; payload khác bị conflict; failure trước commit không để trạng thái nửa vời.

**Đáp án:** enforcement tại DB, transaction gắn key/result/side effect; test integration dùng DB riêng. Thêm crash sau commit trước response, retry phải lấy được kết quả. Fake chỉ giúp kiểm tra orchestration, không chứng minh isolation.

Đạt khi có thể lập bảng “lỗi trước ghi / giữa hai ghi / sau commit / trước ack” và nêu trạng thái cùng cách phục hồi cho mỗi hàng.
