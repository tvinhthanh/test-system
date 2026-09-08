# 04 — Kiến trúc và workflow Go

[Mục lục](README.md) · [Tiếp: Lộ trình](05-learning-path.md)

## Phạm vi và lựa chọn

Go monolith đã được chốt trong [SPEC, QĐ-9](../../SPEC.md). Cấu trúc dưới đây là **đề xuất học và triển khai sau**, chưa tồn tại trong codebase và không thay thế quyết định gate của Phase 0.

| Phương án | Ưu điểm | Đánh đổi |
|---|---|---|
| A — Mở rộng prototype thành một package backend lớn | Ít setup, nhanh cho thử nghiệm | Trộn HTTP, lưu trữ và thuật toán; khó kiểm thử quyền và thay persistence |
| B — Monolith chia theo nghiệp vụ | Ranh giới rõ, tái dùng thuật toán, dễ kiểm thử service | Cần xác định ownership và dependency lúc đầu |

Khuyến nghị B khi bắt đầu backend. Chỉ tạo package khi có trách nhiệm cụ thể; không cần tạo đủ interface/layer trống cho mọi module.

```text
backend/                     # Đề xuất, chưa triển khai
  go.mod
  cmd/api/main.go             # Khởi tạo dependencies, HTTP lifecycle
  internal/
    ingest/
    identity/
    artifact/
    auth/
    rbac/
    runs/
    projects/
    platform/                # Adapter hạ tầng thực sự dùng chung
      postgres/
      objectstore/
      queue/
    config/
```

Đây là các package trong một backend Go module; không cần một `go.mod` cho mỗi nghiệp vụ. `internal` hạn chế import từ bên ngoài cây cho phép, nhưng không tự ngăn coupling giữa các package bên trong.

## Ai sở hữu việc gì?

| Module | Trách nhiệm |
|---|---|
| `ingest` | Validate batch/event, điều phối tiếp nhận và chống xử lý lặp theo protocol |
| `identity` | Fingerprint, alias, resolution, merge/split; giữ phần tính toán độc lập HTTP |
| `artifact` | Metadata, quota/cap, vòng đời upload/download và object storage |
| `auth` | Xác thực token/credential thành principal đáng tin cậy |
| `rbac` | Quyết định quyền qua một hàm `authorize(principal, resource, action)` tập trung |
| `runs` | Vòng đời run, finish, trạng thái và truy vấn lịch sử run |
| `projects` | Dữ liệu/cấu hình project và quan hệ scope với workspace/organization |

`identity` ở đây là **test identity**, không phải tài khoản người dùng. `auth` trả lời “ai gọi”; `rbac` trả lời “được làm gì trên resource nào”. Run là dữ liệu, không phải tầng scope quyền.

## Chiều dependency

HTTP handler nhận request → gọi service nghiệp vụ → service kiểm tra quyền và phối hợp repository/adapter. Domain tính fingerprint/score không import router, SDK object storage hoặc SQL driver.

- Handler xử lý transport: decode, giới hạn body, mapping response/error. Business rule thuộc service.
- Service nhận dependency qua constructor/tham số. `main` khởi tạo và nối các thành phần.
- Định nghĩa interface nhỏ tại nơi sử dụng khi cần thay adapter hoặc kiểm thử; không tạo interface cho mọi struct theo thói quen.
- Gọi module khác qua API service rõ ràng. Không đọc thẳng bảng của module khác khắp codebase.
- Nếu `ingest` và `runs` phụ thuộc vòng, đưa orchestration vào một lớp có trách nhiệm rõ; không sửa bằng package `common` chứa mọi thứ.

## Dữ liệu, quyền và công việc nền

Service gọi chung `authorize`, repository luôn scope dữ liệu theo tenant/project phù hợp. Không tin `projectId` do client gửi chỉ vì token hợp lệ. Cần test truy cập chéo tenant khi backend được triển khai.

Xác định transaction ở use case: ví dụ lưu event và trạng thái xử lý liên quan phải nhất quán khi retry. Unique constraint/upsert chống tạo trùng xuyên process; mutex Go không thay thế được. Nếu ghi DB rồi phát job có nguy cơ mất job giữa hai bước, thiết kế cơ chế publish bền vững như transactional outbox theo nhu cầu protocol.

Worker cần retry có giới hạn, idempotency và backpressure. Goroutine đơn lẻ không phải durable queue. Chọn queue/router/DB driver sau khi kiểm tra dependency và yêu cầu cụ thể; bộ docs này chưa chọn hoặc cài thư viện mới.

## Quy ước khi triển khai

- Truyền `context.Context` qua I/O để hủy công việc và giới hạn thời gian.
- Wrap lỗi có ngữ cảnh, phân loại bằng cơ chế error của Go; không so sánh chuỗi lỗi để quyết định HTTP status.
- Log có request/run/project ID phù hợp, không log token hoặc toàn bộ payload mặc định. Theo dõi latency, lỗi và queue depth ở biên hệ thống.
- Không giữ global config có thể mutate; truyền config/dependency rõ ràng.
- Dùng thư viện chuẩn và code sẵn có trước; thêm abstraction khi có ranh giới cần bảo vệ.
- Khi port identity, giữ các test bất biến và tách adapter persistence. Không chép in-memory store thành database production.

## Workflow cho một thay đổi

1. Đọc SPEC, code và test liên quan; xác định hành vi mong đợi.
2. Với lỗi, tái hiện và tìm nguyên nhân trước khi sửa threshold/timeout.
3. Sửa nhỏ trong đúng module; cập nhật test hành vi khi thay logic.
4. Chạy format/test phù hợp khi được phép; đọc diff sau format.
5. Ghi lại kết quả kiểm tra thật và cập nhật docs nếu API/cờ thay đổi.

Ví dụ lệnh trong module hiện tại (chỉ là hướng dẫn, không được chạy bởi việc thêm docs):

```powershell
gofmt -w config.go
go test ./...
go vet ./...
```

Chỉ format file thực sự sửa; `config.go` là ví dụ. Chạy `-race` khi sửa shared state và benchmark khi sửa hot path, không dùng mọi phép đo cho một sửa tài liệu. Agent phải xin phép các lệnh thuộc nhóm yêu cầu approval theo chính sách terminal project.
