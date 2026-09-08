# 08 — HTTP API và hợp đồng dữ liệu

[Mục lục](README.md) · Tiếp: [Persistence](09-persistence-and-consistency.md)

Mục tiêu: một request đi từ transport đến service có validation, quyền và lỗi rõ ràng. Áp dụng vào backend của project khi triển khai; hiện chưa có API server.

## Từ Nest controller sang Go handler

Handler nhận `http.ResponseWriter` và `*http.Request`. Dùng router để chọn handler, middleware cho concerns HTTP dùng chung, service cho use case. Standard library đủ để học lifecycle và `httptest`; chọn framework sau khi hiểu nhu cầu. Tra API ở [net/http](https://pkg.go.dev/net/http), kiểm tra phiên bản trước khi dùng tính năng mới.

Luồng đề xuất: request ID → xác thực → body limit/decode → validation → service authorize resource → transaction → encode response. Thứ tự chi tiết phụ thuộc endpoint: cần tránh đọc body lớn của caller không hợp lệ và tránh để resource tồn tại bị suy ra ngoài quyền.

## Decode không phải validation

Giới hạn tổng bytes bằng `http.MaxBytesReader` trước khi decode; sau đó kiểm tra số event, độ dài trường và nesting theo contract. `Content-Length` không đáng tin làm kiểm soát duy nhất vì có streaming/chunked request.

Decoder phải từ chối body malformed và kiểm tra không có JSON document thứ hai sau document đầu. `DisallowUnknownFields` hữu ích cho contract chặt, nhưng có thể phá forward compatibility của reporter; chọn và test theo version protocol. Field thiếu và field có zero value đôi khi cần pointer/kiểu optional để phân biệt.

Không trả nguyên domain entity làm DTO chỉ vì dễ marshal: dữ liệu nội bộ như storage key và trường quyền có thể lọt ra response.

## Mã lỗi và contract của project

Áp dụng protocol §7.2 và OpenAPI của project. Với ingest, thiếu/sai token là 401, token đúng nhưng sai project là 404. Các mã khác phải được chốt theo contract của endpoint trước khi triển khai:

| Tình huống | HTTP | Điều client nên làm |
|---|---|---|
| JSON/field không hợp lệ | 400 | Sửa payload |
| Thiếu/sai credential | 401 | Cấp credential đúng |
| Không có quyền | 404 cho ingest sai project; endpoint khác theo contract | Không retry mù |
| Resource không tồn tại trong scope được phép | 404 | Kiểm tra ID/scope |
| Idempotency key trùng nhưng nội dung khác | 409 | Điều tra lỗi caller |
| Vượt body cap | 413 | Chia batch theo protocol |
| Quota/rate limit tạm thời | 429 | Backoff theo hướng dẫn server |
| Dependency tạm thời không sẵn sàng | 503 | Retry có giới hạn nếu thao tác an toàn |

Response lỗi phải theo OpenAPI; các thông tin cần cân nhắc gồm mã ổn định, thông điệp an toàn và request ID; log giữ nguyên cause có kiểm soát. Không dùng một 500 chung cho lỗi validation lẫn DB timeout.

## Pagination và request retry

Run history cần order ổn định có tie-breaker, ví dụ thời gian và ID. Cursor phải gắn filter/scope và được validate; cursor không cấp quyền truy cập. Offset dễ học nhưng khi bảng thay đổi hoặc offset sâu cần đánh giá keyset pagination. Không tự chọn index trước khi xác định query và kiểm chứng plan.

POST nhận event cần hợp đồng idempotency riêng. Timeout ở client không chứng minh server chưa commit; client retry có thể gặp transaction đã thành công. Response thành công phải tương ứng đúng mức durability đã hứa.

## Server và client lifecycle

Cấu hình timeout đọc header, idle và các deadline theo loại endpoint; upload dài cần budget khác request nhỏ. Server phải xử lý shutdown có deadline và phân biệt lỗi đóng bình thường với lỗi startup. Client outbound nên tái dùng client/transport và luôn đóng response body; cắt body nếu chỉ cần một thông báo lỗi nhỏ.

Không lặp gọi dependency vô hạn trong handler. Một request bị cancel cần giải phóng connection và dừng công việc không còn cần thiết.

## Bài tập handler bằng httptest

Khi bắt đầu backend, test handler `POST /v1/runs` bằng payload từ SPEC §7.3 và localRunId của reporter. Code thuộc module backend thật theo §6.9, service fake chỉ nằm trong test. Không tạo project CRUD mẫu hoặc route ngoài contract.

Kiểm chứng bằng request/recorder: thành công, JSON sai, trường bắt buộc thiếu, body vượt cap, hai document JSON liên tiếp, service trả lỗi đã phân loại. Fake ghi nhận số lần gọi để chứng minh input sai không chạm service. Dùng test server riêng cho behavior cần network thật; recorder không mô phỏng đầy đủ transport.

**Gợi ý đáp án:** mapping error tập trung tại biên HTTP, validation trước side effect, body limit trước decode; service không import HTTP status. Hoàn thành khi đổi transport vẫn tái dùng được use case.
