# 04 — Kiến trúc và workflow của project

[Mục lục](README.md) · [Tiếp: Lộ trình nhập môn](05-learning-path.md)

## Nguồn chuẩn và trạng thái

[SPEC §6.9](../../SPEC.md) đã định nghĩa modular monolith; đây là kiến trúc đích của chính project, chưa có backend implementation. Học hiện tại ở `prototype/identity`; khi triển khai P1, đưa thay đổi vào `backend` theo gate và scope của SPEC. Không tạo ứng dụng mẫu hoặc module học riêng.

## Tên module phải thống nhất

| Khái niệm từng dùng trong trao đổi | Module trong SPEC §6.9 | Trách nhiệm |
|---|---|---|
| projects | `tenancy` | Organization, Workspace, Project, Environment |
| auth + rbac | `authz` | ApiToken, Membership, Role; xác thực và authorize tập trung |
| runs | `results` | Run, TestResult, TestResultAttempt |
| ingest | `ingest` | Nhận event theo protocol |
| identity | `identity` | Fingerprint, alias, resolution, merge/split |
| artifact | `artifact` | Upload, cap, retention và metadata |
| Dashboard | `query` | Read model, chỉ đọc |
| Flaky xuyên run | `flaky` | Tổng hợp FlakySignal |

`triage` và `billing` theo scope/gate trong SPEC; `execution` và `testmgmt` chưa tạo trước giai đoạn tương ứng. Không tạo song song `auth`, `rbac`, `runs`, `projects` chỉ để khớp giáo trình cũ.

## Đường dẫn đích

Cây rút gọn từ §6.9.2, **đường dẫn dự kiến, chưa tồn tại**:

```text
backend/
  cmd/api/
  cmd/worker/
  cmd/migrate/
  internal/
    platform/
      config/
      log/
      metric/
      pg/
      objstore/
      queue/
      outbox/
      httpx/
      idem/
    tenancy/
    authz/
    ingest/
    identity/
    results/
    artifact/
    flaky/
    query/
  api/                # OpenAPI và generated types
  test/arch/
  test/integration/
```

`backend/go.mod` khi được tạo là module sản phẩm, không phải module bài tập. Các package nghiệp vụ không cần mỗi nơi một go.mod. Prototype chỉ chuyển sang cấu trúc đích khi có kế hoạch port giữ nguyên invariants/test; không duy trì hai bản thuật toán lệch nhau.

## Ranh giới và phụ thuộc

Platform không import domain. Tenancy/authz không import domain. Domain chỉ dùng hợp đồng công khai của module khác; nội bộ mỗi domain nằm sau `internal/` lồng theo §6.9.4. Go import package, không import từng file: file hợp đồng là quy ước tổ chức; nested internal mới là cơ chế compiler hạn chế truy cập implementation.

Đồ thị phải không có chu trình; tối đa 3 domain import trực tiếp theo §6.9.3. Không tạo `common/models/entity` gom type nghiệp vụ. Interface nhỏ ở caller khi một nơi cần, ở module sở hữu khi nhiều nơi dùng theo quy tắc SPEC.

Mỗi bảng có module sở hữu. Code ghi không JOIN xuyên module; gọi API module trong use case. `query` là ngoại lệ được đọc nhiều bảng cho dashboard, không được ghi hoặc chứa business rule. Test kiến trúc phải giữ các quy tắc này khi backend có code.

## Luồng request của project

HTTP decode/validate → xác thực principal → service authorize resource → use case dùng transaction → adapter persistence → response theo OpenAPI. API nhận run/event/artifact ở §7.2, không tự thêm route học.

Một use case dùng một transaction theo §6.9.6; caller quản lý và truyền transaction xuống qua cơ chế context của project. Đây là quy ước riêng được SPEC yêu cầu, không phải lời khuyên dùng context chứa mọi dependency. Helper typed thuộc platform/pg, module không tự mở transaction con hoặc dùng pool ngoài transaction đang chạy.

Kết quả test, trạng thái run và công việc không được mất dùng outbox theo §6.9.6. Metric mất được có thể dùng cơ chế nhẹ hơn. Deferred identity chạy từ durable event hoàn tất run, không từ goroutine sống nhờ request.

## Học bằng thay đổi thật

| Cách làm | Lợi ích | Đánh đổi |
|---|---|---|
| Đọc code, giải thích và chạy test hiện có | Học ngay, không thêm code thừa | Chưa chứng minh được backend chưa triển khai |
| Bổ sung case thiếu hoặc feature theo roadmap | Học và tạo giá trị trực tiếp cho project | Cần scope, review và integration evidence |

Bắt đầu bằng cách thứ nhất; chỉ sửa khi có khoảng trống hoặc yêu cầu thật. Không thêm interface/helper trống để hoàn thành bài. Khi tới P1, xây từng lát chức năng có auth và persistence, không xây API demo rồi thay lại.

## Quy trình một thay đổi

1. Đọc SPEC và test liên quan, xác định module sở hữu.
2. Ghi hành vi trước/sau và rủi ro quyền/dữ liệu.
3. Sửa đúng package; test hành vi thay đổi, không mirror implementation.
4. Format file sửa, chạy kiểm tra cần thiết theo terminal policy.
5. Cập nhật docs/lệnh chạy và kết quả thật, ghi rõ phần chưa kiểm chứng.

Prototype chạy test trong `prototype/identity`. Lệnh backend chỉ thêm vào docs sau khi module/backend thực sự có. Thay đổi tài liệu hiện tại không tự triển khai API, migrate DB hoặc deploy.
