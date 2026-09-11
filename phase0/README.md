# Phase 0 — Bộ công cụ thi hành

> Bắt đầu **15-09-2026** (chốt lại ngày 11-09; lần chốt 26-08 không diễn ra) · Gate-0 **26-10-2026** · Người giữ quyền kill gate: `tvinhthanhsg@gmail.com`
> Kế hoạch: `SPEC.md` §5 (playbook) và §5.8 (lịch ngày thật). File ở đây là công cụ, không phải kế hoạch.

## Mục tiêu duy nhất của 6 tuần này

Chứng minh **hoặc bác bỏ** ba giả thuyết bằng dữ liệu. Không viết code sản phẩm.

| | Giả thuyết | Ngưỡng pass |
|---|---|---|
| **H1** | Pain "không biết fail vì sao / flaky" đủ nặng | Top-2 của ≥ 8/15 người |
| **H2** | Thời gian tìm nguyên nhân hiện tại đủ lớn để đáng tiền | Trung vị ≥ 20 phút ở ≥ 2/3 partner |
| **H3** | ICP kéo được ngân sách | ≥ 5/15 nói được con số/ngưỡng duyệt được |

**Bác bỏ cũng là kết quả tốt.** Biết sớm rẻ hơn nhiều so với build 6 tháng rồi mới biết.


## Tình trạng — cập nhật 11-09-2026 (tuần 0, còn 4 ngày tới ngày bắt đầu)

> Không điền hộ: dữ liệu Phase 0 phải đến từ người thật. Điền hộ là chế ra đúng thứ bằng chứng mà Gate-0 tồn tại để cân (§5.7).

| | |
|---|---|
| Contact đã ghi vào `01-outreach-tracker.md` | **0 / 30** |
| Ghi chú phỏng vấn trong `02-interview-notes/` | **0 / 15** |
| Partner đã ký | **0 / 3** |
| Baseline tiến cứu đang chạy | **0 / 3** |
| Còn tới ngày bắt đầu (15-09) | **4 ngày** |
| Còn tới hạn chót partner #3 (12-10) | **31 ngày** |
| Còn tới Gate-0 (26-10) | **45 ngày** |

### Vì sao chốt lại ngày

Lần chốt đầu ghi bắt đầu 26-08. Đến 11-09 — ngày 17/42 — mọi ô trên vẫn bằng 0. Hai mốc 01-09 và 08-09 trượt không phải vì kết quả xấu mà vì **chưa gửi gì để có kết quả**. Trong ba đường README này từng liệt kê, chủ dự án chọn **đường C ngày 11-09**: thừa nhận Phase 0 chưa khởi động, chốt lại ngày, viết lại §5.8. Ghi ở `06-gate0-decision-record.md`.

**Đây không dùng quyền gia hạn một lần** của §5.6 — gia hạn là kéo dài một Phase 0 đang chạy, còn đây là ghi nhận nó chưa chạy. Quyền đó vẫn còn nguyên, và vẫn chỉ có một.

### Điều kiện để 15-09 thật sự là ngày bắt đầu

Ngày bắt đầu chỉ có hiệu lực khi **đợt outreach đầu tiên đã gửi và đã ghi vào tracker trong ngày 15-09**. Hết 15-09 mà tracker vẫn 0 → chưa bắt đầu, không đếm ngày, và **không chốt lần ba** trước khi trả lời được câu hỏi vì sao lần hai cũng không gửi. Lịch mà không có hành động thì chỉ là một cách khác để trì hoãn.

## Việc phải làm ngay (tuần 0: 11-09 → 14-09)

Tuần 0 chỉ có **một** việc, và nó không cần ai trả lời:

- [ ] Lập danh sách **≥ 30 contact** vào `01-outreach-tracker.md` — tên, công ty, vai trò, kênh. Nguồn: LinkedIn, danh bạ cựu đồng nghiệp, học viên cũ, nhóm Testing VN
- [ ] Với mỗi contact ở kênh LinkedIn (kênh 2), ghi sẵn **một chi tiết cụ thể về họ** — `07-outreach-messages.md` cấm gửi khuôn mẫu; không có chi tiết thì bỏ contact đó
- [ ] Chọn trước tin nhắn theo kênh cho từng người trong `07-outreach-messages.md`

Rồi ngày **15-09**, tuần 1 bắt đầu:

- [ ] Gửi đợt 1 và **ghi ngay vào tracker** — kể cả khi chưa ai trả lời, tỉ lệ theo kênh là dữ liệu Gate-0
- [ ] Chạy phỏng vấn đầu tiên khi có booking, ghi vào `02-interview-notes/`
- [ ] Chuẩn bị `03-pain-ranking-cards.md` và `08-email-gui-sep.md` trước buổi đầu tiên
- [ ] **21-09:** đủ 3 booking chưa? Chưa → đổi kênh, ghi lại kênh cũ với tỉ lệ

> Mục *"prototype identity chạy trên ≥ 500 test result thật"* ở tuần 3 (§5.8): prototype đã xong, 40 test pass (`prototype/identity/` — precision 1,0000 · recall 0,9920, trích §9.6) nhưng vẫn trên corpus tổng hợp. Mục này **bị chặn vì chưa có partner**, không phải vì code.

## Các file

| File | Dùng khi |
|---|---|
| `01-outreach-tracker.md` | Từ hôm nay. Theo dõi 30 contact → 15 phỏng vấn |
| `07-outreach-messages.md` | **Tin nhắn theo từng kênh** + cách sàng lọc trước khi đặt lịch + cách đọc phản hồi |
| `02-interview-notes-template.md` | Copy một bản cho mỗi buổi phỏng vấn |
| `03-pain-ranking-cards.md` | Mang theo mỗi buổi — câu B7 |
| `04-design-partner-email.md` | Khi mời partner (câu D1) |
| `05-baseline-ttrc-log.md` | Ngay khi partner ký. **Cần 14 ngày liên tục** |
| `06-gate0-decision-record.md` | Ngày 26-10-2026 — và đã có mục "quyết định trước Gate-0" ghi ngày 11-09 |
| `08-email-gui-sep.md` | Mang theo mỗi buổi — câu D3. Deliverable §2.5, dùng để **đo** H3 chứ không phải để bán |

## Bốn mốc không thương lượng

| Ngày | Việc | Nếu trễ |
|---|---|---|
| **15-09** | Đợt outreach đầu đã gửi, đã ghi tracker | Ngày bắt đầu chưa xảy ra — không đếm ngày |
| **21-09** | ≥ 3 booking | Đổi kênh outreach |
| **12-10** | Partner #3 phải ký | Baseline không đủ 14 ngày → Gate-0 **toán học không thể pass** |
| **26-10** | Họp Gate-0 | Thi hành QĐ-6 |

## Bốn điều bị cấm (SPEC §5.7)

1. ~~Viết code sản phẩm "tranh thủ"~~ — **QĐ-11 (11-09) lật luật này có điều kiện:** `backend/` chỉ được đụng vào trong ngày **đã gửi outreach và đã ghi tracker**. Cột `Code?` trong `07-outreach-messages.md`. Chưa gửi → chưa code, không ngoại lệ.
2. Demo mockup rồi hỏi "anh/chị có dùng không" và tính câu trả lời "có" là validation.
3. Tính bạn bè nể nang vào 3 design partner — phải qua screener như mọi người.
4. Sửa hoặc bỏ câu B1–B7 giữa chừng. Được phép **thêm** câu, không được đổi.

## Screener — ai được tính vào 15

Đủ **cả ba** mới tính:

1. Đang trực tiếp maintain bộ Playwright/Cypress ≥ 50 test
2. Bộ test chạy ≥ 3 lần/tuần trên CI
3. Team ≥ 3 người đọc kết quả test

Không tính: người chỉ làm manual testing, agency demo, người không xem CI hàng tuần.
