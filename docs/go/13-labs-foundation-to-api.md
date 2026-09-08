# 13 — Chặng 0–4: học và phát triển trực tiếp trong project

[Mục lục](README.md) · [Tiếp: Chặng 5–9](14-labs-reliability-and-operations.md)

Mỗi chặng chỉ rõ code cần đọc, kiến thức Go, thay đổi hữu ích và bằng chứng. Dùng cùng repo, domain và protocol sản phẩm. Tên file tài liệu được giữ để liên kết cũ tiếp tục hoạt động.

## Nguyên tắc thực hành

- **Đã có:** prototype identity, CLI, fixtures và test. Chặng 0–1 bắt đầu ngay tại đó.
- **Chưa có:** backend. Chặng 2–9 là lộ trình đóng góp vào backend thật khi P1 được triển khai theo SPEC §6.9; hiện có thể đọc contract và ghi thiết kế.
- Không tạo module học, ứng dụng CRUD mẫu hoặc router thay thế protocol.
- Test double nằm trong test của module sản phẩm. Database/object storage test tách dữ liệu để kiểm chứng an toàn, vẫn chạy cùng code sản phẩm.
- Bài học đã có test bao phủ thì đọc và giải thích; chỉ thêm test khi tìm được case thiếu. Không sửa thuật toán chỉ để có diff.
- Mỗi thay đổi ghi SPEC liên quan, đường dẫn, acceptance cases và kết quả thật. Đường dẫn backend dưới đây là dự kiến, không phải file đã có.

## Chặng 0 — Học Go bằng fingerprint và config hiện có

**Đọc:** [fingerprint.go](../../prototype/identity/fingerprint.go), [config.go](../../prototype/identity/config.go), [example_test.go](../../prototype/identity/example_test.go); chương 02/06.

**Học:** struct, receiver, slice ownership, map ordering, zero value và config copy.

**Làm:** theo dấu Identity → Normalize → Fingerprint. Dự đoán hai TitlePath trong Example có bằng nhau không, giải thích tại sao. Đọc fingerprint_test.go trước khi đề xuất case path Windows, tiếng Việt hoặc input mutation còn thiếu.

**Kiểm chứng:** cùng input chuẩn hóa cho fingerprint ổn định; Normalize không sửa input; separator không bị bỏ; config case này không ảnh hưởng case khác.

**Thay đổi nếu cần:** bổ sung regression case vào test hiện có, chỉ sửa production function nếu case phát hiện bug thật. Không tạo package batch hoặc helper ngoài domain để học cú pháp.

**Đạt:** giải thích được behavior từ source và test; ghi rõ đã đọc hay đã chạy.

## Chặng 1 — Học error/JSON/CLI bằng report và protoident

**Đọc:** [playwright.go](../../prototype/identity/playwright.go), [playwright_test.go](../../prototype/identity/playwright_test.go), [retries_test.go](../../prototype/identity/retries_test.go), [CLI](../../prototype/identity/cmd/protoident/main.go).

**Học:** struct tag, error wrapping, slice traversal, flags và tách orchestration khỏi thuật toán.

**Làm:** theo fixture retries đến LoadedTest, đếm attempts/status bằng test đã có; so với JSON gốc. Đọc CLI hiện chỉ chạy corpus tổng hợp. Khi cần hỗ trợ đánh giá report partner trong Phase 0, mở rộng chính protoident/loader với input và report format được xác định; không tạo CLI thứ hai.

**Kiểm chứng:** lỗi đọc/parse không thành success; timed_out/interrupted/flaky được giữ; thay đổi command không phá đường chạy synthetic. Cờ report mới chỉ được ghi trong hướng dẫn sử dụng sau khi đã triển khai và kiểm chứng.

**Hướng giải:** reuse LoadPlaywrightReport, giữ source-of-truth identity. Test dữ liệu thiếu và report quá lớn nếu thay loader/input path; cap report file không tự đồng nhất cap event request.

**Đạt:** hiểu một report đi qua pipeline nào; feature mới nếu có giải quyết nhu cầu đánh giá thật, không chỉ in dữ liệu để học.

## Chặng 2 — Học HTTP qua POST /v1/runs

**Điều kiện:** backend bắt đầu theo Gate-0/scope P1 của SPEC. **Đọc:** §6.9, §7.2–7.3 và chương 08.

**Đường dẫn đích:** backend/api, cmd/api, internal/ingest, internal/results; tenancy/authz cung cấp scope và quyền. Chọn nơi đặt handler theo contract module, không tự lập projects CRUD API.

**Làm:** triển khai endpoint tạo run đúng payload SPEC với localRunId do reporter sinh. Test handler có thể dùng fake service; bản hoàn thành phải đi qua xác thực/phân quyền và durable persistence của chặng 3–4.

**Kiểm chứng:** retry localRunId trả run cũ với 200 theo §7.2; JSON sai/body vượt cap không ghi; token thiếu/sai là 401, đúng token sai project là 404. Không bỏ auth vì đang học handler.

**Hướng giải:** OpenAPI là hợp đồng, DTO theo cơ chế sinh type của backend; handler chỉ xử lý transport. Không trả entity DB trực tiếp.

**Đạt:** contract test pass; chặng có thể làm đồng thời 3–4 nhưng chưa đánh dấu endpoint hoàn tất khi chỉ có fake.

## Chặng 3 — Học persistence qua results và run lifecycle

**Đọc:** §8.4, §8.10, §6.9.5–6; chương 09. **Đường dẫn đích:** internal/results, platform/pg, cmd/migrate, test/integration.

**Làm:** persistence cho Run/TestResult/TestResultAttempt theo schema sản phẩm; tạo và finish run đúng §7.2. Read model dashboard thuộc query theo §6.9.5 khi tới scope đó. Dùng driver/migration version đã chọn cho backend, không tạo stack persistence để học riêng.

**Kiểm chứng:** restart không mất dữ liệu; create/finish lặp không lặp side effect; rollback giữa các ghi không để state nửa vời; late event theo protocol; query luôn scope đúng.

**Hướng giải:** caller điều phối transaction theo SPEC; unique constraints và conflict handling ở DB thật. Không coi mutex hoặc mock là bằng chứng nhiều replica an toàn.

**Đạt:** migration/schema và integration tests của project tái hiện được; môi trường test có dữ liệu biệt lập, không phải codebase thứ hai.

## Chặng 4 — Học security qua tenancy/authz

**Đọc:** §8.2, §8.8, §11.3 và chương 10. **Đường dẫn đích:** internal/tenancy, internal/authz, platform/httpx, test/integration.

**Làm:** credential và policy theo SPEC, authorize tập trung ở service; fixtures Organization/Workspace/Project/Environment cho hai tenant. Không xây đăng nhập JWT/OIDC riêng nếu roadmap hiện tại không yêu cầu.

**Kiểm chứng:** token HMAC và scope ingest theo protocol; expired/revoked theo semantics đã chốt; caller A không truy cập run B dù biết ID; worker/service path không bypass quyền cần bảo vệ.

**Hướng giải:** middleware xác thực, service authorize resource thật, persistence áp scope. Fake principal chỉ dùng trong test có mục đích, không thay verification của endpoint.

**Đạt:** ma trận quyền tự động qua entry point thực; cả chặng 2–4 tạo được một lát chức năng sản phẩm hoàn chỉnh.
