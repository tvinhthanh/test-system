# 15 — Đánh giá năng lực và lời giải định hướng

[Mục lục](README.md) · [Chặng](13-labs-foundation-to-api.md) · [Runbook học](16-study-and-work-handbook.md)

Không có chứng chỉ “master” từ số trang đã đọc. Dùng ma trận này để biết việc gì bạn tự làm được, việc gì cần review. Điểm phản ánh bằng chứng, không phản ánh chức danh hoặc số năm kinh nghiệm.

## Cách chấm

Mỗi năng lực chấm 0–3: **0** chưa làm; **1** làm theo hướng dẫn; **2** làm độc lập, test được và giải thích tradeoff; **3** xử lý được tình huống mới, review người khác và cung cấp evidence dưới lỗi/tải.

| Năng lực | Bằng chứng |
|---|---|
| Ngôn ngữ/ownership | Chặng 0–1, giải thích nil/slice/interface và lỗi |
| HTTP/contracts | Chặng 2, boundary tests và error mapping |
| Persistence | Chặng 3/5, transaction/concurrent replay trên DB thật |
| Authorization | Chặng 4/7, ma trận tenant qua mọi entry point |
| Concurrency/reliability | Chặng 5/8, bounded worker và crash recovery |
| Domain identity | Chặng 6, giữ invariants và giải thích hai stage |
| Debug/performance | Chặng 8, tái hiện, profile, kết luận có giới hạn |
| Delivery/operations | Chặng 9, rollback/restore/incident drill |

Mốc “đủ nhận feature có review”: ngôn ngữ, HTTP, persistence, quyền đạt 2; biết giới hạn của mình ở phần còn lại. Mốc “tự sở hữu phần backend đã triển khai của project”: tất cả đạt ít nhất 2 và capstone hoàn tất. Mức 3 cần tình huống mới và phản biện thực tế, không chỉ chạy lại kịch bản đã nhớ.

**Không dùng tổng điểm để bù lỗ hổng:** cross-tenant leak, mất event đã ack, chưa khôi phục được dữ liệu hoặc goroutine không có đường dừng đều là lý do chưa đạt mốc tương ứng.

## Lời giải tình huống production

### Client timeout nhưng event đã tồn tại

Không xóa event để “cho retry chạy lại”. Tra idempotency record theo scope/key, so payload và trả kết quả tương ứng. Test điểm chết sau commit trước response. Câu trả lời thiếu scope hoặc conflict behavior chưa đủ.

### Worker xử lý job hai lần

Tìm side effect nào cần chống lặp. Gắn job/event key với transaction ghi kết quả, ack sau đó; khi ack bị mất, retry đọc kết quả cũ. Nếu side effect là external API, cần idempotency contract của API đó hoặc cơ chế reconciliation; local DB transaction không bao trùm hệ thống ngoài.

### Review queue tăng nhưng precision vẫn tốt

Kiểm tra visibility, tỷ lệ rename, candidate recency và dữ liệu đầu vào trước threshold. Presence chưa đầy đủ có thể làm nhiều candidate gần điểm nhau, gap bảo vệ việc ghép sai. Hạ gap chỉ để làm dashboard đẹp có thể gây wrong merge. Phải thử trên corpus phù hợp và kiểm tra precision/recall cùng nhau.

### CPU thấp nhưng p95 tăng

Kiểm tra chờ pool, network, locks, queue và downstream. CPU profile một mình không đủ để kết luận code không có vấn đề. Tăng replicas có thể làm DB connection pressure nặng hơn; đưa ra số đo trước khi thay.

### Auth token hợp lệ vẫn lộ artifact tenant khác

Root cause có thể là thiếu resource authorization hoặc object key/URL cấp không scope. Sửa service/repository boundary, bổ sung test caller A truy cập resource B bằng ID thật; test middleware verify signature không bắt được bug này.

### Rollback app nhưng schema đã bỏ cột cũ

Binary cũ không tương thích nữa. Cần rollout expand-contract và rollback window đã thiết kế trước. Trong incident thực, chọn recovery theo trạng thái và backup đã kiểm chứng; không giả vờ rollback image là đủ.

## Câu hỏi code review

1. Input nào bị từ chối trước side effect? Deadline có đến tận dependency không?
2. Ai sở hữu slice/map? Có shared writer hoặc copied mutex không?
3. Quyền resource được kiểm tra ở service nào và query scope nào?
4. Transaction giữ bất biến gì? Crash ở giữa để lại state nào?
5. Retry cùng payload/khác payload diễn ra ra sao?
6. Goroutine/job dừng thế nào? Overload có giới hạn không?
7. Test nào có thể bắt lỗi này tái xuất? Mock có đang che semantics DB không?
8. Log/metric nào giúp xác định lỗi mà không lộ secret?
9. Thay đổi có tương thích dữ liệu/protocol cũ? Rollback là gì?

## Mẫu ghi nhận thay đổi trong project

```text
Chặng / ngày:
Trạng thái: chưa làm | đang làm | đạt có evidence | còn giới hạn
Go / DB / driver / công cụ và phiên bản:
Working directory và các bước tái hiện:
Hành vi được triển khai:
Acceptance cases và kết quả thực tế:
Failure injection, durable state và recovery:
Tradeoff đã chọn / phương án đã cân nhắc:
Điều chưa kiểm chứng:
Đường dẫn code, test, báo cáo, runbook:
Reviewer / câu hỏi còn mở:
```

Lời giải ở đây giải thích hướng thiết kế, không phải implementation mẫu đã chạy. Chặng đọc code cần giải thích và evidence từ source/test; chặng triển khai cần thay đổi hữu ích và kết quả thật trong project; không điền “pass” dựa trên đọc tài liệu.
