# 02 — Định vị & Bản đồ cạnh tranh (v3.1)

> Thay thế phần định vị của tài liệu 00 cũ và toàn bộ tài liệu 02 cũ. Sửa F04.

## 1. Định vị một câu

> **"Bộ nhớ dài hạn cho bộ test E2E của bạn"** — Playwright cho bạn biết *lần chạy này* fail thế nào; chúng tôi cho bạn biết *test này* đã hành xử ra sao qua 500 lần chạy gần nhất, nó có flaky không, fail lần này giống lần nào trước đó, và giả thuyết nguyên nhân là gì.

## 2. Xếp hạng lại differentiator

Thứ tự cũ (v3.0) đặt "rich artifacts" ngang hàng các giá trị khác — sai, vì Playwright cho miễn phí screenshot/video/trace viewer. Thứ tự mới:

| Hạng | Differentiator | Vì sao phòng thủ được |
|---|---|---|
| 1 | **Aggregation xuyên run**: lịch sử, trend, flaky detection ở cấp `stableTestId` | Đòi hỏi test identity ổn định (07) — khối khó nhất, chính là moat. Trace viewer của Playwright chỉ nhìn được 1 run. |
| 2 | **AI triage có vòng phản hồi đo được**: hypothesis → accept/reject → TTRC | ReportPortal có AI phân loại, nhưng không đo và không bán bằng chỉ số TTRC. Chỉ số là thông điệp, không phải tính năng. |
| 3 | **Triage cấp team**: assign, trạng thái, lịch sử quyết định trên từng failure | Trace viewer là công cụ cá nhân; triage là công cụ của team. |
| 4 | Rich artifacts gắn đúng attempt + retention thông minh | Hạ xuống hàng "table stakes" — phải có, không dùng để bán. |

**Câu trả lời chuẩn cho phản biện "Playwright HTML report + trace viewer đủ dùng rồi"** (câu hỏi mở #4 của bản thẩm định):
> "Đúng — cho *một* lần chạy, trên *máy của một người*. Ba câu Playwright report không trả lời được: (1) test này pass hôm nay nhưng đã fail 7 lần trong 30 ngày qua — nó flaky hay vừa hỏng thật? (2) lỗi này đã từng xuất hiện chưa, ai xử lý, kết luận là gì? (3) cả team đang nhìn chung một bức tranh nào về sức khỏe bộ test? Nếu team anh/chị chưa từng cần trả lời 3 câu đó thì sản phẩm này chưa dành cho anh/chị — và đó là dữ liệu Phase 0 chúng tôi cần."

## 3. Bản đồ cạnh tranh đầy đủ (bổ sung 6 tên thiếu)

| Đối thủ | Loại | Chồng lấn | Mức đe dọa | Cách tách mình |
|---|---|---|---|---|
| **Microsoft Playwright Testing (Azure)** | Cloud runner + reporting cho Playwright | Trực diện với P1 + P3 | 🔴 **Nghiêm trọng nhất** | MS bán compute (chạy hộ, browser grid), yếu ở triage/lịch sử/flaky và khóa vào Azure. Ta bán *trí nhớ và triage*, framework-agnostic, không bắt đổi CI. Theo dõi hàng quý: nếu MS thêm cross-run analytics → đôn differentiator #2, #3 và tốc độ SEA. |
| **Datadog Test Optimization** | Test ingestion trong APM suite | Flaky detection, CI visibility | 🔴 Cao | Bán qua hợp đồng Datadog sẵn có, giá enterprise. Ta thắng ở: team không dùng Datadog (đa số SEA), giá, và độ sâu E2E (trace viewer, attempt-level artifact). |
| **ReportPortal** | OSS reporting + AI phân loại | Trực diện với P1 + P4 | 🟠 Cao | Miễn phí nhưng tự vận hành nặng (Java stack, ES cluster), AI phân loại cần train, UX cũ. Ta bán SaaS 10-phút-lên và hypothesis chất lượng LLM. Không bao giờ bán "AI triage" như thể mình phát minh — bán "TTRC giảm X%". |
| **Currents.dev** | SaaS dashboard Playwright/Cypress | Trực diện nhất về hình dáng sản phẩm | 🟠 Cao | Giống ta nhất → thắng bằng: test identity xuyên rename (họ yếu), triage workflow, giá SEA, hỗ trợ tại chỗ. Đây là benchmark UX phải theo dõi hàng tháng. |
| **Allure TestOps** | Reporting + test management | P1 + P2 | 🟠 Trung bình–cao | Nặng, đắt, hướng enterprise QA process. Ta thắng ở developer-first onboarding. |
| **Qase / Testomat.io** | Test management gắn automation | P2 | 🟡 Trung bình | Họ đi từ manual TM sang automation; ta đi từ automation reporting sang TM. Nếu Phase 0 lật QĐ-2 sang "cầu nối" → nhóm này thành đối thủ chính, cập nhật lại bảng. |
| **Launchable / Sealights** | Test intelligence (chọn test thông minh, coverage risk) | P4 tầng phân tích | 🟡 Trung bình | Khác bài toán (chạy ít test hơn ≠ hiểu failure nhanh hơn) nhưng cùng ngân sách "QA tooling". |
| **TestRail / Xray** | Test management truyền thống | P2, và là hệ quy chiếu giá ở SEA | 🟡 Trung bình | Đắt khi quy đổi chi phí địa phương — dùng làm điểm neo giá, không phải đối thủ tính năng. |
| Katalon / mabl / Testim | Nền tảng thay thế framework | Gián tiếp | 🟢 Thấp | Chính là phản-định-vị của ta: "không bắt bạn bỏ Playwright". |

## 4. Rủi ro định vị cần theo dõi (owner + nhịp)

| Rủi ro | Tín hiệu cảnh báo | Nhịp kiểm tra | Phản ứng |
|---|---|---|---|
| Microsoft thêm cross-run analytics vào Playwright Testing | Changelog Azure, Playwright release notes | Hàng quý | Đôn triage-workflow + SEA GTM; xem xét tích hợp thay vì đối đầu |
| Playwright core thêm flaky detection built-in | GitHub roadmap Playwright | Hàng quý | Giá trị dồn về lịch sử dài hạn + team triage (thứ built-in local không làm được) |
| Currents ship test-identity xuyên rename | Changelog Currents | Hàng tháng | Tăng tốc 07, cân nhắc công bố thuật toán làm content marketing |

## 5. ICP và người giữ ngân sách

- **ICP (người dùng):** QA Automation Lead / SDET lead, team 3–15 QA+dev, bộ Playwright/Cypress 100–1000 test, chạy CI hằng ngày.
- **Người giữ ngân sách (thẩm định chỉ đúng: thường không phải ICP):** Engineering Manager / Head of Engineering. Hệ quả cho sản phẩm và sales:
  - Dashboard phải có **một màn hình cho EM**: trend sức khỏe bộ test theo tuần, TTRC, chi phí thời gian đội đốt vào flaky — vũ khí để ICP đi xin ngân sách.
  - Pricing (08) phải có con số mà EM so được với lương giờ kỹ sư: "flaky đốt của team anh X giờ/tháng ≈ $Y, gói này giá $Y/10".
  - Mẫu email "gửi sếp của bạn" là một deliverable của Phase 0, không phải afterthought.
