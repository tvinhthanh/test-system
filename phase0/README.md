# Phase 0 — Bộ công cụ thi hành

> Bắt đầu **26-08-2026** · Gate-0 **06-10-2026** · Người giữ quyền kill gate: `tvinhthanhsg@gmail.com`
> Kế hoạch: `SPEC.md` §5 (playbook) và §5.8 (lịch ngày thật). File ở đây là công cụ, không phải kế hoạch.

## Mục tiêu duy nhất của 6 tuần này

Chứng minh **hoặc bác bỏ** ba giả thuyết bằng dữ liệu. Không viết code sản phẩm.

| | Giả thuyết | Ngưỡng pass |
|---|---|---|
| **H1** | Pain "không biết fail vì sao / flaky" đủ nặng | Top-2 của ≥ 8/15 người |
| **H2** | Thời gian tìm nguyên nhân hiện tại đủ lớn để đáng tiền | Trung vị ≥ 20 phút ở ≥ 2/3 partner |
| **H3** | ICP kéo được ngân sách | ≥ 5/15 nói được con số/ngưỡng duyệt được |

**Bác bỏ cũng là kết quả tốt.** Biết sớm rẻ hơn nhiều so với build 6 tháng rồi mới biết.


## Tình trạng — cập nhật 08-09-2026 (T3, ngày 14/42, hết tuần 2)

> Số dưới đây **không đổi kể từ 30-08**. Không điền hộ: dữ liệu Phase 0 phải đến từ người thật. Điền hộ là chế ra đúng thứ bằng chứng mà Gate-0 tồn tại để cân (§5.7).

| | |
|---|---|
| Contact đã ghi vào `01-outreach-tracker.md` | **0 / 30** |
| Ghi chú phỏng vấn trong `02-interview-notes/` | **0 / 15** |
| Partner đã ký | **0 / 3** |
| Baseline tiến cứu đang chạy | **0 / 3** |
| Còn tới hạn chót partner #3 (22-09) | **14 ngày** |
| Còn tới Gate-0 (06-10) | **28 ngày** |

### Hai mốc đã trượt

| Mốc | Yêu cầu | Thực tế | §5.6 nói phải làm gì |
|---|---|---|---|
| **01-09** hết tuần 1 | ≥ 3 booking | 0 | **Đổi kênh ngay.** Và ghi lại kênh cũ đã thử với tỉ lệ bao nhiêu — bảng đó là đầu vào Gate-0 dù kết quả ra sao |
| **08-09** hết tuần 2 | 5 phỏng vấn · partner #1 ký · baseline #1 chạy | 0 · chưa · chưa | Không ai chịu ký dù pain cao → dữ liệu H1 âm. Nhưng chưa gửi outreach thì **chưa có dữ liệu gì cả**, kể cả âm |

### Số học còn lại — đọc kỹ trước khi quyết gia hạn

Gate-0 điều kiện #3 cần baseline tiến cứu **đủ 14 ngày cho cả 3 partner**. Ngày ký muộn nhất còn hợp lệ là **22-09** (22-09 → 05-10, vừa kịp 06-10). Từ hôm nay còn **14 ngày** để đi từ 0 contact tới 3 partner đã ký.

Phễu theo §5.2 là 30 contact → 15 phỏng vấn → 3 partner. Nén phễu đó vào 14 ngày là việc rất nặng, và **không được nén bằng cách bỏ screener** — §5.7 cấm tường minh, mà partner không qua screener thì baseline của họ không dùng được cho H2.

Ba đường còn lại, phải chọn một và ghi vào `06-gate0-decision-record.md`:

1. **Chạy nước rút 14 ngày** — dồn vào kênh 3 (cựu đồng nghiệp) và kênh 5 (giới thiệu) vì tỉ lệ nhận lời cao nhất; chấp nhận số phỏng vấn < 15 và ghi rõ điều đó ở Gate-0.
2. **Gia hạn một lần** — §5.6 cho phép đúng một lần, phải do `tvinhthanhsg@gmail.com` ký và ghi lý do vào biên bản. Lần thứ hai không được phép.
3. **Thừa nhận Phase 0 chưa khởi động thật** — dời ngày bắt đầu, viết lại §5.8 theo ngày mới. Trung thực hơn là giữ một lịch mà mọi mốc đều đã trượt.

Điều **không** được làm: để lịch trôi tới 06-10 rồi họp Gate-0 với bảng trống. Gate-0 không có dữ liệu thì không phải PASS cũng không phải KILL — nó là chưa họp.

## Việc phải làm ngay (tuần 3: 09-09 → 15-09)

- [ ] Lập danh sách **≥ 30 contact** vào `01-outreach-tracker.md` — việc này không cần ai trả lời, làm được ngay hôm nay
- [ ] Chọn tin nhắn theo kênh trong `07-outreach-messages.md`; ưu tiên **kênh 3** và **kênh 5** vì chỉ còn 14 ngày
- [ ] Gửi outreach đợt 1 và **ghi ngay vào tracker** — kể cả khi chưa ai trả lời, tỉ lệ theo kênh là dữ liệu Gate-0
- [ ] Chạy phỏng vấn đầu tiên, ghi vào `02-interview-notes/`
- [ ] Chuẩn bị `03-pain-ranking-cards.md` và `08-email-gui-sep.md` trước buổi đầu tiên
- [ ] Quyết một trong ba đường ở trên, ghi vào biên bản Gate-0

> Tuần 3 theo §5.8 còn có mục *"prototype identity chạy trên ≥ 500 test result thật"*. Prototype đã xong, test pass (`prototype/identity/` — precision 1,0000 · recall 0,9920, trích §9.6) nhưng vẫn trên corpus tổng hợp. Mục này **bị chặn vì chưa có partner**, không phải vì code.

## Các file

| File | Dùng khi |
|---|---|
| `01-outreach-tracker.md` | Từ hôm nay. Theo dõi 30 contact → 15 phỏng vấn |
| `07-outreach-messages.md` | **Tin nhắn theo từng kênh** + cách sàng lọc trước khi đặt lịch + cách đọc phản hồi |
| `02-interview-notes-template.md` | Copy một bản cho mỗi buổi phỏng vấn |
| `03-pain-ranking-cards.md` | Mang theo mỗi buổi — câu B7 |
| `04-design-partner-email.md` | Khi mời partner (câu D1) |
| `05-baseline-ttrc-log.md` | Ngay khi partner ký. **Cần 14 ngày liên tục** |
| `06-gate0-decision-record.md` | Ngày 06-10-2026 |
| `08-email-gui-sep.md` | Mang theo mỗi buổi — câu D3. Deliverable §2.5, dùng để **đo** H3 chứ không phải để bán |

## Ba mốc không thương lượng

| Ngày | Việc | Nếu trễ |
|---|---|---|
| **01-09** | ≥ 3 booking | Đổi kênh outreach |
| **22-09** | Partner #3 phải ký | Baseline không đủ 14 ngày → Gate-0 **toán học không thể pass** |
| **06-10** | Họp Gate-0 | Thi hành QĐ-6 |

## Bốn điều bị cấm (SPEC §5.7)

1. Viết code sản phẩm "tranh thủ" — ngoại lệ duy nhất: prototype test identity tuần 3.
2. Demo mockup rồi hỏi "anh/chị có dùng không" và tính câu trả lời "có" là validation.
3. Tính bạn bè nể nang vào 3 design partner — phải qua screener như mọi người.
4. Sửa hoặc bỏ câu B1–B7 giữa chừng. Được phép **thêm** câu, không được đổi.

## Screener — ai được tính vào 15

Đủ **cả ba** mới tính:

1. Đang trực tiếp maintain bộ Playwright/Cypress ≥ 50 test
2. Bộ test chạy ≥ 3 lần/tuần trên CI
3. Team ≥ 3 người đọc kết quả test

Không tính: người chỉ làm manual testing, agency demo, người không xem CI hàng tuần.
