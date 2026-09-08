# Ghi chú phỏng vấn — #___

> Copy file này cho mỗi buổi. Đặt tên `02-interview-notes/NN-tên-công-ty.md`.
> **Không sửa, không bỏ câu B1–B7.** Được phép thêm câu ở cuối. Đổi câu giữa chừng làm 15 mẫu không so được với nhau.

| | |
|---|---|
| Số thứ tự | #___ / 15 |
| Ngày | |
| Người được phỏng vấn | |
| Vai trò | |
| Công ty / quy mô team | |
| Kênh tiếp cận | |

**Screener — phải đủ cả 3 mới tính vào 15:**

- [ ] Trực tiếp maintain bộ Playwright/Cypress ≥ 50 test
- [ ] Bộ test chạy ≥ 3 lần/tuần trên CI
- [ ] Team ≥ 3 người đọc kết quả test

→ Đủ điều kiện: ☐ Có ☐ Không *(không đủ thì vẫn ghi chú được, nhưng **không tính vào 15**)*

---

## A — Bối cảnh (5')

**A1.** Bộ test hiện tại: framework, số test, tần suất chạy, chạy ở đâu?

```

```

**A2.** Ai xem kết quả? Bằng công cụ gì?

```

```

---

## B — Pain thật (25')

> Nguyên tắc: hỏi về **quá khứ cụ thể**. Không hỏi "anh/chị có muốn tính năng X không".

**B1.** *"Kể tôi nghe lần **gần nhất** một test fail mà anh/chị phải điều tra. Chuyện gì xảy ra, từng bước?"*
*Đào: mở gì → hỏi ai → mất bao lâu → kết luận là gì*

```

```

**B2.** *"Lần đó mất bao lâu từ lúc thấy fail đến lúc biết nguyên nhân?"*

> Con số: **______ phút** *(ghi cả khi là ước lượng — đây là dữ liệu H2)*

**B3.** *"Tháng qua có test nào fail đi fail lại mà team kết luận 'chắc flaky, rerun đi' không? Bao nhiêu cái? Ai quyết định đó là flaky, dựa vào gì?"*

```

```

**B4.** *"Có lần nào rerun cho qua rồi hóa ra là bug thật lọt ra production không?"*
**Câu ăn tiền.** Nếu có → hỏi thiệt hại cụ thể.

```

```

**B5.** *"Team đang trả tiền cho công cụ QA/CI nào? Bao nhiêu/tháng?"*

```

```

**B6.** *"Lần gần nhất team mua một tool mới: ai đề xuất, ai duyệt, mất bao lâu?"*

```

```

**B7. Pain ranking** — đưa 5 thẻ (`03-pain-ranking-cards.md`), nhờ xếp hạng. Ghi thứ tự họ xếp:

| Hạng | Thẻ |
|---|---|
| 1 | |
| 2 | |
| 3 | |
| 4 | |
| 5 | |

> Thẻ (i) và (ii) trong top-2 → dữ liệu **H1 dương**
> Thẻ (iii) ở hạng 1 → tín hiệu **lật wedge** sang manual↔automation (QĐ-2)

**B8.** *"Hiện team dùng gì để xem/triage kết quả automation? Nếu là Currents/ReportPortal/Allure/Datadog — điều gì vẫn khiến anh/chị phải mở thêm CI log / Slack / Jira?"*
*Kiểm chứng wedge cạnh tranh. Không pitch.*

```

```

---

## C — Phản chứng (10')

**C1.** *"Playwright report + trace viewer đang thiếu gì với anh/chị?"*

Nếu trả lời "đủ dùng" → đào tiếp bằng ba câu:
1. Test này pass hôm nay nhưng đã fail 7 lần trong 30 ngày qua — flaky hay vừa hỏng thật?
2. Lỗi này đã từng xuất hiện chưa, ai xử lý, kết luận là gì?
3. Cả team đang nhìn chung bức tranh nào về sức khỏe bộ test?

**Nếu vẫn trả lời đủ dùng → ghi nhận, KHÔNG tranh cãi.** Đây là dữ liệu H1 âm và nó có giá trị.

```

```

**C2.** *"Nếu có công cụ trả lời được [pain #1 của họ], nó phải làm được tối thiểu gì thì mới đáng cài thêm?"*

```

```

---

## D — Chốt (5')

**D1.** Mời làm design partner — đọc nguyên văn cam kết (`04-design-partner-email.md`).

→ Phản hồi: ☐ Đồng ý ☐ Cần suy nghĩ ☐ Từ chối
→ Nếu từ chối, lý do:

```

```

**D2.** Xin giới thiệu 2 người cùng vai trò:

```
1.
2.
```

**D3.** Cho xem bảng giá ngắn, rồi hỏi: *"Ở mức nào anh/chị có thể tự duyệt / cần xin sếp / chắc chắn không mua?"*

| | Mức | Ghi nguyên văn lý do |
|---|---|---|
| Tự duyệt được | ≤ $____ | |
| Cần xin sếp | $____ | |
| Chắc chắn không | ≥ $____ | |

> Đây là dữ liệu **H3**. Không dùng câu trả lời này thay cho hành vi mua thật.

---

## Tổng kết buổi (điền ngay sau khi kết thúc, đừng để hôm sau)

| | |
|---|---|
| H1 — pain flaky/triage trong top-2? | ☐ Có ☐ Không |
| H2 — thời gian tìm nguyên nhân | ______ phút |
| H3 — nói được con số ngân sách? | ☐ Có ☐ Không |
| Ứng viên design partner? | ☐ Có ☐ Không |

**Câu nói đáng nhớ nhất (nguyên văn):**

```

```

**Điều làm mình bất ngờ / trái với giả định:**

```

```
