# 12 — Delivery và vận hành production

[Mục lục](README.md) · Tiếp: [Chặng từ cơ bản đến backend](13-labs-foundation-to-api.md)

Mục tiêu: deploy được một phiên bản, biết nó hoạt động ra sao và có đường lui khi lỗi. Đây là hướng dẫn xây runbook của chính project; chưa tạo pipeline, container hoặc deployment.

## Build có thể truy nguồn

Một artifact phải gắn source revision, Go version, dependency versions và cấu hình build cần thiết. Khi có VCS, giữ manifest/lock phù hợp và kiểm tra diff dependency. Không dùng tag `latest` làm cách duy nhất nhận diện bản phát hành.

Pipeline đề xuất: kiểm tra format → unit/vet → integration DB → race trên phần concurrency → build artifact → security scan → staging smoke/recovery → rollout. Job và công cụ thực tế phải được chọn theo runner/OS; race cần môi trường compiler phù hợp. Đừng cài công cụ không pin phiên bản trong mỗi build rồi mặc định kết quả tái lập được.

`govulncheck` là công cụ bổ sung, chưa có trong dependency prototype. Xác nhận phiên bản/cách cài trước khi dùng. Báo cáo vulnerability cần triage reachability và bản vá; scan pass không chứng minh toàn bộ hệ thống an toàn. Xem [Go security](https://go.dev/doc/security/).

## Cấu hình runtime

Load config một lần lúc startup, validate giá trị bắt buộc/range, báo lỗi an toàn và dừng nếu không hợp lệ. Phân biệt config thường với secret. Secret lấy từ cơ chế triển khai phù hợp, không lưu trong image/repo hoặc log.

Giới hạn process, DB pool, worker count, queue cap và HTTP body phải được tính cùng nhau. Replica count tăng nhân tài nguyên dùng ở downstream. Ghi default và lý do, không để một env var vô tình tắt giới hạn bằng zero value.

## Quan sát đủ để tìm nguyên nhân

| Tín hiệu | Câu hỏi |
|---|---|
| Request rate/error/latency theo route template | Endpoint nào đang hỏng/chậm? |
| Queue depth và tuổi job | Công việc có bị bỏ lại dù HTTP vẫn khỏe? |
| DB pool wait/query latency | Chờ connection hay chờ query? |
| Identity exact/fuzzy/review và conflict | Chất lượng hoặc phân phối dữ liệu có đổi? |
| Artifact failure/reservation/cleanup | Có mất quota hoặc object mồ côi? |

Log dùng trường có cấu trúc; request/job ID giúp nối sự kiện. Metric label không chứa run ID hoặc dữ liệu cardinality vô hạn. Trace truyền context qua API/DB/queue với quy tắc sampling và redaction. Error log cần actionable context, không lặp cùng một lỗi ở mọi layer.

SLI là cách đo thực tế, SLO là mục tiêu trong cửa sổ thời gian. Với ingest/worker của project, đo tỷ lệ request hợp lệ thành công và tuổi job theo mục tiêu SPEC; phải ghi định nghĩa denominator, exclusions và cửa sổ. Đừng chỉ alert CPU: người dùng có thể mất event trong khi CPU thấp.

## Health và rollout

Liveness nói process còn hoạt động; readiness nói có thể nhận công việc theo thiết kế. Không khiến một dependency chập chờn làm tất cả replica restart liên tục. Dùng deadline nhỏ cho health check và hiểu ảnh hưởng đến traffic.

Trước rollout: kiểm tra tương thích schema giữa binary cũ/mới, migration và queue payload version. Trong rollout: theo error rate, latency và queue lag. Nếu vượt ngưỡng đã thống nhất, ngừng tăng traffic/rollback theo runbook. Rollback binary không tự đảo dữ liệu hoặc destructive migration.

## Backup chỉ có ý nghĩa khi restore được

Ghi RPO (mất tối đa bao nhiêu dữ liệu) và RTO (mất bao lâu để phục hồi), nơi backup, retention và người chịu trách nhiệm. Khôi phục vào môi trường riêng, kiểm tra run/event/alias và artifact references; ghi thời gian thực tế. Chỉ thấy backup job xanh chưa đạt bài học này.

Không dùng delete/migration/chaos trực tiếp trên môi trường có dữ liệu cần giữ. Diễn tập recovery dùng tài nguyên test/staging của project có tên và scope rõ, chạy cùng artifact sản phẩm.

## Runbook mẫu: ingest chậm

1. Xác nhận thời điểm bắt đầu, route, tenant bị ảnh hưởng và mức mất chức năng.
2. So deploy/config gần nhất; xem pool wait, DB latency, worker backlog.
3. Giảm tác động bằng biện pháp đã hiểu: giảm workload nền, rollback bản liên quan hoặc throttle theo policy. Không tăng pool/replica mù.
4. Khi phục hồi, đối soát event accepted với persisted/processed; replay có idempotency nếu cần.
5. Ghi timeline, nguyên nhân, khoảng trống quan sát và hành động ngăn tái diễn.

## Bài tập vận hành

Khi có bản build backend của project, triển khai chính artifact đó trên môi trường staging/kiểm chứng của project; cố ý cấu hình DB không truy cập được trong môi trường kiểm chứng. Kiểm tra startup/readiness/error không lộ secret. Sau đó restore kết nối, tạo run, restart worker giữa job và xác nhận dữ liệu.

**Tiêu chí:** có artifact/version truy nguồn, dashboard tối thiểu, một alert kích hoạt được, runbook rollback và bằng chứng restore. Tự ghi các giới hạn chưa đạt; không gọi môi trường local một process là production-ready chỉ vì container chạy được.
