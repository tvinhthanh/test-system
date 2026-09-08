# 07 — Concurrency, cancellation và vòng đời

[Mục lục](README.md) · Tiếp: [HTTP](08-http-and-contracts.md)

Mục tiêu: mọi goroutine đều có lý do tồn tại, giới hạn tài nguyên và đường dừng. Concurrency giúp chồng lấp công việc; parallelism là thực thi cùng lúc. Thêm goroutine không đảm bảo nhanh hơn nếu nghẽn ở DB pool hoặc mutex.

## Chọn primitive theo vấn đề

| Nhu cầu | Cách tiếp cận | Giới hạn |
|---|---|---|
| Bảo vệ nhiều field cùng bất biến | Mutex quanh critical section | Không bảo vệ process khác |
| Đếm một giá trị độc lập | Atomic nếu semantics đủ đơn giản | Nhiều atomic không tự thành transaction |
| Chuyển công việc giữa goroutine | Channel có capacity rõ | Không durable qua restart |
| Đợi nhóm công việc kết thúc | WaitGroup | Không tự truyền lỗi/cancel |
| Hủy cả cây I/O | Context và dependency tuân thủ context | Không cưỡng ép dừng hàm bỏ qua context |

Không copy struct chứa mutex sau khi bắt đầu sử dụng. Không giữ mutex khi gọi network chậm nếu không thật sự cần khóa toàn bộ thao tác. Shared map của resolver chỉ an toàn khi không có writer hoặc có đồng bộ đúng; việc có một `SafeIdentityStore` không làm mọi map trong package tự an toàn.

## Ownership của channel

Chỉ phía chịu trách nhiệm hoàn tất gửi mới đóng channel; nhiều producer cần coordinator đợi chúng kết thúc. Gửi vào channel đã đóng gây panic. Nhận từ channel đóng đọc hết buffer rồi nhận zero value với `ok == false`. Nil channel chặn gửi/nhận.

Một worker minh họa; `Job` và `handle` đại diện công việc trong platform/queue khi backend được triển khai, cần imports `context` và `fmt`:

```go
func worker(ctx context.Context, jobs <-chan Job,
    handle func(context.Context, Job) error) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case job, ok := <-jobs:
            if !ok {
                return nil
            }
            if err := ctx.Err(); err != nil {
                return err
            }
            if err := handle(ctx, job); err != nil {
                return fmt.Errorf("handle job: %w", err)
            }
        }
    }
}
```

Đây là một worker fail-fast, chưa phải pool hoàn chỉnh hoặc queue production. Caller phải xử lý lỗi, cancel peers, dừng producer và đợi cleanup. `handle` phải truyền context xuống I/O. Context vẫn có thể bị cancel ngay sau kiểm tra; side effect cần semantics riêng, không thể chỉ dựa vào `ctx.Err()`.

## Backpressure

Ví dụ tính tải cho pool của backend dự kiến: 4 worker, mỗi job chậm khoảng 100 ms: công suất lý tưởng khoảng 40 job/s trước overhead. Nếu nhận 100 job/s kéo dài, backlog sẽ tăng; tạo thêm goroutine vô hạn chỉ chuyển backlog vào RAM/connections.

Chọn: chặn producer có deadline, từ chối tạm thời, hoặc ghi vào durable queue có quota. Ghi lại queue capacity, timeout, hành vi overload. Production cần quota theo tenant để một project không chiếm hết worker.

## Context và thời gian

Truyền `ctx` là tham số đầu của hàm I/O. Tạo context con có deadline phù hợp và gọi `cancel` để giải phóng tài nguyên. Value thường dành cho metadata có phạm vi request; không dùng như túi chứa DB/config/dependency. Riêng project có quy ước truyền transaction qua context tại SPEC §6.9.6: dùng helper typed tập trung trong platform/pg, không suy rộng quy ước đó sang dependency khác. Xem [context API](https://pkg.go.dev/context).

Không lấy request context đã kết thúc để chạy durable background job. Request lưu công việc bền vững, worker xử lý bằng lifecycle context của nó, có deadline từng attempt. Đừng bỏ cancellation bằng cách thay mọi context thành `Background()`.

## Shutdown có thứ tự

1. Ngừng nhận việc mới/readiness phù hợp với deployment.
2. Cho HTTP request đang xử lý một khoảng drain hữu hạn.
3. Ngừng claim job mới; hoàn tất hoặc trả lease công việc đang chạy theo queue semantics.
4. Hết deadline thì cancel; đợi goroutine kết thúc trước khi đóng DB/storage.
5. Báo rõ request/job chưa hoàn tất để retry có thể khôi phục.

`time.Sleep` không phải cơ chế đồng bộ. Test dùng channel/barrier để tạo thứ tự xác định, kèm timeout chỉ để phát hiện treo. Đọc [memory model](https://go.dev/ref/mem) để hiểu quan hệ đồng bộ; không suy luận rằng goroutine đã ghi dữ liệu chỉ vì đã chờ đủ lâu.

## Thực hành trong project: store và worker

Bắt đầu bằng concurrent.go và concurrent_test.go trong prototype: đọc critical section GetOrCreate và test hai caller cùng fingerprint. Không sửa code đã đúng để tạo bài tập. Khi backend/platform/queue có worker thật, thêm case 20 job/3 worker vào test của nó, dùng barrier quan sát active tối đa 3, đóng input và cancellation. Đây là thông số test, không phải cấu hình production.

**Gợi ý đáp án:** producer gửi bằng `select` giữa jobs và `ctx.Done`; coordinator đóng channel sau khi producer hoàn tất; kết quả/lỗi phải được tiêu thụ kể cả khi hủy. Kiểm tra shutdown với handler chờ cancellation. Race detector kiểm tra đường chạy, không chứng minh mọi interleaving; rà ownership bổ sung.

Đạt khi có thể chỉ ra “goroutine này dừng ở đâu” cho tất cả goroutine, kể cả lỗi worker đầu tiên.
