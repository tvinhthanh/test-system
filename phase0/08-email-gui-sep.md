# Mẫu email "gửi sếp của bạn"

> **Deliverable Phase 0** — SPEC §2.5 gọi đích danh, không phải afterthought.
> Lý do tồn tại: ICP (QA Automation Lead / SDET) **không phải** người giữ ngân sách. Người duyệt là EM / Head of Engineering. Sản phẩm bán được hay không phụ thuộc vào việc ICP có **cầm được lập luận** đi xin tiền hay không.

## ĐỌC TRƯỚC: ở Phase 0 đây là dụng cụ đo, không phải tài liệu bán hàng

Chưa có sản phẩm. Không được gửi email này cho sếp của ai trong 6 tuần Phase 0.

Cách dùng đúng: đưa bản in ở câu **D3**, sau khi đã hỏi xong B5/B6, rồi nói:

> *"Giả sử có công cụ làm được điều anh/chị vừa nói. Nếu anh/chị phải xin ngân sách cho nó, email gửi sếp sẽ trông thế nào? Anh/chị điền thử mấy ô này được không ạ?"*

**Thứ cần đo không phải câu trả lời "có mua không". Thứ cần đo là họ có điền nổi các ô trống không.** Người đang đau thật điền được số trong vòng một phút vì họ đã từng nhẩm rồi. Người trả lời cho lịch sự sẽ ngập ngừng ở ô đầu tiên — và đó là dữ liệu H3 âm, có giá trị ngang dữ liệu dương (§5.7 cấm tính câu "có" thành validation).

---

## Mẫu email

**Tiêu đề:** Đề xuất công cụ theo dõi kết quả test E2E — $____/tháng

Chào anh/chị [tên sếp],

Em muốn đề xuất team mua [tên công cụ], khoảng **$____/tháng**.

**Vấn đề đang tốn tiền của mình:**

Tháng vừa rồi team có **____ lần** test E2E fail phải điều tra. Trung bình mỗi lần mất **____ phút** để biết đó là bug thật, test hỏng hay lỗi môi trường. Tổng cộng khoảng **____ giờ/tháng**, tương đương **$____/tháng** tính theo chi phí giờ kỹ sư.

Phần lớn thời gian đó không phải sửa lỗi — mà là **tìm xem có lỗi thật không**: mở CI log, so với lần chạy trước, hỏi trong Slack xem ai từng gặp, kiểm tra xem test này có hay fail vô cớ không.

[*Nếu có — đây là đoạn nặng nhất, đừng bỏ:*]
Ngoài ra, ngày **____** team rerun cho qua một test fail và kết luận là flaky. Hóa ra là bug thật, lọt lên production. Thiệt hại: **____**.

**Công cụ này giải quyết cái gì:**

Nó giữ lịch sử từng test qua các lần chạy, nên trả lời được ba câu mà Playwright report hiện tại không trả lời được:

1. Test này pass hôm nay, nhưng đã fail 7 lần trong 30 ngày qua — flaky hay vừa mới hỏng thật?
2. Lỗi này đã từng xuất hiện chưa, lần trước ai xử lý, kết luận là gì?
3. Cả team đang nhìn chung bức tranh nào về sức khỏe bộ test?

**Chi phí so sánh:**

| | |
|---|---|
| Chi phí hiện tại (thời gian điều tra) | **$____/tháng** |
| Giá công cụ | **$____/tháng** |
| Team đang trả cho [tool hiện tại] | $____/tháng |

Em không đề xuất thay thế gì đang chạy — test vẫn chạy trên CI như cũ, chỉ thêm một dòng vào `playwright.config.ts`.

**Đề nghị:** cho em chạy thử ____ tuần trên [tên dự án]. Nếu sau đó thời gian điều tra không giảm đo được, mình bỏ.

Em cảm ơn anh/chị.

---

## Cách tính các ô trống

Số phải lấy từ chính team họ, **không lấy từ mình**. Ba số đầu đã có sẵn trong buổi phỏng vấn:

| Ô | Lấy ở đâu | Ghi chú |
|---|---|---|
| Số lần fail phải điều tra/tháng | **B3** + CI history của họ | Không tính lần rerun là qua ngay mà không ai nhìn |
| Phút/lần | **B2** — trung vị, không phải ca tệ nhất | Ca tệ nhất làm lập luận yếu đi khi sếp hỏi lại |
| Chi phí giờ kỹ sư | Họ tự biết, đừng đoán hộ | Blended cost, không phải lương gross |
| Giá công cụ | §10.5 — Team **$49/th**, Business **$199/th** | Founding partner −50%, SEA −40%, **không cộng dồn** |
| Tool hiện tại | **B5** | Đây là anchor mạnh nhất vì sếp đã duyệt nó rồi |

**Công thức:**

```
giờ/tháng  = (số lần fail × phút mỗi lần) / 60
$/tháng    = giờ/tháng × chi phí giờ kỹ sư
```

**Phép thử §2.5 phải qua:** con số `$/tháng` nên vào khoảng **10× giá gói**. Nếu ra thấp hơn 3× thì lập luận không đứng được — và đó là phát hiện thật, cần ghi vào ghi chú phỏng vấn, không phải chỗ để chỉnh số cho đẹp.

> Ví dụ nhẩm được: 25 failure/tháng × 20 phút = 8,3 giờ. Ở mức $25/giờ ≈ **$208/tháng**, so với gói Team $49 là hơn 4×. Đây là ví dụ minh họa cách nhẩm, **không phải số của bất kỳ team nào** — mọi con số trong email phải là số họ tự nói ra.

## Anchor bên ngoài (§10.5)

TestRail Professional **$41/user/tháng**, 10 user ≈ **$410/tháng** (thông báo giá 01-08-2026). Dùng khi sếp phản xạ *"$49 có đắt không"* — nhưng phải **re-verify trước mỗi lần dùng**, §10.5 cấm hardcode anchor này lâu dài.

## Quan sát gì ở D3 — đây mới là phần thu dữ liệu

| Quan sát được | Nghĩa | Tính vào H3? |
|---|---|---|
| Điền được cả 3 số đầu, không cần tra | Đã từng nhẩm rồi → pain thật, có ý thức chi phí | ✅ **Có** |
| Điền được, phải mở CI ra đếm | Vẫn tốt — số có thật | ✅ Có |
| Nói được **ai duyệt** và **ngưỡng tự quẹt** | Đúng yêu cầu §10.6 | ✅ Có |
| Ngập ngừng ở ô đầu, hỏi ngược "khoảng bao nhiêu thì hợp lý" | Chưa đau đủ, hoặc đang đoán ý mình | ❌ **Không** |
| "Chắc sếp em duyệt thôi" mà không nói được quy trình | Câu xã giao, §10.6 cấm tính | ❌ Không |
| Nói thẳng "cái này không đáng tiền" | **Dữ liệu H3 âm — ghi nguyên văn** | ❌ Không, nhưng có giá trị |

Ghi kết quả vào ô **D3** của `02-interview-notes-template.md`. Ngưỡng Gate-0 điều kiện #6: **≥ 5/15** nói được con số ngân sách hoặc ngưỡng duyệt được, **kèm quy trình ai duyệt**.

## Bốn điều cấm

1. **Không gửi email này cho sếp của ai trong Phase 0.** Chưa có sản phẩm để chạy thử — lời hứa "chạy thử ____ tuần" hiện chưa giữ được.
2. **Không điền số hộ họ.** Số mình đưa ra đo được thiện chí của họ, không đo được pain.
3. **Không dùng ca tệ nhất thay cho trung vị.** Sếp sẽ hỏi lại, và lập luận sập ngay tại chỗ — B2 đã ghi trung vị, dùng đúng nó.
4. **Không hứa % giảm TTRC.** Chưa có baseline của chính họ thì mọi con số cải thiện đều là bịa. Baseline là việc của `05-baseline-ttrc-log.md`, và nó phải chạy đủ 14 ngày trước.
