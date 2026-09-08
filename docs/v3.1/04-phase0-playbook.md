# 04 — Phase 0 Playbook (v3.1)

> Mục tiêu Phase 0: chứng minh hoặc bác bỏ 3 giả thuyết bằng dữ liệu, trong 6 tuần, không viết code sản phẩm.
> Sửa F03: file này biến "chưa có design partner" từ rủi ro mơ hồ thành checklist thi hành được.

## 1. Ba giả thuyết phải kiểm chứng

| # | Giả thuyết | Cách đo | Ngưỡng chấp nhận |
|---|---|---|---|
| H1 | Pain "không biết fail vì sao / flaky" đủ nặng và đủ tần suất | Pain ranking trong phỏng vấn | Top-2 pain của ≥ 8/15 người |
| H2 | TTRC hiện tại đủ lớn để đáng tiền | Đo baseline (§4) | Trung vị ≥ 20 phút/failure ở ≥ 2/3 partner |
| H3 | ICP kéo được ngân sách | Hỏi thẳng về ngân sách + quy trình duyệt (§3, câu B5–B7) | ≥ 5/15 nói được con số họ/sếp sẵn sàng trả |

## 2. Tuyển 15 người phỏng vấn

- **Screener (đủ cả 3 mới tính vào 15):** (1) đang trực tiếp maintain bộ Playwright/Cypress ≥ 50 test; (2) bộ test chạy ≥ 3 lần/tuần trên CI; (3) team ≥ 3 người đọc kết quả test.
- **Kênh (VN/SEA theo QĐ-3):** cộng đồng Testing VN (Facebook/Discord/Telegram QA), meetup, LinkedIn outreach cá nhân hóa, mạng lưới cựu đồng nghiệp/học viên. Mục tiêu nhịp: 5 booking/tuần từ tuần 1.
- **Không tính vào 15:** người chỉ làm manual testing (trừ khi kiểm chứng nhánh wedge B), agency demo, người không xem CI hàng tuần.
- Ghi chú mỗi buổi theo mẫu §3, lưu một nơi, review chéo cuối mỗi tuần.

## 3. Kịch bản phỏng vấn (45 phút, bán-cấu-trúc)

Nguyên tắc: hỏi về **quá khứ cụ thể**, không hỏi "anh/chị có muốn tính năng X không" (ai cũng nói muốn — dữ liệu rác).

**Phần A — Bối cảnh (5')**
- A1. Bộ test hiện tại: framework, số test, tần suất chạy, chạy ở đâu?
- A2. Ai xem kết quả? Bằng công cụ gì (Playwright HTML report, CI log, Allure, khác)?

**Phần B — Pain thật (25')**
- B1. "Kể tôi nghe lần **gần nhất** một test fail mà anh/chị phải điều tra. Chuyện gì xảy ra, từng bước?" *(đào: mở gì, hỏi ai, mất bao lâu, kết luận là gì)*
- B2. "Lần đó mất bao lâu từ lúc thấy fail đến lúc biết nguyên nhân?" *(số liệu cho baseline — ghi con số, kể cả ước lượng)*
- B3. "Tháng qua có test nào fail đi fail lại mà team kết luận 'chắc flaky, rerun đi' không? Bao nhiêu cái? Ai quyết định đó là flaky, dựa vào gì?"
- B4. "Có lần nào rerun cho qua rồi hóa ra là bug thật lọt ra production không?" *(đây là câu ăn tiền — nếu có, hỏi thiệt hại)*
- B5. "Team đang trả tiền cho công cụ QA/CI nào? Bao nhiêu/tháng?"
- B6. "Lần gần nhất team mua một tool mới: ai đề xuất, ai duyệt, mất bao lâu?"
- B7. Pain ranking: đưa 5 thẻ — (i) không biết fail vì sao, (ii) flaky làm mất niềm tin vào CI, (iii) quản lý manual + automation song song, (iv) viết test mới tốn công, (v) chạy test quá chậm — nhờ xếp hạng. *(thẻ (iii) chính là phép thử wedge B của QĐ-2)*

**Phần C — Phản chứng (10')**
- C1. "Playwright report + trace viewer đang thiếu gì với anh/chị?" *(nếu trả lời "đủ dùng" — đào tiếp: 'thế câu hỏi test-này-có-flaky-không anh/chị trả lời bằng gì?'; nếu vẫn đủ → ghi nhận, đây là dữ liệu H1 âm, không tranh cãi)*
- C2. "Nếu có công cụ trả lời được [pain #1 của họ], anh/chị hình dung nó phải làm được tối thiểu gì thì mới đáng cài thêm?"

**Phần D — Chốt (5')**
- D1. Mời làm design partner (đọc nguyên văn cam kết §5).
- D2. Xin giới thiệu 2 người cùng vai trò.

## 4. Đo baseline TTRC (điều kiện của Gate-0, dữ liệu đối chứng cho tuần 12)

**Định nghĩa đo (khớp 06 §3):** TTRC của một failure = thời điểm team chấp nhận một giả thuyết nguyên nhân − thời điểm failure xuất hiện trên CI.

Hai phương pháp, dùng cả hai cho mỗi partner:

1. **Hồi cứu (tuần đầu hợp tác):** cùng partner mở 10 failure gần nhất trong CI history + Slack/Jira, dựng lại timeline từng cái, ghi TTRC từng failure. Ưu: nhanh. Nhược: recall bias — ghi rõ nhãn "retrospective".
2. **Tiến cứu (2 tuần):** partner log mỗi failure điều tra vào một form 4 ô: `thời điểm thấy fail / thời điểm có kết luận / kết luận (category theo taxonomy 06 §7) / có rerun-cho-qua không`. Nhắc qua Slack 2 lần/tuần. Đây là số baseline chính thức.

**Output bắt buộc:** bảng mỗi partner: n failure, trung vị TTRC, p90 TTRC, % rerun-cho-qua. Không có bảng này = Gate-0 chưa đủ điều kiện pass, kể cả đã đủ 3 partner.

## 5. Mẫu cam kết Design Partner (1 trang, ký qua email là đủ)

> **Cam kết Design Partner — [Tên platform]**
> Bên A (platform) cam kết: (1) miễn phí sử dụng ≥ 12 tháng; (2) hỗ trợ trực tiếp qua kênh riêng, phản hồi < 1 ngày làm việc; (3) không sử dụng dữ liệu test của Bên B ngoài mục đích vận hành dịch vụ và cải thiện sản phẩm; không chia sẻ cho bên thứ ba; xóa toàn bộ theo yêu cầu trong 7 ngày; (4) ưu đãi giá sáng lập nếu chuyển sang trả phí.
> Bên B (design partner) cam kết: (1) tích hợp reporter vào ≥ 1 dự án thật chạy ≥ 3 lần/tuần; (2) call phản hồi 30'/tuần trong 12 tuần; (3) cho phép đo baseline TTRC theo §4; (4) phản hồi thẳng — chê được hoan nghênh hơn khen.
> Hai bên có quyền dừng bất cứ lúc nào bằng thông báo email; điều khoản dữ liệu (A.3) vẫn hiệu lực sau khi dừng.

## 6. Vận hành 6 tuần

| Tuần | Việc chính | Cờ đỏ cần phản ứng |
|---|---|---|
| 1 | Outreach đợt 1 (≥ 30 contact), 3–5 phỏng vấn đầu | < 3 booking → đổi kênh ngay, không đợi |
| 2 | 5 phỏng vấn, mời partner đầu tiên | Pain ranking phân tán không có pattern → xem lại screener |
| 3 | 5 phỏng vấn; partner #1 ký; bắt đầu đo baseline; **prototype identity** trên report thật | Không ai chịu ký dù khen → pain không đủ nặng, dữ liệu H1 âm |
| 4 | Phỏng vấn bù; partner #2; baseline chạy | |
| 5 | Partner #3; tổng hợp pain ranking | |
| 6 | Chốt bảng baseline; **họp Gate-0**, đối chiếu từng điều kiện ở 03 §2, ra biên bản pass/kill có chữ ký người giữ quyền kill | Bất kỳ điều kiện nào trượt → thi hành QĐ-6, không thương lượng "thêm 2 tuần nữa" quá 1 lần |

## 7. Anti-pattern bị cấm trong Phase 0

- Viết code sản phẩm "tranh thủ" (ngoại lệ duy nhất: prototype identity tuần 3–4).
- Demo mockup rồi hỏi "anh/chị có dùng không" và tính câu trả lời "có" là validation.
- Tính bạn bè nể nang vào 3 design partner — partner phải qua screener §2 như mọi người.
- Đổi câu hỏi phỏng vấn giữa chừng khiến 15 mẫu không so được với nhau (được phép *thêm* câu, không được sửa/bỏ câu B1–B7).
