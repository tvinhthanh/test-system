# 01 — Bản ghi quyết định (Decision Record)

> Trạng thái mỗi quyết định: **CHỐT** (đã quyết, mọi tài liệu phải theo) hoặc **CHỐT TẠM `[ASSUMPTION]`** (đủ để làm việc, Phase 0 có quyền lật).
> Quy tắc thay đổi: muốn lật một quyết định CHỐT phải có dữ liệu mới (phỏng vấn, số đo, hợp đồng) — không lật bằng cảm giác.

---

## QĐ-1 — Onboarding: reporter-first hay agent-first?

**Quyết định: CHỐT — Reporter-first.**

Người dùng cài `@platform/playwright-reporter` (một dòng trong `playwright.config.ts`), test vẫn chạy trên CI/máy của họ như cũ, reporter đẩy kết quả + artifact lên platform. Local agent là **đường nâng cấp** cho scheduling và thực thi có kiểm soát — không phải điều kiện dùng sản phẩm.

**Lý do:**
1. Khả thi 8/10 so với 5/10 (thẩm định §4.3): đội 1–2 người ship được trong 3–5 tháng thay vì 6–9.
2. Time-to-value của người dùng: 10 phút (cài package) so với nửa ngày (cài, cấu hình, vận hành agent trong mạng nội bộ).
3. Khớp wedge "không bắt bỏ Playwright": reporter là mô hình xâm lấn thấp nhất có thể.
4. Agent vẫn nằm trong tầm nhìn (P3) — quyết định này là **thứ tự**, không phải loại bỏ.

**Hệ quả bắt buộc (đã thực hiện trong bộ v3.1):**
- Roadmap 03: Phase 1 chỉ gồm reporter + ingestion + dashboard; local agent chuyển sang P3.
- Protocol spec 05: phần Agent Protocol giữ nguyên làm thiết kế P3, phần Ingestion API là hợp đồng P1.
- Bản tóm tắt điều hành và slide deck **phải viết lại**: mục MVP số 1 là Reporter SDK, không phải Local Agent. Chưa sửa xong hai bản đó thì không gửi cho bất kỳ ai bên ngoài.

**Trade-off chấp nhận:** không kiểm soát được môi trường chạy test ở P1 → một số tín hiệu (resource của máy chạy test, network condition) không thu được. Chấp nhận: các tín hiệu đó phục vụ P4+, không chặn giá trị lõi.

---

## QĐ-2 — Wedge: "Report Center tốt hơn" hay "cầu nối manual → automation"?

**Quyết định: CHỐT TẠM `[ASSUMPTION]` — Report Center, định vị lại quanh aggregation.**

Wedge cho 12 tháng đầu: **"Bộ nhớ dài hạn cho bộ test E2E của bạn"** — tổng hợp xuyên nhiều run: lịch sử, trend, flaky detection, triage cấp team. KHÔNG bán "rich artifacts" làm luận điểm chính (Playwright cho miễn phí rồi — thẩm định §3).

**Lý do chọn tạm hướng này thay vì "cầu nối manual → automation":**
1. Đường đi kỹ thuật ngắn hơn và tận dụng đúng khối đã ước lượng (ingestion, identity, dashboard). Hướng "cầu nối" kéo Test Management (P2, 2–4 người-tháng, nhiều importer đuôi dài) lên trước.
2. Có thể chứng minh TTRC bằng số trong 12 tuần; hướng "cầu nối" khó có chỉ số sắc như vậy.
3. Hướng "cầu nối" không mất đi: nếu Phase 0 cho thấy ≥ 8/15 team phỏng vấn có bộ test thủ công lớn chạy song song và coi đó là pain số 1 → lật quyết định này tại Gate-0, chi phí lật gần bằng 0 vì chưa code.

**Câu hỏi Phase 0 phải trả lời để chốt cứng:** trong 15 phỏng vấn, pain nào được xếp #1 nhiều hơn — "không biết fail vì sao / flaky" hay "quản lý song song manual + automation"?

---

## QĐ-3 — Thị trường: quốc tế hay VN/SEA trước?

**Quyết định: CHỐT TẠM `[ASSUMPTION]` — Sản phẩm tiếng Anh chuẩn quốc tế; go-to-market bắt đầu từ VN/SEA.**

- Sản phẩm, docs, UI: **tiếng Anh** ngay từ đầu (thêm i18n vi/th/id sau — chi phí thêm ngôn ngữ nhỏ hơn nhiều chi phí "quốc tế hóa" một sản phẩm sinh ra bằng tiếng Việt).
- Design partner và khách trả tiền đầu tiên: **VN + SEA** — nơi có lợi thế thật (giá theo mặt bằng địa phương, hỗ trợ tại chỗ, TestRail/Xray đắt khi quy đổi) mà Currents/Allure không có.
- Giá niêm yết USD, có regional pricing cho SEA (chi tiết ở 08 §6).

**Lý do:** đây không phải ngã ba thật — có thể lấy cả hai nếu tách "sản phẩm" khỏi "kênh bán". Rủi ro của "quốc tế ngay": không có kênh phân phối, đốt thời gian vào content marketing trong khi Phase 0 cần phỏng vấn mặt-đối-mặt. Rủi ro của "VN-only": khóa sản phẩm vào tiếng Việt, sau này gỡ rất đắt.

---

## QĐ-4 — Kéo AI triage từ Phase 4 lên Phase 1.5?

**Quyết định: CHỐT — Có. AI Triage v0 vào Phase 1.5.**

Phạm vi v0 (cố ý hẹp):
- Input: TestResult fail + error message + stack trace + diff so với lần pass gần nhất + lịch sử flaky của stableTestId.
- Output: **một** root-cause hypothesis có cấu trúc: `{category, confidence, evidence[], suggestedAction}` — category lấy từ taxonomy ở 06 §7.
- Chỉ **đề xuất**, người **duyệt** (accept/reject) — hành vi duyệt sinh `TriageEvent`, chính là dữ liệu đo TTRC. AI không bao giờ tự đóng issue, tự re-run, tự sửa test.

**Guardrail chống rủi ro "giả thuyết rác phá niềm tin":**
- Metric bắt buộc từ ngày đầu: `hypothesis_acceptance_rate` = accepted / (accepted + rejected). Ngưỡng: < 40% trong 2 tuần liên tiếp → tắt mặc định, chuyển thành opt-in, quay lại tinh chỉnh prompt/context.
- Hiển thị confidence; dưới 0.5 thì gắn nhãn "low confidence" thay vì giấu.
- Chi phí LLM tính vào cost model (08 §3) với trần: chỉ chạy cho fail mới (không chạy lại cho fail trùng fingerprint trong cùng ngày).

**Lý do:** 0,5–1 người-tháng một khi schema chuẩn — rẻ nhất danh sách, đúng thông điệp bán mạnh nhất, và là công cụ chứng minh TTRC ở tuần 12.

---

## QĐ-5 — Build managed agent trong 12 tháng đầu?

**Quyết định: CHỐT — Không.**

Managed agent (container do platform vận hành, resource limit, egress policy, sandbox) = tự xây một CI thu nhỏ: 3–6 người-tháng + gánh vận hành vĩnh viễn.

**Điều kiện mở lại (ghi để khỏi tranh cãi lại từ đầu):** ≥ 3 khách trả tiền yêu cầu bằng văn bản VÀ sẵn sàng trả gói giá cao hơn ≥ 2× cho tính năng này, HOẶC một hợp đồng enterprise coi đây là điều kiện ký. Trước đó, mọi nhu cầu "chạy hộ" trả lời bằng: reporter trên CI sẵn có của khách + local agent (P3) tự vận hành.

---

## QĐ-6 — Ngưỡng dừng của Phase 0?

**Quyết định: CHỐT — Có kill criteria, nguyên văn như sau.**

> Nếu sau **6 tuần** kể từ ngày bắt đầu Phase 0 không có đủ **3 design partner** thỏa cả 3 điều kiện: (a) có bộ E2E Playwright/Cypress thật đang chạy ≥ 3 lần/tuần, (b) ký cam kết phản hồi hàng tuần (mẫu ở 04 §5), (c) đồng ý cho đo baseline TTRC — thì **dừng, không chuyển sang Phase 1**. Hai lựa chọn tại điểm dừng: đổi wedge (quay lại QĐ-2, phương án "cầu nối") hoặc đóng dự án.

- **Người có quyền tuyên bố gate trượt:** ghi tên cụ thể vào ô dưới trước khi bắt đầu Phase 0. Không được là "cả team quyết" — quyết định tập thể về việc dừng là quyết định không bao giờ được ra.

| Vai trò | Tên | Ngày ký |
|---|---|---|
| Người giữ quyền kill gate | ____________ | ____ |

- "Vừa build vừa tìm partner" bị **cấm tường minh** trong 6 tuần này. Ngoại lệ duy nhất: prototype test-identity (03 §4, tuần 3–4) vì đó là hoạt động giảm rủi ro kỹ thuật, không phải build sản phẩm.

---

## Phụ lục — 5 câu hỏi mở của tài liệu 05 cũ (chốt luôn tại đây, chi tiết ở 05 §8)

| Câu hỏi | Quyết định | Ghi chú |
|---|---|---|
| Long polling hay WebSocket cho agent | Long polling (P3) | WebSocket chỉ khi có số đo cho thấy cần |
| Timeout mặc định | Bảng chuẩn ở 05 §8.2 | request 30s, job claim TTL 60s, heartbeat 20s |
| Ngưỡng chunked upload | ≥ 8 MB | dưới ngưỡng: single PUT |
| Số phiên bản protocol hỗ trợ | N và N−1 | N−2 trả 426 Upgrade Required |
| Agent tự cập nhật | Opt-in, mặc định tắt | enterprise không bao giờ chấp nhận auto-update mặc định |
