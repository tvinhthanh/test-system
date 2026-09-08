# 01 — Cài đặt và sử dụng

[Mục lục](README.md) · [Tiếp: Go căn bản](02-go-through-project.md)

## Chuẩn bị môi trường

Module khai báo `go 1.25.5`: đây là phiên bản Go tối thiểu yêu cầu, không phải khóa tuyệt đối phiên bản toolchain. Khi tái hiện số đo, ghi lại phiên bản thực tế. Xem [ý nghĩa dòng go trong go.mod](https://go.dev/doc/modules/gomod-ref).

Trên Windows, cài bằng bộ cài Go chính thức, sau đó mở lại PowerShell để nhận PATH mới. Xem [hướng dẫn cài Go](https://go.dev/doc/install). Không cần Node.js, PostgreSQL hay Docker để chạy prototype identity hiện tại.

Từ thư mục gốc repo trên máy này:

```powershell
Set-Location 'C:\source\Testing Project\prototype\identity'
go version
```

Nếu checkout ở nơi khác, thay đường dẫn cho đúng. **Tất cả lệnh Go bên dưới chạy trong `prototype/identity`**, nơi chứa `go.mod`; root repo chưa có Go module.

## Lần chạy đầu tiên

Chạy lần lượt:

```powershell
go test ./...
go run ./cmd/protoident
```

`go test` biên dịch và chạy test; thành công thường hiển thị `ok`, package CLI có thể hiện `[no test files]`. `go run` biên dịch rồi chạy CLI đo thuật toán, không mở HTTP server. Cả hai có thể tạo build cache; toolchain có thể cần được tải nếu môi trường cấu hình tự chọn phiên bản.

CLI in thông tin corpus, seed, threshold và báo cáo precision/recall. Precision mô tả độ đúng của các match được chọn; recall mô tả khả năng khôi phục các trường hợp cần match theo cách tính của harness. Đọc [`eval.go`](../../prototype/identity/eval.go) khi cần diễn giải chính xác mẫu số. Kết quả trên corpus tổng hợp không chứng minh chất lượng trên mọi report thực tế.

## Các lệnh thường dùng

| Mục đích | Lệnh |
|---|---|
| Xem ví dụ có kiểm tra output | `go test -run '^Example' -v .` |
| Chạy lại test, bỏ kết quả test cache | `go test -count=1 ./...` |
| Xem vì sao không auto-match | `go run ./cmd/protoident -diag` |
| Thử corpus khác | `go run ./cmd/protoident -suite=2000 -mutate=50 -seed=99` |
| Đo ảnh hưởng visibility | `go run ./cmd/protoident -visibility` |
| Đo ảnh hưởng độ cũ candidate | `go run ./cmd/protoident -age` |
| Thử recency gate 30 ngày | `go run ./cmd/protoident -recency-gate=30` |
| Đo chi phí khi project lớn hơn | `go run ./cmd/protoident -scale` |
| Benchmark exact match, kèm allocations | `go test -run '^$' -bench '^BenchmarkExactMatch$' -benchmem .` |

Sweep `-scale` và benchmark có thể tốn thời gian/CPU. Không cần chạy tất cả mỗi lần sửa. Tên cờ và mặc định nằm trong [`cmd/protoident/main.go`](../../prototype/identity/cmd/protoident/main.go); các giá trị thí nghiệm không tự cập nhật cấu hình sản phẩm.

## Đọc fixture Playwright

[`playwright_test.go`](../../prototype/identity/playwright_test.go) minh họa gọi `LoadPlaywrightReport` với fixture trong `testdata/`. Hàm trả về `([]LoadedTest, error)`.

CLI chưa hỗ trợ `-report`, vì vậy đừng dùng `go run ... -report=...`. Khi học cách đọc report, theo test có sẵn và xử lý `error` trước khi dùng kết quả. Chưa có lệnh upload report lên backend.

## Lỗi thường gặp

| Triệu chứng | Kiểm tra / cách xử lý |
|---|---|
| `go` không được nhận diện | Xác nhận đã cài Go và mở terminal mới sau khi PATH thay đổi |
| Không tìm được module / lỗi directory prefix | Chuyển vào `prototype/identity`, không chạy từ root repo |
| Toolchain quá cũ hoặc tải toolchain thất bại | So `go version` với `go.mod`; kiểm tra mạng và chính sách toolchain của máy |
| `flag provided but not defined` | Đối chiếu cờ với `main.go`; CLI không có API server hoặc report upload |
| Test báo không tìm thấy fixture | Giữ nguyên `testdata/` và chạy test từ module |
| Test thời gian thất bại | Đọc tên test, số đo và tải máy; đừng tăng budget trước khi tìm nguyên nhân |

Race detector là bước bổ sung khi làm concurrency:

```powershell
go test -race ./...
```

Nó chỉ phát hiện race trên đường chạy được thực thi, không chứng minh chương trình hoàn toàn không có race. Cần môi trường cgo/C compiler phù hợp; trên Windows cần kiểm tra yêu cầu toolchain riêng. Xem [Race Detector chính thức](https://go.dev/doc/articles/race_detector). Không cài compiler hoặc đổi cấu hình môi trường chỉ để lướt qua lỗi mà chưa xác định nguyên nhân.
