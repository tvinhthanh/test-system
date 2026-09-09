# Automation Testing Platform

> Bản tóm tắt đối ngoại · Phiên bản 3.11 · 09-09-2026
> Nguồn: `SPEC.md` v3.11 — tài liệu này sinh từ đó, không sửa tay
> Trạng thái: Phase 0 (validation). Chưa có sản phẩm chạy được.

---

## Vấn đề

Automation E2E của team đang chạy. Nhưng khi nó fail:

- Mất quá lâu để biết đó là bug thật, test flaky, lỗi môi trường hay lỗi code test.
- Screenshot, video, trace, console log nằm rải rác giữa CI log, máy local và devtools.
- Test lúc pass lúc fail bị rerun cho qua — và đôi khi bug thật lọt ra production.
- Không ai trong team nhìn chung một bức tranh về sức khỏe bộ test.

## Giải pháp

**Bộ nhớ dài hạn cho bộ test E2E của bạn.**

Playwright cho bạn biết *lần chạy này* fail thế nào. Chúng tôi cho bạn biết *test này* đã hành xử ra sao qua 500 lần chạy gần nhất: nó có flaky không, fail lần này giống lần nào trước đó, ai đã xử lý, và giả thuyết nguyên nhân là gì.

## Cách dùng

Không phải đổi CI. Không phải viết lại test. Không phải bỏ Playwright.

```
Thêm một dòng vào playwright.config.ts
        ↓
Test vẫn chạy trên CI/máy của bạn như cũ
        ↓
Reporter đẩy kết quả + artifact lên platform
        ↓
Xem report có lịch sử, flaky detection, triage
```

Mục tiêu: từ lúc cài tới report đầu tiên **dưới 15 phút**.

## MVP

1. **Reporter SDK cho Playwright** — đường vào chính, xâm lấn thấp nhất
2. Ingestion API
3. Test identity ổn định xuyên rename và di chuyển file
4. Report Center: lịch sử xuyên run, flaky detection
5. Artifact: screenshot / video / trace, gắn đúng từng lần retry
6. Trace viewer nhúng
7. Xác thực và phân quyền theo tenant

## Roadmap

| Phase | Nội dung | Điều kiện vào |
|---|---|---|
| **P0** — Validate | 15 phỏng vấn, 3 design partner, đo baseline thời gian tìm nguyên nhân | Đang ở đây |
| **P1** — Report Center | Reporter SDK, ingestion, test identity, lịch sử, flaky, artifact, trace viewer | P0 đạt tiêu chí |
| **P1.5** — AI Triage | Giả thuyết nguyên nhân có cấu trúc; người duyệt, AI không tự sửa test | P1 đạt tiêu chí |
| **P2** — Test Management | Test case thủ công, nối manual ↔ automation, requirement mapping, release gate, import | P1.5 đạt tiêu chí và có nhu cầu thật |
| **P3** — Scheduler & Agent | Local agent (pull-only), scheduler, CI ingestion, notification, guard cho môi trường production | Khách trả tiền yêu cầu scheduling |
| **P4** — AI nâng cao | Phân cụm failure, gợi ý sửa | Chất lượng AI ở P1.5 đạt ngưỡng ổn định |
| **P5** — Mở rộng | Visual regression, performance, distributed runner | *Vision 3–5 năm, gated by demand* |
| **P6** — Enterprise | SSO, RBAC nâng cao, audit log, on-prem | *Vision 3–5 năm, gated by demand* |

Mỗi phase có tiêu chí định lượng để đi tiếp — và tiêu chí để dừng. Chúng tôi không build phase sau nếu phase trước không chứng minh được giá trị với người dùng thật.

## Điểm khác biệt

| | |
|---|---|
| **Tổng hợp xuyên nhiều lần chạy** | Lịch sử, xu hướng, flaky detection ở cấp từng test — bền vững qua rename và di chuyển file. Đây là khối kỹ thuật khó nhất và là nền của mọi thứ còn lại |
| **AI triage có vòng phản hồi đo được** | Giả thuyết → người duyệt hoặc từ chối → đo lại thời gian tìm nguyên nhân. Chúng tôi bán chỉ số, không bán tính năng |
| **Triage cấp team** | Giao việc, trạng thái, lịch sử quyết định trên từng failure. Trace viewer là công cụ cá nhân; triage là công cụ của team |
| **Artifact gắn đúng từng lần retry** | Video lần fail và lần pass là hai artifact khác nhau — chênh lệch giữa chúng là dữ liệu quý nhất khi chẩn đoán |

## Chỉ số thành công

**Thời gian trung vị từ lúc test fail đến lúc team chấp nhận một giả thuyết nguyên nhân.**

Chúng tôi đo con số này của bạn *trước* khi bạn dùng sản phẩm, rồi đo lại sau. Nếu nó không giảm, sản phẩm chưa đủ tốt và chúng tôi cần biết điều đó.

Đi kèm hai chỉ số chống tự lừa: tỉ lệ failure thực sự được triage, và tỉ lệ giả thuyết AI được người xem xét. Một con số đẹp trên 30% failure còn lại bỏ mặc thì không có ý nghĩa gì.

## Bối cảnh thị trường

Không gian này đã có sản phẩm trưởng thành: Currents.dev, ReportPortal, Allure TestOps, Datadog Test Optimization, Azure App Testing — Playwright Workspaces, và nhóm test management truyền thống như TestRail, Xray, Qase.

Chúng tôi không tuyên bố hơn họ ở điểm nào cho tới khi có benchmark kiểm chứng được. Điều chúng tôi chọn làm khác:

- Không bắt bạn rời Playwright hay đổi CI.
- Tập trung vào độ bền của định danh test qua thời gian, thay vì chỉ hiển thị kết quả.
- Đo và bán bằng thời gian tìm nguyên nhân, không bằng danh sách tính năng.
- Giá và hỗ trợ theo mặt bằng Việt Nam và Đông Nam Á.

## Chúng tôi đang tìm gì

**3 design partner** cho giai đoạn validation.

Bên bạn:
- Có bộ E2E Playwright hoặc Cypress thật, chạy ít nhất 3 lần một tuần trên CI.
- Cho phép đo baseline thời gian tìm nguyên nhân trong 2 tuần.
- Một cuộc gọi phản hồi 30 phút mỗi tuần, trong 12 tuần.
- Phản hồi thẳng. Chê hữu ích hơn khen.

Bên chúng tôi:
- Miễn phí ít nhất 12 tháng.
- Hỗ trợ trực tiếp qua kênh riêng, phản hồi trong một ngày làm việc.
- Không dùng dữ liệu test của bạn ngoài mục đích vận hành và cải thiện sản phẩm; không chia sẻ cho bên thứ ba; xóa toàn bộ theo yêu cầu trong 7 ngày.
- Ưu đãi giá sáng lập nếu sau này chuyển sang trả phí.

Hai bên dừng bất cứ lúc nào bằng một email. Điều khoản dữ liệu vẫn có hiệu lực sau khi dừng.

---

*Tài liệu này sinh từ `SPEC.md` v3.11 ngày 09-09-2026. Mọi con số về giá và chi phí trong tài liệu nội bộ là giả định đang được kiểm chứng, không phải cam kết.*
