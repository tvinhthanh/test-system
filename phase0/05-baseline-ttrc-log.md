# Baseline TTRC — sổ đo

> **Điều kiện Gate-0 #3.** Không có bảng tổng kết ở cuối file này thì Gate-0 **không đủ điều kiện pass**, kể cả đã đủ 3 partner.
> Định nghĩa (SPEC §5.4): thời gian từ lúc failure **được CI báo cho team** đến lúc team **chấp nhận một giả thuyết nguyên nhân**.

## Hai phương pháp — dùng CẢ HAI cho mỗi partner

### 1. Hồi cứu — tuần đầu hợp tác, làm cùng partner

Mở 10 failure gần nhất trong CI history + Slack/Jira, dựng lại timeline từng cái.
Nhanh, nhưng **có recall bias** — luôn gắn nhãn `retrospective`, không trộn vào số chính thức.

### 2. Tiến cứu — 14 ngày liên tục, ĐÂY LÀ SỐ CHÍNH THỨC

Partner tự log mỗi failure họ điều tra. Nhắc qua Slack **2 lần/tuần** — không nhắc thì dữ liệu sẽ thủng.

> ⚠️ **14 ngày là ràng buộc cứng.** Partner #3 ký muộn nhất 12-10 để baseline chạy 12-10 → 25-10, vừa kịp Gate-0 ngày 26-10.

---

## Partner #1 — tên: ______________  ngày ký: ______  baseline từ ______ đến ______

| # | Thời điểm thấy fail | Thời điểm có kết luận | TTRC (phút) | Kết luận (category) | Rerun cho qua? | Nhãn |
|---|---|---|---|---|---|---|
| 1 | | | | | ☐ | |
| 2 | | | | | ☐ | |
| 3 | | | | | ☐ | |
| 4 | | | | | ☐ | |
| 5 | | | | | ☐ | |
| 6 | | | | | ☐ | |
| 7 | | | | | ☐ | |
| 8 | | | | | ☐ | |
| 9 | | | | | ☐ | |
| 10 | | | | | ☐ | |

## Partner #2 — tên: ______________  ngày ký: ______  baseline từ ______ đến ______

| # | Thời điểm thấy fail | Thời điểm có kết luận | TTRC (phút) | Kết luận (category) | Rerun cho qua? | Nhãn |
|---|---|---|---|---|---|---|
| 1 | | | | | ☐ | |
| 2 | | | | | ☐ | |
| 3 | | | | | ☐ | |
| 4 | | | | | ☐ | |
| 5 | | | | | ☐ | |

## Partner #3 — tên: ______________  ngày ký: ______  baseline từ ______ đến ______

| # | Thời điểm thấy fail | Thời điểm có kết luận | TTRC (phút) | Kết luận (category) | Rerun cho qua? | Nhãn |
|---|---|---|---|---|---|---|
| 1 | | | | | ☐ | |
| 2 | | | | | ☐ | |
| 3 | | | | | ☐ | |
| 4 | | | | | ☐ | |
| 5 | | | | | ☐ | |

---

## Category — dùng đúng 7 giá trị này (SPEC §8.7)

Đây là taxonomy sẽ dùng suốt đời sản phẩm. Dùng sai từ Phase 0 thì baseline không so được với số sau này.

| Code | Nghĩa |
|---|---|
| `app_bug` | Bug thật của ứng dụng |
| `test_bug` | Lỗi trong code test (selector sai, assertion sai) |
| `flaky_timing` | Race/timing — fail ngẫu nhiên ở wait, pass khi retry |
| `env_infra` | Môi trường/hạ tầng — DNS, service phụ thuộc down |
| `env_data` | Dữ liệu test — fixture hết hạn, tài khoản bị khóa |
| `dependency_external` | Bên thứ ba — sandbox payment, API ngoài 5xx |
| `unknown` | Chưa xác định |

---

## BẢNG TỔNG KẾT — bắt buộc có trước Gate-0

| Partner | n failure | **Trung vị TTRC** | p90 TTRC | % rerun-cho-qua | Đủ 14 ngày? |
|---|---|---|---|---|---|
| #1 | | | | | ☐ |
| #2 | | | | | ☐ |
| #3 | | | | | ☐ |

**Kiểm tra H2** *(Gate-0 điều kiện #5)*: trung vị ≥ 20 phút ở **≥ 2/3 partner**?

→ ☐ Đạt  ☐ **Không đạt** → không được build wedge "giảm TTRC" chỉ vì partner lịch sự đồng ý

---

## Ba lỗi làm hỏng baseline — tránh từ đầu

1. **Trộn hồi cứu vào số chính thức.** Recall bias làm số đẹp lên. Luôn tách nhãn.
2. **Chỉ log failure khó.** Nếu partner chỉ ghi những ca mất 2 tiếng mà bỏ qua ca 5 phút, trung vị bị thổi lên và sau này sản phẩm "cải thiện" một con số vốn đã sai. Nhắc partner log **mọi** failure họ điều tra.
3. **Để partner nhớ lại cuối tuần.** Log ngay lúc xử lý. Nhắc 2 lần/tuần chính là để chống việc này.
