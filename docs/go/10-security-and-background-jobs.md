# 10 — Security, artifact và background jobs

[Mục lục](README.md) · Tiếp: [Testing và performance](11-testing-debugging-performance.md)

Mục tiêu: quyền truy cập và xử lý lặp vẫn đúng khi chạy nhiều worker. Tất cả flow dưới đây là thiết kế học; đối chiếu [SPEC](../../SPEC.md) trước khi dùng cho sản phẩm.

## Auth và RBAC theo resource

Auth xác thực credential thành principal. RBAC quyết định principal có được làm action trên resource hay không. Đọc đúng loại credential của endpoint: không thay token HMAC trong protocol bằng JWT chỉ vì quen framework.

Thực hành tại authz với token HMAC-SHA-256 theo SPEC §8.8; không bổ sung OIDC/JWT chỉ để học. Decode credential chưa phải verify. Với endpoint dùng cookie, xem CSRF/Origin theo SPEC; CORS không phải cơ chế phân quyền. Test thuật toán và scope đã chọn cho sản phẩm, không tự viết mật mã.

Project yêu cầu một điểm quyết định `authorize(principal, resource, action)` ở service layer. Middleware chỉ xác thực chưa đủ vì quyền phụ thuộc resource thật. Scope đi theo Organization → Workspace → Project → Environment; Run không phải tầng quyền.

## Ma trận quyền phải chạy thành test

| Caller | Resource | Kết quả cần kiểm chứng |
|---|---|---|
| Không có credential | Endpoint bảo vệ | Bị từ chối trước side effect |
| Token hết hạn/bị thu hồi | Project cũ từng có quyền | Không tiếp tục truy cập |
| Viewer A | Đọc run thuộc A | Chỉ trả field được phép |
| Viewer A | Ghi/merge identity thuộc A | Bị từ chối nếu policy không cấp |
| Thành viên A | Run/artifact thuộc B | Không đọc/ghi được dù biết ID |
| Worker cho A | Job payload gán project B | Bị từ chối hoặc job được chứng minh nguồn/scope đúng |

Cache quyền cần có semantics thu hồi và key đầy đủ scope. Không cache chỉ theo user ID. Audit ghi actor/action/resource/outcome, không ghi raw credential. Lỗi 403/404 phải nhất quán với chính sách chống lộ resource, không làm lộ existence qua response body.

## Artifact là một vòng đời

Trong module artifact của backend, biểu diễn vòng đời theo SPEC §7.7; các trạng thái giải thích ở đây là: pending upload → uploaded chưa verify → available, hoặc failed/expired. Chưa xác nhận object thực sự hợp lệ thì chưa công bố available.

1. Authorize thao tác upload trên run/project.
2. Reserve quota có kiểm soát concurrency; sinh object key phía server.
3. Upload theo cap và expiry; client không tùy ý chọn path của tenant khác.
4. Verify object metadata/size/checksum phù hợp trước finalize.
5. Retry finalize idempotently; cleanup upload dở và quota reservation hết hạn.

Download cũng authorize; signed URL là capability có thời hạn, tránh đưa vào log. Không phục vụ HTML/report không tin cậy cùng origin ứng dụng. Trace viewer và sandbox phải theo SPEC. Multipart upload cần cleanup các phần dang dở; streamed upload tránh `ReadAll` với file không bị giới hạn.

## Queue delivery và retry

Job có identity ổn định, scope, loại thao tác, version payload và attempt metadata. Payload không chứa credential sống dài. Worker revalidate các điều kiện có thể đã đổi như resource bị xóa hoặc quyền cần thiết.

Phân loại lỗi: transient (timeout/throttling) có retry; permanent (payload không hợp lệ) dừng hoặc vào dead-letter. Backoff tăng dần có jitter và trần, giới hạn cả số lần lẫn tổng thời gian. Không retry lỗi permission như lỗi network.

Nếu lease job ngắn hơn thời gian xử lý, worker khác có thể claim cùng job. Renew lease có điều kiện hoặc dùng cơ chế fencing/version để worker cũ không ghi kết quả muộn. Idempotency vẫn cần vì broker không đảm bảo mọi side effect chỉ xảy ra một lần.

Đo queue lag và tuổi job lâu nhất theo loại workload. Tách pool ingest/identity/artifact khi tác vụ chậm có thể làm nghẽn workload quan trọng; không tạo nhiều service chỉ để có nhiều queue.

## Bài tập

Cho job finalize artifact bị deliver hai lần, object vượt cap và token download thuộc project khác. Dự đoán domain state, quota và audit sau mỗi lần.

**Gợi ý đáp án:** hai delivery không tăng quota hai lần; quá cap không chuyển available; cross-tenant bị chặn trước cấp URL. Cleanup phải có idempotency vì có thể retry. Kiểm tra đồng thời hai upload cùng tranh quota cuối để lộ race check-then-write.

Đạt khi test kiểm chứng quyền cả HTTP lẫn worker, và có cách khôi phục job/artifact dang dở. Theo dõi dependency/toolchain bằng quy trình [Go security](https://go.dev/doc/security/); quét lỗ hổng không thay thế test authorization.
