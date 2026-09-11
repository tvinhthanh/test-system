# Biên bản Gate-0

> **Họp ngày 26-10-2026.** Người có quyền tuyên bố: `tvinhthanhsg@gmail.com` (§1.6, ký 26-08-2026).
> Điền TRƯỚC buổi họp, không điền trong lúc họp. Số liệu phải lấy từ `01-outreach-tracker.md` và `05-baseline-ttrc-log.md`, không lấy từ trí nhớ.

## Quyết định trước Gate-0 — ghi ngày 11-09-2026

Lần chốt đầu ghi Phase 0 bắt đầu 26-08-2026, Gate-0 06-10-2026. Đến 11-09 (ngày 17/42): tracker 0/30, phỏng vấn 0/15, partner 0/3. Hai mốc 01-09 và 08-09 trượt vì chưa gửi outreach nào.

`README.md` của thư mục này đặt ra ba đường. Người giữ quyền kill gate chọn:

| Đường | Chọn? |
|---|---|
| A. Nước rút 11 ngày, giữ lịch cũ | ☐ |
| B. Dùng quyền gia hạn một lần của §5.6 | ☐ |
| **C. Thừa nhận Phase 0 chưa khởi động; chốt lại ngày bắt đầu** | **☑** |

**Ngày bắt đầu mới: 15-09-2026. Gate-0 mới: 26-10-2026.** §5.8 và mọi file trong `phase0/` đã viết lại theo ngày này.

**Vì sao C mà không phải B:** quyền gia hạn §5.6 là để kéo dài một Phase 0 *đang chạy* mà thiếu thời gian. Ở đây chưa có gì chạy — dùng quyền gia hạn duy nhất để che một lần chưa bắt đầu là tiêu mất lưới an toàn trước khi cần. Quyền đó **vẫn còn nguyên** sau quyết định này.

**Điều kiện kèm theo:** ngày 15-09 chỉ có hiệu lực khi đợt outreach đầu đã gửi và đã ghi vào `01-outreach-tracker.md` trong ngày. Không có lần chốt thứ ba.

| Vai trò | Tên | Quyết định | Ngày |
|---|---|---|---|
| Người giữ quyền kill gate | `tvinhthanhsg@gmail.com` | Đường C | 11-09-2026 |

---

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
