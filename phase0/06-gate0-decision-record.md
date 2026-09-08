# Biên bản Gate-0

> **Họp ngày 06-10-2026.** Người có quyền tuyên bố: `tvinhthanhsg@gmail.com` (§1.6, ký 26-08-2026).
> Điền TRƯỚC buổi họp, không điền trong lúc họp. Số liệu phải lấy từ `01-outreach-tracker.md` và `05-baseline-ttrc-log.md`, không lấy từ trí nhớ.

## Sáu điều kiện PASS — phải đủ CẢ SÁU (SPEC §4.5)

| # | Điều kiện | Số thật | Đạt? |
|---|---|---|---|
| 1 | ≥ 15 phỏng vấn ICP hoàn thành, có ghi chú theo mẫu | ___ / 15 | ☐ |
| 2 | 3 design partner ký cam kết, có bộ E2E thật chạy ≥ 3 lần/tuần | ___ / 3 | ☐ |
| 3 | Baseline TTRC **tiến cứu đủ 14 ngày cho cả 3 partner**, có n / median / p90 / % rerun | ☐ đủ ☐ thiếu | ☐ |
| 4 | **H1** — pain "không biết fail vì sao / flaky" trong top-2 của ≥ 8/15 | ___ / 15 | ☐ |
| 5 | **H2** — median baseline TTRC ≥ 20 phút ở ≥ 2/3 partner | ___ / 3 | ☐ |
| 6 | **H3** — ≥ 5/15 nói được con số ngân sách hoặc ngưỡng giá duyệt được, kèm quy trình ai duyệt | ___ / 15 | ☐ |

## Nếu có điều kiện trượt — nhánh nào?

| Trượt điều | Hành động bắt buộc |
|---|---|
| **#2** (không đủ 3 partner) | Thi hành **QĐ-6**: dừng. Hai lựa chọn — đổi wedge, hoặc đóng dự án |
| **#4** (H1) *nhưng* thẻ (iii) là top-1 của ≥ 8/15 | **Lật wedge** sang manual↔automation (QĐ-2 phương án B). Viết lại §2 và §4 trong 1 tuần, chạy lại Gate-0 rút gọn |
| **#5** (H2) | Không build wedge "giảm TTRC". Partner đồng ý vì lịch sự không phải bằng chứng |
| **#6** (H3) | Chưa có bằng chứng thương mại → **không mở P1-full**. Chỉ được làm P1-min/prototype nếu người ký ghi rõ lý do và giới hạn ngân sách |

## Quyết định

☐ **PASS** — chuyển sang Phase 1
☐ **PIVOT** — lật wedge, viết lại §2/§4, chạy lại Gate-0 rút gọn
☐ **KILL** — dừng dự án

**Nếu PASS, chọn phạm vi ngay tại đây** (SPEC §13 mục 6, §4.4):

☐ **P1-min** — 4–6 người-tháng, ~12 tuần với 2 người full-time
☐ **P1-full** — 6,5–11,5 người-tháng, 14–25 tuần với 2 người full-time

Số FTE thực tế: ______  Ngân sách: ______

## Lý do quyết định

*Viết bằng số, không bằng cảm nhận. Nếu phải dùng chữ "cảm thấy" thì chưa đủ dữ liệu để quyết.*

```

```

## Ba điều học được mà trước Phase 0 không biết

```
1.
2.
3.
```

## Giả định nào bị bác bỏ?

*Mục này quan trọng hơn phần PASS. Nếu không có giả định nào bị bác bỏ sau 15 phỏng vấn, khả năng cao là đã hỏi dẫn dắt.*

```

```

---

## Ký

| Vai trò | Tên | Quyết định | Ngày |
|---|---|---|---|
| Người giữ quyền kill gate | `tvinhthanhsg@gmail.com` | | |

## Quy tắc gia hạn

Gia hạn **tối đa một lần**, phải do chính người trên ký, và phải ghi lý do vào đây:

```
Gia hạn tới ngày: ______   Lý do:

```

> "Thêm 2 tuần nữa" lần thứ hai **không được phép** (SPEC §5.6). Quyết định tập thể về việc dừng là quyết định không bao giờ được ra — đó là lý do ô ký này chỉ có một tên.
