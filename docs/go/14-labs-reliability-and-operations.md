# 14 — Chặng 5–9: reliability và vận hành chính backend này

[Mục lục](README.md) · [Chặng 0–4](13-labs-foundation-to-api.md) · [Đánh giá](15-assessment-and-reference-solutions.md)

Tiếp tục trên backend sản phẩm của chặng 2–4, theo scope P1 đã chọn. Đường dẫn dưới đây là đích triển khai chưa tồn tại. Test/failure injection dùng môi trường của project có dữ liệu biệt lập; không dựng ứng dụng học thứ hai.

## Chặng 5 — Ingest events, idempotency và outbox

**Đọc:** SPEC §7.1–7.4, §6.9.6; chương 08–10.
**Đích:** internal/ingest, results, platform/idem, platform/outbox, test/integration.

**Làm:** POST /v1/runs/{localRunId}/events; cap và khóa (localRunId, event.sequence) theo SPEC trong scope project đã xác thực. Ghi durable effects và outbox trong cùng transaction. Trường hợp cùng key khác payload phải được chốt trong contract/OpenAPI nếu SPEC chưa đủ chi tiết, không tự tạo batch key thay khóa event.

**Test:** duplicate event, hai caller đồng thời, rollback, crash sau commit trước response, publish thành công rồi crash trước đánh dấu. Replay không nhân đôi kết quả.

**Hướng giải:** consumer idempotent, ack sau durable effect; outbox không biến mọi side effect thành exactly-once.
**Đạt:** đối soát event accepted → persisted → processed hoặc trạng thái lỗi có đường phục hồi, có bảng failure point và evidence.

## Chặng 6 — Port và tích hợp identity hai tầng

**Đọc:** SPEC §9, §6.9.10; prototype resolve/score/presence/visibility tests.
**Đích:** internal/identity với hợp đồng công khai và nested internal, cmd/worker.

**Làm:** đưa thuật toán đã kiểm chứng vào module sản phẩm cùng tests. ResolveInline chỉ exact trong ingest; ResolveDeferred từ sự kiện finish bền vững, với presence đúng. Candidate filtering DB, scoring Go theo SPEC.

**Test:** rename/move, same-title candidates, partial visibility, duplicate job, concurrent runs, fp version migration, cross-project candidates. Giữ separator/template normalization/status invariants.

**Hướng giải:** một implementation thuật toán được bảo trì; không fork một bản đơn giản để học. Không gọi Resolve đầy đủ mỗi event rồi coi là hai stage.
**Đạt:** review_queue không tự thành accepted match, restart không mất pending work, trace cho thấy exact/deferred chạy đúng nơi.

## Chặng 7 — Artifact theo protocol

**Đọc:** SPEC §7.7, §10.4; chương 10.
**Đích:** internal/artifact, platform/objstore, test/integration.

**Làm:** POST /v1/artifacts và POST /v1/artifacts/{artifactId}/complete; upload presigned/multipart đúng contract, cap, checksum, retention và quota. Khóa retry theo §7.2, không nghĩ ra identity artifact mới cho bài học.

**Test:** upload dở, cap, tranh quota, complete lặp, checksum/object thiếu, tenant B xin artifact A, cleanup retry.
**Hướng giải:** server sở hữu object key, verify trước available, reserve/release quota có concurrency semantics; streaming không giữ toàn file không giới hạn trong memory.
**Đạt:** đối soát quota/object/metadata và chứng minh download authorization bằng test sản phẩm.

## Chặng 8 — Tải và lifecycle của api/worker

**Đọc:** SPEC §6.4, §6.9.8, §14.2–14.3; chương 07/11.
**Đích:** cmd/api, cmd/worker, platform/queue, platform/metric, test/integration.

**Làm:** workload ingest/identity/artifact của sản phẩm, bounded pools và shutdown. Budget lấy từ SPEC; số đo máy dev ghi môi trường rõ, không tự sửa SLA để bài học pass.

**Test:** overload, DB/storage chậm, request cancel, queue đầy, shutdown khi claim/commit. Restart không mất kết quả đã nhận và retry không nhân đôi.
**Hướng giải:** profile/pool wait trước khi tăng worker/connection; đổi một biến và giữ correctness check.
**Đạt:** báo cáo workload, throughput, percentiles, errors/lag/memory và giới hạn; không dùng prototype microbenchmark thay E2E load test.

## Chặng 9 — CI, rollout, restore và incident

**Đọc:** chương 12, SPEC §14 và gate hiện hành.
**Đích:** pipeline/runbook của repo, backend/test/arch, backend/test/integration, môi trường staging của cùng ứng dụng.

**Làm:** build có version, kiểm tra architecture/race/security, health/metrics, rollout và rollback tương thích schema, backup/restore được kiểm chứng.
**Test:** release lỗi có alert, worker restart giữa publish/ack, restore vào DB kiểm chứng riêng và đối soát artifact references. Không fault injection vào dữ liệu người dùng.

**Hướng giải:** định RPO/RTO, rollback trigger và migration compatibility trước diễn tập; giữ tài nguyên kiểm chứng biệt lập nhưng dùng cùng artifact sản phẩm.
**Đạt:** người khác làm theo runbook repo phục hồi được; có timeline, thời gian restore và hạn chế còn lại.

## Bài tổng hợp trên luồng sản phẩm

Reporter → tạo run → events → exact/pending → finish/deferred → artifact → history với quyền đúng. Timeout sau commit rồi retry, restart worker và đối soát; tenant khác bị chặn; chứng minh recovery theo scope đã triển khai.

Chưa có feature/môi trường thì ghi chưa triển khai hoặc chưa kiểm chứng, không tạo bản demo thay thế để đánh dấu hoàn thành. Roadmap học đi cùng roadmap sản phẩm.
