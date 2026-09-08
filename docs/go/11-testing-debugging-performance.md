# 11 — Testing, debugging và performance

[Mục lục](README.md) · Tiếp: [Vận hành](12-delivery-and-operations.md)

Mục tiêu: chọn bằng chứng đúng cho rủi ro, chẩn đoán bằng dữ liệu và không tăng timeout để che lỗi. Lệnh dưới đây là hướng dẫn trong module tương ứng; agent phải tuân thủ terminal policy. Fuzz/race/profile có thể tốn CPU, ghi cache hoặc file.

## Chọn loại test

| Rủi ro | Test phù hợp | Không được suy ra |
|---|---|---|
| Normalization đổi fingerprint | Unit/table test và golden case có chủ đích | Vài case không chứng minh không có collision |
| Handler decode/mapping sai | `httptest`, fake service | Không chứng minh database đúng |
| Transaction/uniqueness sai | Integration với DB thật riêng cho test | Mock SQL không chứng minh concurrent semantics |
| Report không bình thường | Fuzz + fixture regression | Fuzzer chạy ngắn không chứng minh an toàn mọi input |
| Duplicate delivery | Consumer integration/recovery test | Một happy path không chứng minh idempotency |
| Cross-tenant access | Test với hai tenant và nhiều entry point | Auth middleware pass chưa chứng minh quyền resource |
| Latency dưới tải | Load test workload có tỷ lệ thực tế | Benchmark local không phải API p95 |

Test thường nên kiểm tra input/output và side effect quan sát được. Không khóa thứ tự gọi mọi private helper. Fake phù hợp khi cần điều khiển lỗi, còn DB thật phù hợp khi cần chứng minh behavior DB. Test tạo resource phải dọn đúng phạm vi test, không dùng database làm việc hoặc production.

## Table test và lỗi boundary

Đặt tên case theo hành vi: empty title, duplicate event, timeout after commit. Với normalize, test bất biến đầu vào không bị mutate. Với config, dùng instance riêng thay vì sửa global. Với thời gian, truyền `now` hoặc clock nhỏ để không cần sleep.

`Example` có comment Output vừa dạy API vừa kiểm tra hành vi. Trong prototype xem [`example_test.go`](../../prototype/identity/example_test.go). Thêm example khi API public mới cần mẫu sử dụng; không cần test chỉ để xác nhận file docs tồn tại.

## Fuzz có thuộc tính rõ

Chọn NormalizeTitle hiện có trong prototype/identity/fingerprint.go. Thuộc tính: không panic với input trong contract; normalize hai lần cho cùng kết quả; không mutate dữ liệu gốc. Bổ sung seed tiếng Việt, chuỗi rỗng, separator và path Windows. Không đặt thuộc tính sai như “mọi hai chuỗi khác nhau phải có fingerprint khác nhau” vì normalization cố ý đồng nhất một số input.

Nếu xác định cần fuzz coverage, thêm FuzzNormalize vào prototype/identity/fingerprint_test.go hoặc file *_test.go cùng package. Sau khi test đó thực sự tồn tại, chạy trong prototype/identity:

```powershell
go test -run '^$' -fuzz '^FuzzNormalize$' -fuzztime=30s .
```

Đây chưa phải tên test có sẵn trong prototype. Input gây lỗi phải được giữ thành regression corpus; đọc [fuzz tutorial](https://go.dev/doc/tutorial/fuzz) để biết cấu trúc fuzz target và corpus.

## Debug loop có thể lặp lại

1. Ghi expected/actual, input nhỏ nhất, version và cấu hình.
2. Xác định lỗi deterministic hay phụ thuộc timing/tải.
3. Đặt giả thuyết có thể bác bỏ: ví dụ map bị mutate giữa hai lần resolve.
4. Thêm quan sát tại boundary hoặc dùng debugger breakpoint có mục tiêu.
5. Sửa nguyên nhân, giữ regression case và kiểm tra ảnh hưởng liên quan.

Với debugger như Delve, xác minh phiên bản tương thích toolchain trước khi cài. Các breakpoint hữu ích: sau normalize, sau alias lookup và trước decision; xem input, top score, runner-up, gap. Không bật dump payload/token để debug thuận tiện.

## Đo trước khi tối ưu

Benchmark sẵn có trong prototype:

```powershell
go test -run '^$' -bench '^BenchmarkExactMatch$' -benchmem -count=3 .
```

Ghi CPU/memory máy, Go version, dữ liệu, commit nếu có và tải nền. So nhiều mẫu cùng điều kiện. Nếu sửa fuzzy path, đo fuzzy benchmark riêng; đừng suy ra từ exact path. Tách setup khỏi timer và kiểm tra kết quả vẫn đúng.

Khi benchmark chỉ ra bottleneck, có thể lấy profile (ghi file ở thư mục hiện tại):

```powershell
go test -run '^$' -bench '^BenchmarkExactMatch$' -cpuprofile=cpu.out .
go tool pprof -top cpu.out
```

Profile CPU cho biết nơi tiêu CPU; heap profile xem allocations/retained memory; block/mutex profile tìm chờ đồng bộ; execution trace xem scheduling. Đọc [Go diagnostics](https://go.dev/doc/diagnostics). Không public pprof endpoint trên internet.

GC cost liên quan lượng allocation và live heap. Giảm copy/retain dữ liệu không cần thiết thường nên xem trước tuning GC. Slice nhỏ giữ backing array lớn có thể giữ nhiều memory; pointer không luôn nhanh hơn value. Không thêm `sync.Pool` khi chưa biết lifetime và số đo.

## Load test và failure injection

Tạo workload trộn exact/fuzzy, kích thước batch, tenants và artifact riêng. Đo throughput, p50/p95/p99, error rate, queue lag, DB pool wait và memory. Ghi rõ offered load và achieved load; hệ thống từ chối nhiều request có thể có latency đẹp giả tạo.

Tiêm lỗi từng loại trên môi trường disposable: DB chậm, object store lỗi, worker restart, publish lặp, timeout sau commit. Sau phục hồi phải kiểm tra dữ liệu và backlog, không chỉ HTTP trở lại 200. Budget lấy từ SPEC hiện hành; cấu hình test nhỏ để học phải ghi rõ chưa đủ tải xác nhận SLA.

Đạt khi báo cáo chứa giả thuyết, cách tái hiện, phép đo trước/sau, correctness check và giới hạn của kết luận.
