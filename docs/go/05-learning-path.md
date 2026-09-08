# 05 — Lộ trình thực hành

[Mục lục](README.md)

Mỗi buổi khoảng 45–90 phút, điều chỉnh theo nền tảng của bạn. Bài tập thay đổi code là tùy chọn để thực hành; bộ tài liệu này chưa thực hiện chúng và không mở rộng phạm vi Phase 0.

| Buổi | Học và thực hành | Hoàn thành khi |
|---|---|---|
| 1 | Đọc setup; chạy prototype và example nếu môi trường đã sẵn sàng | Giải thích được module/package/main và nơi chạy lệnh |
| 2 | Đọc `Identity`, `Normalize`, `Fingerprint` | Chỉ được trường nào ảnh hưởng fingerprint, vì sao cần separator |
| 3 | Đọc loader, struct tag, `error`, slice | Theo được report → `LoadedTest`, phân biệt final status với attempts |
| 4 | Theo dấu `Resolve`, `DefaultConfig`, `Score` | Giải thích được 4 decision và vai trò của gap/presence |
| 5 | Đọc test và thử thêm một case normalization chưa được bao phủ | Case kiểm tra hành vi cụ thể, không chỉ kiểm tra lại cùng implementation |
| 6 | Đọc benchmark và `SafeIdentityStore` | Phân biệt race trong memory với tính atomic ở database |
| 7 | Đọc kiến trúc; phác luồng nhận event trên giấy | Chỉ rõ ai authorize, ai lưu dữ liệu, khi nào fuzzy và retry |

## Bài tập 1 — Hiểu copy của slice

Đọc `ExampleConfig`, dự đoán khác biệt giữa thay toàn bộ `TitlePath` và sửa một phần tử sau khi copy struct. Viết ghi chú: dữ liệu nào được copy, dữ liệu nào còn chung backing array? Đối chiếu cách `Normalize` tạo slice mới.

Tiêu chí: giải thích được vì sao một “bản sao” có thể vô tình sửa input ban đầu.

## Bài tập 2 — Điều tra một review_queue

Dùng ví dụ `ExampleConfig`: candidate đã cũ 60 ngày, recency gate là 30. Đọc điều kiện trong `Resolve` để giải thích kết quả. Sau đó dự đoán khi gate bằng 0 trước khi tự chạy thử.

Tiêu chí: phân biệt “điểm đủ cao” với “thỏa toàn bộ điều kiện auto-match”; không kết luận `MatchedID` ở review queue đã được ghép thật.

## Bài tập 3 — Đọc một report thật

Theo `playwright_test.go` và fixture `playwright-report-retries.json`. Chọn một test có retries, ghi lại trạng thái cấp test, trạng thái từng attempt và `FinalStatus` sau mapping.

Tiêu chí: giải thích vì sao chỉ đọc `unexpected` sẽ mất thông tin timeout. Không tự suy ra flaky khác với verdict Playwright.

## Bài tập 4 — Đọc một benchmark đúng cách

Đọc `BenchmarkExactMatch`, xác định phần chuẩn bị store và phần nằm trong timer. Nếu chạy, ghi lại Go version, tên benchmark, thời gian và allocations; so trong cùng môi trường.

Tiêu chí: nêu được ít nhất ba thứ benchmark chưa đo, ví dụ PostgreSQL, HTTP và queue. Không chuyển `ns/op` local thành cam kết latency API.

## Bài tập 5 — Thiết kế một vertical slice

Phác thảo luồng: principal hợp lệ → quyền trên project → nhận event → exact identity hoặc pending → lưu trạng thái → khi run đủ điều kiện hoàn tất thì fuzzy resolution. Đây là bài thiết kế, chưa yêu cầu dựng backend.

Trả lời: request retry có tạo trùng không? Hai shard gửi cùng fingerprint thì sao? Process dừng sau khi commit nhưng trước khi gửi job thì sao? Dữ liệu project khác được loại ở đâu?

Tiêu chí: mỗi quyết định có module sở hữu; không có hai nơi tự viết quy tắc quyền khác nhau.

## Tự kiểm tra trước khi nhận việc backend

- Tôi phân biệt được Go module, package và module nghiệp vụ.
- Tôi đọc được pointer receiver, slice/map, error và struct tag.
- Tôi biết `Resolve` là tính decision, chưa phải transaction production.
- Tôi giải thích được exact inline và fuzzy deferred, cùng hạn chế visibility.
- Tôi chọn test theo rủi ro thay đổi và báo đúng phần chưa kiểm chứng.
- Tôi biết phần nào đã có trong code và phần nào mới là thiết kế.

Nếu còn vướng cú pháp, quay lại [A Tour of Go](https://go.dev/tour/). Nếu vướng domain, đọc [walkthrough](03-identity-walkthrough.md) và [README prototype](../../prototype/identity/README.md) trước khi thay thuật toán.
