# Automation Testing Platform — Spec hợp nhất

> **Phiên bản:** v3.2-reviewed · **Ngày:** 24-08-2026 · **Trạng thái:** Đã review nội dung; còn quyết định vận hành **Nguồn:** bộ v3.0 (8 tài liệu, `docs/v3.0/`) + bộ v3.1 (10 tài liệu, `docs/v3.1/`) + bản thẩm định 24-08-2026 **Quy tắc:** Đây là nguồn sự thật duy nhất. Mọi file trong `docs/` là lịch sử, chỉ để tra cứu. Tài liệu nào nói khác file này → file đó sai.

---

## 0. Cách đọc và những gì đã thay đổi khi gộp

### 0.1 Bộ tài liệu này đến từ đâu

| **Bộ** | **Vị trí** | **Vai trò** |
| ------------------- | ------------ | -------------------------------------------------------------------- |
| v3.0                | `docs/v3.0/` | Bản gốc, 8 tài liệu thiết kế + 3 bản đối ngoại. **Đã lỗi thời**      |
| v3.1                | `docs/v3.1/` | Bản viết lại sau thẩm định, 10 tài liệu. Nội dung đúng nhưng rời rạc |
| **SPEC.md**         | root         | **Bản gộp, đã vá lỗi khớp nối. Dùng bản này**                        |

Việc tách `docs/v3.0/` và `docs/v3.1/` cũng xử lý luôn vụ va chạm đánh số: trước đó root có cả `01-ICP-Personas-and-JTBD.md` lẫn `01-decisions.md`, rất dễ mở nhầm.

### 0.2 Những gì tôi đã SỬA khi gộp và review v3.2

Các lỗi N01–N10 đến từ lần gộp v3.1; N11–N19 là phát hiện mới trong review 24-08-2026. Mục đã đánh ✅ là nội dung đã được sửa ngay trong SPEC này.

| # | Lỗi / rủi ro | Cách sửa | Xem mục |
|---|---|---|---|
| N01 | Timeline cũ cho P1 chạy trước Gate-0 | Dời mọi build sản phẩm sau Gate-0 | §4.3 |
| N02 | P1 4–6 người-tháng không khớp phạm vi thật | Ước lượng lại **6,5–11,5**; giữ **P1-min 4–6** | §4.2, §4.4 |
| N03 | `UNIQUE(runId, clientTestId)` làm mất test khi fingerprint trùng | Thêm `occurrenceIndex` | §8.4 |
| N04 | Unique của `TestIdentityAlias` không implement được | Denormalize `projectId` | §8.5 |
| N05 | `flakyScore` chia 0 khi `totalRuns = 1` | Guard `< 2` → `insufficient_data` | §8.6 |
| N06 | `flaky_pass` mất khỏi `Run.status` nhưng nguyên tắc “không xanh sạch” vẫn còn | ✅ **Chốt QĐ-7 phương án B:** giữ enum, dùng `flakyCount`; UI `passed + flakyCount>0` phải hiển thị warning/amber | §1.7, §8.4 |
| N07 | Production Safety Guard biến mất nhưng bản đối ngoại vẫn bán | ✅ **Chốt QĐ-8:** dời sang P3 cùng Local Agent; xóa khỏi MVP/P1 | §1.8, §4.1, §4.7 |
| N08 | Observability và JTBD biến mất | Khôi phục | §3.3, §6.4 |
| N09 | Gate-1 và thuật toán identity dùng hai thước đo khác nhau | Hòa giải về precision/recall | §4.5, §9.6 |
| N10 | Claim về Currents chưa kiểm chứng | Bỏ claim khỏi thông điệp bán hàng; benchmark riêng | §2.4 |
| N11 | **P1 đầy đủ 6,5–11,5 người-tháng nhưng timeline chỉ cho 2 người × 12 tuần ≈ 6 người-tháng** | ✅ Tách timeline P1-min và P1-full; Gate-1 theo cuối scope đã chọn, không hardcode tuần 18 cho mọi trường hợp | §4.3, §4.5 |
| N12 | Partner #3 ký tuần 5 nhưng baseline tiến cứu cần 2 tuần, Gate-0 lại ở cuối tuần 6 | ✅ Yêu cầu đủ 3 partner ký **chậm nhất cuối tuần 4** | §5.6 |
| N13 | Phase 0 tuyên bố kiểm chứng H1–H3 nhưng Gate-0 cũ chỉ bắt H1; có thể pass dù TTRC thấp hoặc không ai trả tiền | ✅ Gate-0 nay bắt đủ **H1 + H2 + H3** | §4.5, §5.1 |
| N14 | `Split` cho phép chọn mốc T nhưng alias schema chỉ cho một mapping fingerprint vĩnh viễn | ✅ Thêm `validFrom/validTo`, partial unique cho alias đang hiệu lực | §8.5, §9.4 |
| N15 | TTRC dùng `firstFailedAt` từ client → lệch clock; acceptance rate có thể đẹp giả nếu user bỏ qua hypothesis rác | ✅ Thêm `failureObservedAt` server-side, `hypothesis_decision_rate` và `triage_coverage` | §1.4, §8.3, §4.5 |
| N16 | Bản đồ cạnh tranh dùng tên dịch vụ Microsoft đã retired và đánh giá thấp overlap của Currents | ✅ Cập nhật thành **Azure App Testing / Playwright Workspaces**; bỏ feature-gap chưa có bằng chứng | §2.4 |
| N17 | Anchor TestRail `$37 × 10` đã cũ theo thông báo giá 2026; regional/founding discount chưa nói có stack hay không | ✅ Cập nhật anchor; discount **không stack** và có contribution-margin floor | §10.5 |
| N18 | API token random dùng Argon2id trên mọi ingest request gây chi phí xác thực không cần thiết; thiếu CSRF/origin guard và kiểm tra kích thước object sau presigned upload | ✅ Token dùng HMAC-SHA-256; thêm Origin/CSRF guard; HEAD/checksum/cleanup multipart | §7.7, §8.8, §11.2, §11.4 |
| N19 | Billing theo `stableTestId` phụ thuộc trực tiếp vào thuật toán merge/split đang còn hiệu chỉnh | ✅ Chưa charge theo active-test ở P1; chạy **shadow meter 4 tuần** trước billing thật | §10.5–10.6 |

### 0.3 Những gì vẫn CẦN người chịu trách nhiệm quyết

QĐ-7 và QĐ-8 đã được chốt trong review này vì các phần khác của SPEC đã phụ thuộc vào chính hai hướng đó. Còn lại là quyết định vận hành không thể suy ra từ kiến trúc:

| Vấn đề | Khi nào bắt buộc chốt | Xem mục |
|---|---|---|
| **Tên người giữ quyền kill gate** | **Trước ngày bắt đầu Phase 0** | §1.6 |
| **Ngày bắt đầu Phase 0** | Trước khi dùng mốc tuần 1–6 | §4.3, §5.6 |
| **P1-min hay P1-full** | **Không cần chặn Phase 0**; phải chốt tại Gate-0 trước khi build | §4.3, §4.4 |

> Không cần trì hoãn phỏng vấn Phase 0 chỉ vì chưa chọn P1-min/P1-full. Mục tiêu của Phase 0 chính là tạo dữ liệu để quyết định phạm vi build có đáng đầu tư hay không.

### 0.4 Việc còn treo, KHÔNG được đánh dấu hoàn thành

Ba bản đối ngoại trong `docs/v3.0/` **chưa được sửa** — tôi verify bằng cách đọc file, không phải suy đoán:

```
Automation_Testing_Platform_v3.{md,docx,pptx}    2026-07-07, nguyên vẹn
  MVP mục #1:  "Local Agent"              ← vẫn agent-first, trái QĐ-1
  MVP mục #6:  "Production Safety Guard"  ← vẫn quảng cáo như MVP dù QĐ-8 đã dời sang P3
  Roadmap:     grep "Phase 5|Phase 6" → 0 ← vẫn bị cắt
  .docx:       **Unified Quality...**     ← vẫn còn ** literal

```

README của bộ v3.1 đánh dấu F12 và F16 là "✅ Đóng". **Sai** — quy tắc xuất bản đã viết, nhưng hiện vật chưa sửa. Và nó mâu thuẫn với chính QĐ-1, chỗ ghi *"Chưa sửa xong hai bản đó thì không gửi cho bất kỳ ai bên ngoài."*

Điều này nguy hiểm ở đúng thời điểm: Phase 0 tuần 1–6 là lúc mang deck đi gặp design partner. Ai tin ma trận README sẽ gửi bản pitch agent-first, roadmap cụt, cho đúng nhóm người mà v3.1 vừa quyết định pitch reporter-first.

**Cấm gửi ba file đó ra ngoài cho đến khi regenerate theo §4.7.**

---

## 1. Bản ghi quyết định (ADR)

> Trạng thái: **CHỐT** (đã quyết, mọi phần khác phải theo) · **CHỐT TẠM ****`[ASSUMPTION]`** (đủ để làm việc, Phase 0 có quyền lật) · **CẦN QUYẾT** (chưa ai quyết). Muốn lật một quyết định CHỐT phải có dữ liệu mới — phỏng vấn, số đo, hợp đồng. Không lật bằng cảm giác.

### 1.1 QĐ-1 — Onboarding: reporter-first ✅ CHỐT

Người dùng cài `@platform/playwright-reporter` (một dòng trong `playwright.config.ts`), test vẫn chạy trên CI/máy của họ như cũ, reporter đẩy kết quả + artifact lên platform. Local agent là **đường nâng cấp** cho scheduling và thực thi có kiểm soát, không phải điều kiện dùng sản phẩm.

**Lý do:**

1. Khả thi 8/10 so với 5/10 — đội 1–2 người ship được thay vì không ship được.
2. Time-to-value: 10 phút (cài package) so với nửa ngày (cài, cấu hình, vận hành agent trong mạng nội bộ).
3. Khớp wedge "không bắt bỏ Playwright" — reporter là mô hình xâm lấn thấp nhất có thể.
4. Agent vẫn nằm trong tầm nhìn (P3). Đây là quyết định về **thứ tự**, không phải loại bỏ.

**Hệ quả bắt buộc:**

- Local agent chuyển từ P1 sang **P3** (§4.1).
- Phần Agent Protocol (§7.8) giữ làm thiết kế P3, chưa build. Phần Ingestion API (§7.2) là hợp đồng P1.
- Production Safety Guard **đã chốt dời sang P3** theo QĐ-8.
- Ba bản đối ngoại **phải viết lại** trước khi gửi ra ngoài (§0.4).

**Trade-off chấp nhận:** không kiểm soát môi trường chạy test ở P1 → mất một số tín hiệu (resource máy chạy, network condition). Các tín hiệu đó phục vụ P4+, không chặn giá trị lõi.

### 1.2 QĐ-2 — Wedge: Report Center, định vị quanh aggregation `[ASSUMPTION]`

Wedge 12 tháng đầu: **"Bộ nhớ dài hạn cho bộ test E2E của bạn"** — tổng hợp xuyên nhiều run: lịch sử, trend, flaky detection, triage cấp team. **Không** bán "rich artifacts" làm luận điểm chính (Playwright cho miễn phí rồi).

**Vì sao tạm chọn hướng này thay vì "cầu nối manual → automation":**

1. Đường kỹ thuật ngắn hơn, tận dụng đúng khối đã ước lượng. Hướng "cầu nối" kéo Test Management (P2, 2–4 người-tháng, nhiều importer đuôi dài) lên trước.
2. Chứng minh được TTRC bằng số trong một chu kỳ gate; hướng "cầu nối" khó có chỉ số sắc như vậy.
3. Hướng "cầu nối" không mất đi: nếu Phase 0 cho thấy ≥ 8/15 team phỏng vấn có bộ test thủ công lớn chạy song song và coi đó là pain #1 → lật tại Gate-0, chi phí lật gần bằng 0 vì chưa code.

**Câu hỏi Phase 0 phải trả lời để chốt cứng:** trong 15 phỏng vấn, pain nào được xếp #1 nhiều hơn — "không biết fail vì sao / flaky" hay "quản lý song song manual + automation"?

### 1.3 QĐ-3 — Thị trường: sản phẩm tiếng Anh, GTM từ VN/SEA `[ASSUMPTION]`

- Sản phẩm, docs, UI: **tiếng Anh** ngay từ đầu. Thêm i18n vi/th/id sau — chi phí thêm ngôn ngữ nhỏ hơn nhiều chi phí quốc-tế-hóa một sản phẩm sinh ra bằng tiếng Việt.
- Design partner và khách trả tiền đầu tiên: **VN + SEA** — nơi có lợi thế thật (giá theo mặt bằng địa phương, hỗ trợ tại chỗ, TestRail/Xray đắt khi quy đổi) mà Currents/Allure không có.
- Giá niêm yết USD, có regional pricing cho SEA (§10.5).

**Lý do:** đây không phải ngã ba thật — lấy được cả hai nếu tách "sản phẩm" khỏi "kênh bán". Rủi ro của "quốc tế ngay": không có kênh phân phối, đốt thời gian vào content marketing trong khi Phase 0 cần phỏng vấn mặt-đối-mặt. Rủi ro của "VN-only": khóa sản phẩm vào tiếng Việt, sau gỡ rất đắt.

### 1.4 QĐ-4 — AI Triage v0 vào Phase 1.5 ✅ CHỐT

Phạm vi v0 (cố ý hẹp):

- **Input:** TestResult fail + error message + stack trace + diff so với lần pass gần nhất + lịch sử flaky của `stableTestId`.
- **Output:** *một* root-cause hypothesis có cấu trúc `{category, confidence, evidence[], suggestedAction}`, category lấy từ taxonomy §8.7.
- Chỉ **đề xuất**, người **duyệt**. Hành vi duyệt sinh `TriageEvent`; AI không bao giờ tự đóng issue, tự re-run, tự sửa test.

**Metric bắt buộc từ ngày đầu:**

- `hypothesis_acceptance_rate = accepted / (accepted + rejected)`.
- `hypothesis_decision_rate = (accepted + rejected) / eligible_hypotheses` — chống trường hợp hypothesis rác bị bỏ qua nên acceptance rate nhìn đẹp giả.
- `triage_coverage = failures có HYPOTHESIS_ACCEPTED / failures đủ điều kiện triage`.

**Guardrail:**

- Chỉ kết luận chất lượng AI khi rolling 14 ngày có **≥ 30 quyết định**. Ít hơn → `insufficient_data`, không bật/tắt bằng mẫu quá nhỏ.
- Nếu `hypothesis_acceptance_rate < 40%` **và** `hypothesis_decision_rate ≥ 60%` trong 2 cửa sổ 14 ngày liên tiếp → tắt mặc định, chuyển opt-in.
- Nếu `hypothesis_decision_rate < 60%` → ưu tiên sửa UX/context/instrumentation; không được tuyên bố AI tốt chỉ từ acceptance rate.
- Confidence < 0.5 gắn nhãn `low confidence`.
- Fail trùng fingerprint trong cùng ngày không chạy lại hypothesis; LLM có trần cứng §10.3.

**Lý do:** 0,5–1 người-tháng một khi schema chuẩn; đây là lớp tăng tốc triage, không phải điều kiện tồn tại của Report Center.

### 1.5 QĐ-5 — Không build managed agent trong 12 tháng đầu ✅ CHỐT

Managed agent (container do platform vận hành, resource limit, egress policy, sandbox) = tự xây một CI thu nhỏ: 3–6 người-tháng + gánh vận hành vĩnh viễn.

**Điều kiện mở lại** (ghi để khỏi tranh cãi lại từ đầu): ≥ 3 khách trả tiền yêu cầu **bằng văn bản** VÀ sẵn sàng trả gói cao hơn ≥ 2× cho tính năng này; HOẶC một hợp đồng enterprise coi đây là điều kiện ký.

Trước đó, mọi nhu cầu "chạy hộ" trả lời bằng: reporter trên CI sẵn có của khách + local agent (P3) khách tự vận hành.

### 1.6 QĐ-6 — Ngưỡng dừng của Phase 0 ✅ CHỐT

> Nếu sau **6 tuần** kể từ ngày bắt đầu Phase 0 không có đủ **3 design partner** thỏa cả 3 điều kiện: (a) có bộ E2E Playwright/Cypress thật đang chạy ≥ 3 lần/tuần, (b) ký cam kết phản hồi hàng tuần (mẫu §5.5), (c) đồng ý cho đo baseline TTRC — thì **dừng, không chuyển sang Phase 1**. Hai lựa chọn tại điểm dừng: đổi wedge (quay lại QĐ-2, phương án "cầu nối") hoặc đóng dự án.

**Người có quyền tuyên bố gate trượt** — điền trước khi bắt đầu Phase 0. Không được là "cả team quyết": quyết định tập thể về việc dừng là quyết định không bao giờ được ra.

| **Vai trò** | **Tên** | **Ngày ký** |
| ------------------------- | ------------------------ | -------- |
| Người giữ quyền kill gate | \_\_\_\_\_\_\_\_\_\_\_\_ | \_\_\_\_ |

**"Vừa build vừa tìm partner" bị cấm tường minh trong 6 tuần này.** Ngoại lệ duy nhất: prototype test-identity (§4.3 tuần 3–4) — đó là hoạt động giảm rủi ro kỹ thuật, không phải build sản phẩm.

### 1.7 QĐ-7 — `flaky_pass` ở cấp Run ✅ CHỐT: phương án B

**Quyết định:** giữ `Run.status = queued|running|passed|failed|error|cancelled`; thêm/giữ `Run.flakyCount int`.

Bất biến sản phẩm:

1. `Run.status = passed` chỉ nói **không còn test fail cuối cùng**.
2. Nếu `flakyCount > 0`, dashboard **không được hiển thị clean green**. Trạng thái trình bày phải là `Passed with flaky tests`/warning (amber), có badge số lượng.
3. API/filter phải cho phép `hasFlaky=true`; client không được suy diễn “passed = hoàn toàn sạch”.
4. `flakyCount` là dữ liệu dẫn xuất từ `TestResult.finalStatus = flaky_pass`, phải recompute được nếu cần.

**Lý do:** tránh phình enum và tránh mọi query “passed” phải nhớ gộp hai status, nhưng vẫn giữ nguyên nguyên tắc v3.0 rằng retry-pass không được che flakiness.

### 1.8 QĐ-8 — Production Safety Guard ✅ CHỐT: dời sang P3

**Quyết định:** Production Safety Guard **không thuộc MVP/P1**. Nó được dời sang **P3**, gắn với Local Agent/Scheduler.

**Lý do kỹ thuật:** reporter-first chỉ quan sát kết quả sau khi test đã chạy; platform không điều khiển execution nên không thể chặn một destructive test trước khi nó chạm production. Safety Guard chỉ có ý nghĩa ở execution path do agent kiểm soát.

**Hệ quả:**

- Xóa `Production Safety Guard` khỏi mọi danh sách MVP/P1 và khỏi differentiator hiện tại.
- P3 mới được phép hứa các guard như environment allowlist, destructive-test policy, approval trước prod smoke.
- Nếu Phase 0 có ≥ 5/15 ICP xếp production-safety là pain quan trọng **và** có ít nhất một khách sẵn sàng trả tiền cho execution control → được quyền xem lại thứ tự P3 bằng ADR mới.
- Ba bản đối ngoại cũ vẫn bị cấm gửi cho tới khi regenerate theo §4.7.

### 1.9 Phụ lục — 5 câu hỏi mở của v3.0, đã chốt

| **Câu hỏi** | **Quyết định** | **Ghi chú** |
| ------------------------------------ | ------------------------ | ------------------------------------------------------- |
| Long polling hay WebSocket cho agent | **Long polling** (P3)    | WebSocket chỉ khi có số đo cho thấy cần                 |
| Timeout mặc định                     | Bảng chuẩn §7.9          | request 30 s, claim TTL 60 s, heartbeat 20 s            |
| Ngưỡng chunked upload                | **≥ 8 MB**               | Dưới ngưỡng: single PUT                                 |
| Số phiên bản protocol hỗ trợ         | **N và N−1**             | N−2 trả `426 Upgrade Required`                          |
| Agent tự cập nhật                    | **Opt-in, mặc định tắt** | Enterprise không bao giờ chấp nhận auto-update mặc định |

---

## 2. Định vị và cạnh tranh

### 2.1 Định vị một câu

> **"Bộ nhớ dài hạn cho bộ test E2E của bạn"** — Playwright cho bạn biết *lần chạy này* fail thế nào; chúng tôi cho bạn biết *test này* đã hành xử ra sao qua 500 lần chạy gần nhất, nó có flaky không, fail lần này giống lần nào trước đó, và giả thuyết nguyên nhân là gì.

### 2.2 Xếp hạng differentiator

Thứ tự cũ đặt "rich artifacts" ngang hàng các giá trị khác — sai, vì Playwright cho miễn phí screenshot/video/trace viewer.

| **Hạng** | **Differentiator** | **Vì sao phòng thủ được** |
| ------------------------------------------- | ------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| 1                                           | **Aggregation xuyên run**: lịch sử, trend, flaky detection ở cấp `stableTestId` | Đòi hỏi test identity ổn định (§9) — khối khó nhất, chính là moat. Trace viewer của Playwright chỉ nhìn được 1 run     |
| 2                                           | **AI triage có vòng phản hồi đo được**: hypothesis → accept/reject → TTRC       | ReportPortal có AI phân loại, nhưng không đo và không bán bằng chỉ số TTRC. Chỉ số là thông điệp, không phải tính năng |
| 3                                           | **Triage cấp team**: assign, trạng thái, lịch sử quyết định trên từng failure   | Trace viewer là công cụ cá nhân; triage là công cụ của team                                                            |
| 4                                           | Rich artifacts gắn đúng attempt + retention thông minh                          | Hạ xuống hàng *table stakes* — phải có, không dùng để bán                                                              |

> Lưu ý QĐ-8: "Safety Policy" từng là differentiator #5 ở v3.0. Nó không còn trong danh sách này vì reporter-first không thực thi test. Đừng để nó quay lại slide.

### 2.3 Câu trả lời chuẩn cho phản biện nặng nhất

**"Playwright HTML report + trace viewer đủ dùng rồi."**

> "Đúng — cho *một* lần chạy, trên *máy của một người*. Ba câu Playwright report không trả lời được: (1) test này pass hôm nay nhưng đã fail 7 lần trong 30 ngày qua — nó flaky hay vừa hỏng thật? (2) lỗi này đã từng xuất hiện chưa, ai xử lý, kết luận là gì? (3) cả team đang nhìn chung một bức tranh nào về sức khỏe bộ test? Nếu team anh/chị chưa từng cần trả lời 3 câu đó thì sản phẩm này chưa dành cho anh/chị — và đó là dữ liệu Phase 0 chúng tôi cần."

Câu cuối quan trọng: người phỏng vấn **không được tranh cãi** để bảo vệ sản phẩm. Trả lời "đủ dùng" là dữ liệu H1 âm, ghi nhận và đi tiếp.

### 2.4 Bản đồ cạnh tranh — cập nhật 24-08-2026

| Đối thủ | Loại / overlap hiện tại | Đe dọa | Cách định vị an toàn |
|---|---|---|---|
| **Azure App Testing — Playwright Workspaces** | Cloud-hosted browsers + reporting, test runs, artifacts, trace viewer. **Microsoft Playwright Testing cũ đã retired ngày 08-03-2026.** | 🔴 Nghiêm trọng nhất | Không claim “Microsoft yếu ở history/flaky” nếu chưa benchmark. Tách bằng workflow triage xuyên run, CI không phụ thuộc Azure, test identity và giá/hỗ trợ SEA — tất cả phải đo bằng benchmark/Phase 0. |
| **Currents.dev** | Reporter + dashboard Playwright, test history, flaky detection/monitoring/quarantine, error analysis, orchestration | 🔴 Rất cao | Đây là đối thủ hình dạng gần nhất. **Bỏ claim “yếu ở rename identity” cho tới khi kiểm chứng.** So sánh thật trên rename/move, triage workflow, TTRC, onboarding và pricing. |
| **ReportPortal** | OSS/hosted reporting + analysis/AI, history và issue workflow | 🟠 Cao | Không bán “AI analysis” như phát minh mới. Giá trị phải là onboarding nhẹ + E2E-specific identity + workflow đo TTRC. Tránh claim chủ quan kiểu “UX cũ” nếu chưa có user test. |
| **Allure TestOps** | Reporting + test management + analytics | 🟠 TB–cao | Tách bằng developer-first onboarding và reporter-first wedge; giá/độ nặng phải verify trước khi đưa ra bán hàng. |
| **Datadog Test Optimization** | CI visibility/test optimization trong suite observability | 🔴 Cao với org đã dùng Datadog | Tập trung team không muốn mua cả observability suite; so sánh trên E2E depth và total cost, không bằng feature claim chưa đo. |
| **Qase / Testomat.io** | Test management + automation | 🟡 Trung bình | Nếu QĐ-2 lật sang manual→automation, nhóm này thành đối thủ chính và bảng này phải viết lại. |
| **TestRail / Xray** | Test management truyền thống | 🟡 Trung bình | Chủ yếu là benchmark ngân sách/process, không phải đối thủ P1 trực tiếp. |
| Katalon / mabl / Testim | Platform thay thế/bao quanh framework | 🟢 Thấp–TB | Phản-định-vị: “giữ Playwright/CI hiện tại”; vẫn theo dõi vì họ có thể mở reporting/AI sâu hơn. |

**Quy tắc competitor claims:** chỉ được dùng trong tài liệu bán hàng khi có một trong ba bằng chứng: (a) docs chính thức hiện hành, (b) account benchmark có ảnh/changelog ngày kiểm chứng, hoặc (c) lời khách hàng được ghi nguồn. Claim không có bằng chứng phải viết `[CHƯA KIỂM CHỨNG]` hoặc bỏ hẳn.

**Deliverable Phase 0 bổ sung:** ít nhất 5/15 interview phải hỏi “đang dùng gì để xem/triage automation results?”; nếu có user Currents/ReportPortal/Allure, hỏi cụ thể điều gì vẫn làm họ mất thời gian. Đây là phép thử trực tiếp xem wedge có thật sự khác biệt hay chỉ khác wording.

### 2.5 ICP và người giữ ngân sách

- **ICP (người dùng):** QA Automation Lead / SDET lead. Team 3–15 QA+dev, bộ Playwright/Cypress 100–1000 test, chạy CI hằng ngày.
- **Người giữ ngân sách:** Engineering Manager / Head of Engineering — **thường không phải ICP**. Hệ quả bắt buộc:
  - Dashboard phải có **một màn hình cho EM**: trend sức khỏe bộ test theo tuần, TTRC, thời gian đội đốt vào flaky. Đây là vũ khí để ICP đi xin ngân sách.
  - Pricing phải có con số EM so được với lương giờ kỹ sư: *"flaky đốt của team anh X giờ/tháng ≈ $Y; gói này giá $Y/10"*.
  - Mẫu email "gửi sếp của bạn" là **deliverable của Phase 0**, không phải afterthought.

---

## 3. ICP, Persona và JTBD

> Khôi phục từ `docs/v3.0/01-ICP-Personas-and-JTBD.md` — nội dung này biến mất hoàn toàn khỏi bộ v3.1 (N08). Đã rút gọn về phần còn hiệu lực sau QĐ-1 và QĐ-2.

### 3.1 Persona

| **Persona** | **Vai trò trong MVP** | **Nhu cầu chính** |
| ----------------------------------------- | ------------------ | ------------------------------------------------------- |
| **QA Automation Lead**                    | 🎯 ICP chính       | Triage failure nhanh, phát hiện flaky, niềm tin vào CI  |
| **Engineering Manager**                   | 💰 Người duyệt chi | Trend sức khỏe, TTRC, release confidence                |
| **Developer**                             | Người dùng phụ     | Artifact gắn commit/branch, lỗi network/console rõ ràng |
| **QA Manual**                             | Ngoài phạm vi P1   | Vào từ P2 (Test Management) — hoặc sớm hơn nếu QĐ-2 lật |
| **PO / BA**                               | Ngoài phạm vi      | Trạng thái release cấp cao                              |

**Không tối ưu cho ở MVP:** solo developer, người học Playwright, team chưa có quy trình test, tổ chức đòi SSO/RBAC đầy đủ ngay từ đầu.

### 3.2 Nỗi đau của người mua

> *"Automation E2E của chúng tôi có chạy, nhưng khi nó fail thì mất quá lâu để biết đó là bug thật, test flaky, lỗi môi trường hay lỗi code test."*

### 3.3 Jobs To Be Done

| **#** | **JTBD** | **Phase phục vụ** |
| ---------------------- | ----------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------- |
| 1                      | Khi test fail sau deploy, hiểu nhanh **cái gì fail và vì sao**, để quyết định rollback / sửa app / sửa test | P1 + P1.5                                                           |
| 2                      | Gom screenshot, video, trace, console, network vào **một report**, khỏi nhảy giữa CI log và devtools        | P1                                                                  |
| 3                      | Test lúc pass lúc fail → hệ thống **tự nhận diện và gắn nhãn flaky**, để team không coi là pass sạch        | P1                                                                  |
| 4                      | So sánh môi trường / browser / commit khi "local pass, staging fail"                                        | P1                                                                  |
| 5                      | ~~Chạy smoke test trên production an toàn, test phá hoại bị chặn~~                                          | ✅ **QĐ-8:** không khả thi ở reporter-first; dời sang P3 |
| 6                      | Trước release, thấy flow nào pass/fail/skip để **ra quyết định có bằng chứng**                              | P1                                                                  |
| 7                      | Nhận diện case thủ công nào nên chuyển thành automation                                                     | P2 (hoặc P1 nếu lật QĐ-2)                                           |
| 8                      | AI tóm tắt nguyên nhân khả dĩ + hành động tiếp theo                                                         | P1.5                                                                |

### 3.4 Hành trình người dùng MVP

```
Đăng ký → Tạo project → Cài reporter vào repo Playwright sẵn có
   → Run đầu tiên tự đẩy lên → Xem report có artifact
   → (P1.5) AI tóm tắt nguyên nhân → Người duyệt hypothesis
   → Quyết định: công cụ này có tiết kiệm thời gian debug không?

```

---

## 4. Roadmap và Gates

> Đây là **bản đồ định hướng có gate**, không phải kế hoạch thi công. Chỉ P0, P1, P1.5 được lập kế hoạch chi tiết; P2+ chỉ lập kế hoạch sau khi gate trước pass.

### 4.1 Roadmap tổng

| **Phase** | **Tên** | **Nội dung lõi** | **Điều kiện vào** | **Người-tháng** |
| ------------------------------------------------ | ----------------- | ------------------------------------------------------------------------------------------------- | ----------------------------------------- | ------------------------------- |
| **P0**                                           | Validate          | 15 phỏng vấn, 3 design partner, baseline TTRC                                                     | Bắt đầu ngay                              | 0 code (trừ prototype identity) |
| **P1**                                           | Report Center v0  | Ingestion API, Reporter SDK, test identity v1, artifact, dashboard v0, trace viewer, authN + RBAC | **Gate-0 pass**                           | **6,5–11,5** (§4.2)             |
| **P1.5**                                         | AI Triage v0      | Hypothesis + accept/reject + TriageEvent + đo TTRC                                                | **Gate-1 pass**, schema đứng yên ≥ 2 tuần | 0,5–1                           |
| **P2**                                           | Test Management   | Manual test, import TestRail/Xray/Excel                                                           | Gate-1.5 pass **và** dữ liệu khách ủng hộ | 2–4                             |
| **P3**                                           | Scheduler & Agent | Local agent (pull-only), scheduler, CI ingestion, notification, **Safety Guard (QĐ-8)**           | Khách trả tiền yêu cầu scheduling         | 3–5                             |
| **P4**                                           | AI nâng cao       | Smart triage, phân cụm failure, gợi ý fix                                                         | `acceptance_rate` v0 ≥ 60% ổn định        | 2–3                             |
| **P5**                                           | Mở rộng           | Visual regression, performance, security, distributed runner                                      | Chỉ khi có cầu trả tiền                   | —                               |
| **P6**                                           | Enterprise        | SSO, RBAC nâng cao, audit log, plugin marketplace, on-prem                                        | Hợp đồng enterprise cụ thể                | —                               |

**Thay đổi so với v3.0:** local agent P1 → **P3** (QĐ-1); AI triage tách khỏi P4 thành **P1.5** (QĐ-4); managed agent **loại** khỏi 12 tháng đầu (QĐ-5); Safety Guard P1 → **P3** (QĐ-8, đã chốt).

### 4.2 Phạm vi P1 thật — gom từ 5 nơi (sửa N02)

Bản v3.1 ghi P1 = "4–6 người-tháng" cho một dòng liệt kê ngắn. Nhưng phạm vi P1 thật nằm rải khắp các tài liệu khác. Đây là danh sách đầy đủ:

| **Khối** | **Nguồn yêu cầu** | **Người-tháng** |
| ------------------------------------------------------------------ | --------------------------- | ------------ |
| Ingestion API (§7.2)                                               | §7                          | 0,5–1        |
| Playwright Reporter SDK (§7.5)                                     | §7                          | 0,5–1        |
| Test identity v1 + resolver (§9)                                   | §9                          | 1,5–3        |
| **Merge / Split / Review Queue UI**                                | §9.4 — *"bắt buộc có ở P1"* | 0,5–1        |
| Artifact pipeline: cap, chunked, retention job                     | §7.7, §10.4                 | 1–1,5        |
| **Xử lý client-side cho 5 loại artifact** (nén, cắt, strip)        | §10.4                       | 0,3–0,5      |
| Dashboard v0: run list, test history, flaky view                   | §4.3                        | 1,5–2        |
| Trace viewer nhúng                                                 | §4.3                        | 0,5–1        |
| **Màn hình EM**                                                    | §2.5 — *"phải có"*          | 0,3–0,5      |
| **AuthN đầy đủ**: email/pass + OAuth + verify + MFA TOTP + session | §11.2                       | 0,5–1        |
| **RBAC 6 role + ma trận quyền + test cross-tenant**                | §11.3                       | 0,5–1        |
| **Tổng**                                                           |                             | **6,5–11,5** |

Sáu khối in đậm là những khối **không có trong dòng roadmap gốc** nhưng lại được các tài liệu khác tuyên bố "bắt buộc có ở P1". Đây là nguồn gốc của sai lệch ước lượng.

### 4.3 Timeline — tách theo scope để không vượt capacity

**Phase 0 — tuần 1–6** (cố định)

| Tuần | Việc | Deliverable kiểm chứng được |
|---|---|---|
| 1–6 | Phỏng vấn, tuyển partner, đo baseline TTRC | 15 notes; 3 cam kết; baseline đủ điều kiện §5.4 |
| 3–4 | Prototype test identity — ngoại lệ code duy nhất | ≥ 500 test result thật; precision/recall |
| 6 | **Gate-0** | Biên bản pass/kill có chữ ký |

Sau Gate-0, timeline phụ thuộc scope đã chọn. **Không được dùng cùng một lịch 12 tuần cho cả P1-min và P1-full.**

#### Track A — P1-min (mục tiêu 12 tuần, phù hợp 2 người full-time)

- Tuần 7–9: Ingestion API + schema + email/password + tenant isolation.
- Tuần 9–12: Reporter SDK + artifact pipeline tối thiểu.
- Tuần 12–16: Dashboard v0 + history/flaky + trace.
- Tuần 15–18: identity operations dạng script/CSV + 3-role RBAC + hardening.
- Cuối tuần 18: **Gate-1**, nếu đủ mọi điều kiện §4.5.

Capacity: 2 người × 12 tuần ≈ 6 người-tháng, khớp P1-min 4–6 người-tháng.

#### Track B — P1-full (6,5–11,5 người-tháng)

Với **2 người full-time**, duration thực tế theo capacity là khoảng **14–25 tuần sau Gate-0** (chưa tính buffer tuyển dụng/leave). Gate-1 diễn ra ở **cuối scope P1 thực tế**, không hardcode tuần 18.

Trình tự bắt buộc vẫn giữ:

1. Ingestion/schema/authN nền.
2. Reporter + artifact.
3. Test identity + dashboard/history.
4. Merge/Split/Review Queue UI + full RBAC + EM screen + hardening.
5. ≥ 2 tuần usage thật của cả 3 partner trước Gate-1.

> Nếu management yêu cầu “Gate-1 ở tuần 18” thì đó **chính là quyết định chọn P1-min**, không phải ép P1-full vào lịch ngắn hơn estimate.

**Phase 1.5 — 6 tuần sau Gate-1**

- Tuần G1+1 → G1+3: AI Triage v0 + instrumentation.
- Tuần G1+3 → G1+6: vận hành, đủ sample, đo TTRC/decision/coverage.
- Cuối G1+6: **Gate-1.5**.

**Kết luận về 90 ngày:** P0 + P1 + P1.5 không thể nằm trong 90 ngày với scope hiện tại. Nếu deadline 90 ngày là ràng buộc cứng, phải đổi mục tiêu từ “P0→P1.5” thành một milestone hẹp hơn, ví dụ “Gate-0 + ingestion/reporter alpha”.

### 4.4 P1-min — nếu bắt buộc phải vừa 12 tuần

Cắt về đúng lõi chứng minh giá trị. Ước lượng sau cắt: **4–6 người-tháng** (chính là con số v3.1 đưa ra — nó đúng cho phạm vi này, không đúng cho phạm vi §4.2).

| **Cắt khỏi P1** | **Thay bằng** | **Dời sang** |
| --------------------------------- | ------------------------------------------------- | ---------- |
| OAuth Google/GitHub               | Chỉ email + password                              | P2         |
| MFA TOTP                          | —                                                 | P2         |
| RBAC 6 role                       | 3 role: `admin` / `member` / `viewer`             | P2 mở rộng |
| Merge/Split **UI**                | Script vận hành maintainer chạy tay, có ghi audit | P2         |
| Review Queue **UI**               | Export CSV + import quyết định                    | P2         |
| Màn hình EM                       | —                                                 | P1.5       |
| Xử lý client-side 5 loại artifact | Chỉ video + trace                                 | P2         |

**Không được cắt, dù có gì xảy ra:** test identity + resolver (không có nó thì không có sản phẩm), artifact cap phía server (không có nó thì lỗ tiền), tenant isolation + test cross-tenant (không có nó thì rò dữ liệu giữa khách).

### 4.5 Gates

#### Gate-0 — cuối tuần 6

**PASS khi đủ cả 6:**

1. ≥ 15 phỏng vấn ICP hoàn thành, ghi chú theo mẫu §5.3.
2. 3 design partner ký cam kết (§5.5), có bộ E2E thật chạy ≥ 3 lần/tuần.
3. Baseline TTRC **tiến cứu đủ 2 tuần cho cả 3 partner**; có `n`, median, p90, `% rerun-cho-qua`.
4. **H1:** pain “không biết fail vì sao / flaky” nằm top-2 của ≥ 8/15.
5. **H2:** median baseline TTRC ≥ 20 phút/failure ở ≥ 2/3 partner.
6. **H3:** ≥ 5/15 người nói được **một con số ngân sách hoặc ngưỡng giá** họ/sếp có thể duyệt cho bài toán này, kèm quy trình ai duyệt.

**KILL / PIVOT:**

- Thiếu điều 2 → thi hành QĐ-6.
- H1 trượt nhưng manual+automation là top-1 của ≥ 8/15 → lật wedge QĐ-2 và viết lại §2/§4 trước build.
- H2 trượt → không build wedge “giảm TTRC” chỉ vì partner lịch sự đồng ý.
- H3 trượt → chưa có bằng chứng commercial; không mở P1-full. Muốn tiếp tục chỉ được làm P1-min/prototype nếu kill-gate owner ghi rõ lý do và giới hạn ngân sách.

#### Gate-1 — cuối scope P1 đã chọn

**PASS khi đủ cả 4:**

1. Cả 3 partner đẩy report từ dự án thật ≥ 2 tuần liên tục, ≥ 3 run/tuần/partner.
2. Test identity v1: `precision ≥ 0.98`, `recall ≥ 0.80`, đo trên ≥ 200 quyết định match thủ công cùng phương pháp §9.6.
3. ≥ 2/3 partner trả lời “có” cho câu: *“Nếu ngày mai dịch vụ tắt, công việc của anh/chị có bị ảnh hưởng không?”*
4. Chụp baseline **trước AI** cho `triage_coverage` và `untriaged_rate`; đây là mốc so sánh Gate-1.5.

**KILL:** < 2 partner còn dùng thật tại Gate-1 → quay lại QĐ-2, không mở P1.5.

#### Gate-1.5 — 6 tuần sau Gate-1

**PASS khi đủ cả 4:**

1. **Ít nhất 2/3 partner** giảm median TTRC ≥ 30% so với baseline P0. Không pool tất cả failure thành một median vì partner volume lớn sẽ lấn partner nhỏ.
2. `hypothesis_acceptance_rate ≥ 40%`.
3. `hypothesis_decision_rate ≥ 60%` và rolling 14 ngày có ≥ 30 quyết định; nếu không → `insufficient_data`.
4. `triage_coverage ≥ 70%` để TTRC không đẹp giả vì chỉ tính các failure dễ.

**KILL cho AI, không kill dự án:** acceptance < 40% ở hai cửa sổ đủ sample → AI về opt-in; Report Center vẫn đi tiếp nếu Gate-1 đã chứng minh giá trị.

#### Gate-2 trở đi

Chỉ viết chi tiết khi gate trước pass. Mỗi gate phải có: (a) số định lượng, (b) người có quyền kill, (c) hành động cụ thể khi trượt.

### 4.6 Nguyên tắc thi công

1. **Prototype rủi ro cao nhất trước** — test identity đi trước dashboard.
2. **Schema đóng băng trước khi build AI** — P1.5 chỉ bắt đầu khi §8 không có migration mới trong 2 tuần.
3. **Boring stack** — Postgres + object storage (S3/R2) + một backend monolith + queue. Không K8s, không microservices, không Kafka. Nâng cấp khi có số đo, không khi có cảm giác.
4. **Mọi tuần đều ship cho partner** — không sprint nào kết thúc bằng "demo nội bộ".

### 4.7 Quy tắc tài liệu đối ngoại

1. **Không cắt roadmap.** Bản tóm tắt/deck được rút gọn nhưng phải hiện đủ P0–P6; P5–P6 gắn nhãn *"Vision 3–5 năm, gated by demand"*. Cắt hẳn tạo cảm giác sản phẩm không có đường lên enterprise.
2. **Một nguồn sinh nhiều định dạng.** File `SPEC.md` là gốc; `.docx`/`.pptx` sinh bằng pipeline (pandoc hoặc script). **Cấm copy-paste tay Markdown vào Word** — đó là nguyên nhân của lỗi `**` literal đang còn trong bản hiện tại.
3. **Checklist trước khi gửi ra ngoài:**
   -  Không còn ký tự `**`, `#`, ` ``` ` literal trong bản Word/PowerPoint
   -  MVP mục #1 là **Reporter SDK**, không phải Local Agent
   -  Roadmap hiện đủ P0–P6
   -  **Đã xóa "Production Safety Guard" khỏi mục MVP** (QĐ-8)
   -  Không có khẳng định `[CHƯA KIỂM CHỨNG]` nào về đối thủ
   -  Ghi version + ngày, trỏ về commit của `SPEC.md`
4. Cho đến khi checklist trên xanh hết: **ba file trong ****`docs/v3.0/`**** bị cấm gửi ra ngoài.**

### 4.8 Khả thi theo quy mô đội

| **Quy mô** | **Phạm vi khả thi** | **Thời gian** |
| ---------------------------------- | ----------------------------- | ------------------------------------------------------ |
| 1 người / part-time                | P0 + **P1-min** reporter-only | 4–6 tháng                                              |
| 1 người full-time                  | P0 + P1 đầy đủ                | 8–12 tháng                                             |
| 2–3 người full-time                | P0 + P1 + P1.5                | 6–9 tháng                                              |
| 2–3 người full-time                | Thêm agent cơ bản (P3 sớm)    | 9–12 tháng                                             |
| < 5 người                          | Toàn bộ P0–P6                 | Không khả thi trong 1–2 năm. P5–P6 là tầm nhìn 3–5 năm |

---

## 5. Phase 0 Playbook

> Mục tiêu: chứng minh hoặc bác bỏ 3 giả thuyết bằng dữ liệu, trong 6 tuần, **không viết code sản phẩm**.

### 5.1 Ba giả thuyết phải kiểm chứng

| **#** | **Giả thuyết** | **Cách đo** | **Ngưỡng chấp nhận** |
| -------------------------------------- | ------------------------------------------------------------ | ------------------------------------------------- | ------------------------------------------ |
| H1                                     | Pain "không biết fail vì sao / flaky" đủ nặng và đủ tần suất | Pain ranking trong phỏng vấn                      | Top-2 của ≥ 8/15 người                     |
| H2                                     | TTRC hiện tại đủ lớn để đáng tiền                            | Đo baseline (§5.4)                                | Trung vị ≥ 20 phút/failure ở ≥ 2/3 partner |
| H3                                     | ICP kéo được ngân sách                                       | Hỏi thẳng ngân sách + quy trình duyệt (B5–B6 + D3) | ≥ 5/15 nói được con số/ngưỡng họ hoặc sếp có thể duyệt |

### 5.2 Tuyển 15 người phỏng vấn

- **Screener** (đủ cả 3 mới tính vào 15): (1) đang trực tiếp maintain bộ Playwright/Cypress ≥ 50 test; (2) bộ test chạy ≥ 3 lần/tuần trên CI; (3) team ≥ 3 người đọc kết quả test.
- **Kênh** (VN/SEA theo QĐ-3): cộng đồng Testing VN (Facebook/Discord/Telegram QA), meetup, LinkedIn outreach cá nhân hóa, mạng lưới cựu đồng nghiệp/học viên. Nhịp mục tiêu: **5 booking/tuần từ tuần 1**.
- **Không tính vào 15:** người chỉ làm manual testing (trừ khi kiểm chứng nhánh wedge B), agency demo, người không xem CI hàng tuần.

### 5.3 Kịch bản phỏng vấn (45 phút, bán cấu trúc)

> Nguyên tắc: hỏi về **quá khứ cụ thể**, không hỏi "anh/chị có muốn tính năng X không" — ai cũng nói muốn, đó là dữ liệu rác.

**Phần A — Bối cảnh (5')**

- A1. Bộ test hiện tại: framework, số test, tần suất chạy, chạy ở đâu?
- A2. Ai xem kết quả? Bằng công cụ gì?

**Phần B — Pain thật (25')**

- B1. *"Kể tôi nghe lần ****gần nhất**** một test fail mà anh/chị phải điều tra. Chuyện gì xảy ra, từng bước?"* — đào: mở gì, hỏi ai, mất bao lâu, kết luận là gì.
- B2. *"Lần đó mất bao lâu từ lúc thấy fail đến lúc biết nguyên nhân?"* — ghi con số, kể cả ước lượng.
- B3. *"Tháng qua có test nào fail đi fail lại mà team kết luận 'chắc flaky, rerun đi' không? Bao nhiêu cái? Ai quyết định đó là flaky, dựa vào gì?"*
- B4. *"Có lần nào rerun cho qua rồi hóa ra là bug thật lọt ra production không?"* — **câu ăn tiền**; nếu có, hỏi thiệt hại.
- B5. *"Team đang trả tiền cho công cụ QA/CI nào? Bao nhiêu/tháng?"*
- B6. *"Lần gần nhất team mua một tool mới: ai đề xuất, ai duyệt, mất bao lâu?"*
- B7. **Pain ranking** — đưa 5 thẻ, nhờ xếp hạng: (i) không biết fail vì sao · (ii) flaky làm mất niềm tin vào CI · (iii) quản lý manual + automation song song · (iv) viết test mới tốn công · (v) chạy test quá chậm. *Thẻ (iii) chính là phép thử wedge B của QĐ-2.*
- B8. *“Hiện team dùng gì để xem/triage kết quả automation? Nếu là Currents/ReportPortal/Allure/Datadog, điều gì vẫn khiến anh/chị phải mở thêm CI log/Slack/Jira?”* — dùng để kiểm chứng wedge cạnh tranh, không pitch.

**Phần C — Phản chứng (10')**

- C1. *"Playwright report + trace viewer đang thiếu gì với anh/chị?"* — nếu trả lời "đủ dùng", đào tiếp bằng §2.3; nếu vẫn đủ → **ghi nhận, không tranh cãi**. Đây là dữ liệu H1 âm.
- C2. *"Nếu có công cụ trả lời được [pain #1 của họ], nó phải làm được tối thiểu gì thì mới đáng cài thêm?"*

**Phần D — Chốt (5')**

- D1. Mời làm design partner (đọc nguyên văn cam kết §5.5).
- D2. Xin giới thiệu 2 người cùng vai trò.
- D3. Sau khi đã hỏi pain và buying process, cho xem **một bảng giá ngắn** §10.5 rồi hỏi: *“Ở mức nào anh/chị có thể tự duyệt / cần xin sếp / chắc chắn không mua?”* Ghi số và lý do. Không dùng câu trả lời này thay cho hành vi mua thật.

### 5.4 Đo baseline TTRC

**Định nghĩa đo** (khớp §8.3): TTRC của một failure = thời điểm team **chấp nhận** một giả thuyết nguyên nhân − thời điểm failure **được hệ thống/CI surface cho team**. Ở P0, mốc đầu lấy từ CI notification/report timestamp; sau khi dùng platform, mốc chuẩn là `TestResult.failureObservedAt` do server ghi khi nhận failure đầu tiên. Không dùng clock của máy runner làm nguồn sự thật metric.

Dùng **cả hai** phương pháp cho mỗi partner:

1. **Hồi cứu** (tuần đầu hợp tác): cùng partner mở 10 failure gần nhất trong CI history + Slack/Jira, dựng lại timeline từng cái. Nhanh, nhưng có recall bias — ghi rõ nhãn `retrospective`.
2. **Tiến cứu** (2 tuần): partner log mỗi failure điều tra vào form 4 ô — `thời điểm thấy fail` / `thời điểm có kết luận` / `kết luận (category theo §8.7)` / `có rerun-cho-qua không`. Nhắc qua Slack 2 lần/tuần. **Đây là số baseline chính thức.**

**Output bắt buộc:** bảng mỗi partner gồm `n failure`, `trung vị TTRC`, `p90 TTRC`, `% rerun-cho-qua`. Không có bảng này = Gate-0 chưa đủ điều kiện pass, kể cả đã đủ 3 partner.

### 5.5 Mẫu cam kết Design Partner

> **Cam kết Design Partner — [Tên platform]**
>
> **Bên A (platform) cam kết:** (1) miễn phí sử dụng ≥ 12 tháng; (2) hỗ trợ trực tiếp qua kênh riêng, phản hồi < 1 ngày làm việc; (3) không sử dụng dữ liệu test của Bên B ngoài mục đích vận hành dịch vụ và cải thiện sản phẩm, không chia sẻ cho bên thứ ba, xóa toàn bộ theo yêu cầu trong 7 ngày; (4) ưu đãi giá sáng lập nếu chuyển sang trả phí.
>
> **Bên B (design partner) cam kết:** (1) tích hợp reporter vào ≥ 1 dự án thật chạy ≥ 3 lần/tuần; (2) call phản hồi 30 phút/tuần trong 12 tuần; (3) cho phép đo baseline TTRC theo §5.4; (4) phản hồi thẳng — chê được hoan nghênh hơn khen.
>
> Hai bên có quyền dừng bất cứ lúc nào bằng thông báo email; điều khoản dữ liệu (A.3) vẫn hiệu lực sau khi dừng.

### 5.6 Vận hành 6 tuần

| Tuần | Việc chính | Cờ đỏ cần phản ứng |
|---|---|---|
| 1 | Outreach ≥ 30 contact, 3–5 phỏng vấn | < 3 booking → đổi kênh ngay |
| 2 | 5 phỏng vấn; partner #1 ký; bắt đầu baseline #1 | Không ai chịu ký dù pain cao → dữ liệu H1 âm |
| 3 | 5 phỏng vấn; partner #2 ký; baseline #2; prototype identity | Ứng viên partner không qua screener → không tính |
| 4 | Phỏng vấn bù; **partner #3 phải ký chậm nhất cuối tuần 4**; baseline #3 bắt đầu | Nếu chưa có #3 cuối tuần 4, Gate-0 tuần 6 có nguy cơ toán học không đủ 2 tuần baseline |
| 5 | Duy trì baseline cả 3; tổng hợp H1/H2/H3 + competitor tool usage | H2/H3 yếu → chuẩn bị kill/pivot, không “làm đẹp” dữ liệu |
| 6 | Đủ 14 ngày baseline #3; họp Gate-0 theo 6 điều kiện §4.5 | Bất kỳ điều kiện nào trượt → thi hành nhánh kill/pivot; extension chỉ một lần và phải do kill-gate owner ký |

**Bất biến lịch:** ký partner #3 ở tuần 5 **không còn hợp lệ**, vì baseline tiến cứu yêu cầu 2 tuần mà Gate-0 ở cuối tuần 6.

### 5.7 Anti-pattern bị cấm

- Viết code sản phẩm "tranh thủ" — ngoại lệ duy nhất: prototype identity tuần 3–4.
- Demo mockup rồi hỏi "anh/chị có dùng không" và tính câu trả lời "có" là validation.
- Tính bạn bè nể nang vào 3 design partner — partner phải qua screener §5.2 như mọi người.
- Đổi câu hỏi phỏng vấn giữa chừng khiến 15 mẫu không so được với nhau. Được phép **thêm** câu, không được sửa/bỏ câu B1–B7.

---

## 6. Kiến trúc hệ thống

### 6.1 Thành phần

Platform

Phía khách hàng

P3

CI / máy dev
chạy Playwright

Reporter SDK
(P1)

Local Agent
(P3)

API Server
/v1

Identity Resolver

Postgres

Object Storage
S3 / R2

Dashboard

AI Triage
(P1.5)

| **Thành phần** | **Phase** | **Trách nhiệm** |
| ------------------------------ | ---- | ------------------------------------------------------------------------- |
| **Reporter SDK**               | P1   | Thu kết quả từ Playwright, tính fingerprint, batch event, upload artifact |
| **API Server**                 | P1   | AuthN/AuthZ, ingestion, artifact, query báo cáo                           |
| **Identity Resolver**          | P1   | `clientTestId` → `stableTestId` (§9)                                      |
| **Dashboard**                  | P1   | Run list, test history, flaky view, trace viewer, triage UI, màn hình EM  |
| **AI Triage**                  | P1.5 | Sinh hypothesis, ghi `TriageEvent`                                        |
| **Local Agent**                | P3   | Thực thi test theo job, pull-only                                         |

### 6.2 Nguyên tắc kiến trúc giữ từ v3.0

1. **Agent pull-only** — agent luôn gọi ra, server không bao giờ gọi vào. Không mở port inbound trong mạng khách. *(Giữ nguyên, đúng.)*
2. **Framework là engine, không phải sản phẩm** — Playwright/Cypress/Jest/Pytest là engine. Platform điều phối và báo cáo, không thay thế.
3. **AI đề xuất, người duyệt** — AI không bao giờ tự sửa test theo cách che giấu regression thật.
4. **Report Center nhận từ mọi nguồn** — không chỉ từ automation: kết quả thủ công (P2), CI ingestion (P3), import lịch sử (P2) đều phải vào chung một schema.

### 6.3 Multi-tenancy

```
Organization → Workspace → Project → Environment → Run

```

Mỗi tenant phải có: cô lập dữ liệu, kiểm soát truy cập, audit, chính sách truy cập artifact, cấu hình retention. Chi tiết quyền ở §11.3, token scope ở §8.8.

### 6.4 Observability (khôi phục — N08)

> Mục này biến mất hoàn toàn khỏi v3.1. Nó không phải nice-to-have: **Gate-1 phụ thuộc vào việc partner đẩy report thành công liên tục 2 tuần**, mà không có gì theo dõi tỉ lệ lỗi ingest thì không biết mình đang trượt cho đến khi partner bỏ đi.

| **Nhóm** | **Chỉ số bắt buộc từ P1** | **Ngưỡng cảnh báo** |
| -------------------------------------------- | ------------------------------------------------------------------ | ----------------------- |
| **Ingestion**                                | Tỉ lệ lỗi `POST /v1/runs` và `/events` theo project                | > 1% trong 15 phút      |
|                                              | Số run mở quá 6 giờ bị auto-close                                  | > 2/ngày/project        |
|                                              | Độ trễ resolver `clientTestId → stableTestId` (p50, p99)           | p99 > 5 s               |
| **Artifact**                                 | Tỉ lệ upload thất bại, tỉ lệ checksum mismatch                     | > 2%                    |
|                                              | Số artifact ở trạng thái `unlinked` quá 1 giờ                      | > 0                     |
| **Identity**                                 | Số identity mới tạo/ngày/project (đột biến = dấu hiệu vỡ identity) | > 3× baseline tuần      |
|                                              | Độ dài Review Queue                                                | > 50 cặp chờ            |
| **Chi phí**                                  | Dung lượng storage theo org; biến phí/org so với doanh thu         | > 40% doanh thu (§10.3) |
|                                              | Số hypothesis LLM/ngày/org                                         | chạm trần gói           |
| **Sản phẩm** | `untriaged_rate`, `triage_coverage`, `hypothesis_acceptance_rate`, `hypothesis_decision_rate` | acceptance < 40%; decision rate < 60%; coverage < 70% ở cửa sổ Gate-1.5 |
| **Hạ tầng**                                  | Lỗi API 5xx, độ trễ p99, độ trễ queue                              | theo chuẩn thường       |

Mọi log có correlation id. **Không** log token, password, hay nội dung artifact (§11.5).

### 6.5 Versioning API

Mọi API công khai có version: `/v1/runs`, `/v1/artifacts`, `/v1/agent/...`. Reporter SDK và agent protocol gửi kèm `X-Protocol-Version`. Hỗ trợ N và N−1; N−2 trả `426 Upgrade Required`.

---

## 7. Ingestion API & Agent Protocol

> **Phần A (§7.1–7.7) là hợp đồng của P1 — build ngay.** **Phần B (§7.8–7.9) là thiết kế P3 — giữ để không thiết kế P1 chặn đường P3, chưa build.**

### 7.1 Nguyên tắc giao thức

1. **Idempotent by default** — mọi request ghi mang idempotency key; server dedupe, retry của client không bao giờ nhân đôi dữ liệu.
2. **Client không cần biết ID của server tại thời điểm phát sự kiện** — mọi sự kiện khóa theo định danh client tự tính (`localRunId`, `clientTestId`, `attempt`); server tự resolve (§7.6).
3. **Server quyết định trần** (kích thước, rate, page size); client chỉ xin trong trần.
4. **Versioning** — prefix `/v1/`, header `X-Protocol-Version`.
5. **Error format thống nhất** — `{"error": {"code": "RUN_NOT_FOUND", "message": "...", "details": {}}}`. Code máy đọc được, không leak stack trace/SQL.

### 7.2 Endpoints (P1)

**Xác thực:** header `Authorization: Bearer <ingest_token>`. Scope tối thiểu `project:ingest` trên đúng 1 Project. Sai/thiếu token → `401`; token đúng nhưng sai project → `404` (không lộ tồn tại của project khác).

| **#** | **Endpoint** | **Mục đích** | **Idempotency** |
| -------------------------------- | ------------------------------------------ | ------------------------------------------ | ------------------------------------------------------------- |
| 1                                | `POST /v1/runs`                            | Khai báo run mới                           | `localRunId` (UUID reporter sinh) — gửi lại trả run cũ, `200` |
| 2                                | `POST /v1/runs/{localRunId}/events`        | Batch sự kiện test (≤ 500 event hoặc 5 MB) | `(localRunId, event.sequence)`                                |
| 3                                | `POST /v1/runs/{localRunId}/finish`        | Đóng run, gửi tổng kết                     | Gọi lần 2 → `200`, không đổi trạng thái                       |
| 4                                | `POST /v1/artifacts`                       | Khai báo artifact + xin URL upload         | `(localRunId, clientTestId, attempt, artifactName)`           |
| 5                                | `PUT <presigned-url>` / chunked            | Upload nội dung                            | Theo cơ chế storage                                           |
| 6                                | `POST /v1/artifacts/{artifactId}/complete` | Chốt upload, gửi checksum                  | `artifactId`                                                  |

### 7.3 Payload

`POST /v1/runs`:

```json
{
  "localRunId": "0198c3e1-...",
  "environmentKey": "staging",
  "trigger": {"type": "ci", "provider": "github-actions", "ref": "refs/heads/main",
              "commitSha": "ab12...", "ciRunUrl": "https://..."},
  "framework": {"name": "playwright", "version": "1.46.0"},
  "reporter": {"name": "@platform/playwright-reporter", "version": "0.3.1", "protocolVersion": "1.1"},
  "startedAt": "2026-08-24T02:11:04.120Z"
}

```

Response `201`: `{"runId": "run_9f2...", "limits": {...}, "identityMap": {}}`. `runId` do server cấp, reporter lưu nhưng **không bắt buộc dùng** — mọi call sau khóa theo `localRunId`. `limits` là trần artifact (§10.4) — reporter đọc từ đây, **không hardcode**.

Event trong batch — ba trường in đậm chính là chỗ v3.0 gãy:

```json
{
  "sequence": 42,
  "type": "test.finished",
  "clientTestId": "ct_5f0e...",
  "occurrenceIndex": 0,
  "attempt": 1,
  "identity": {
    "framework": "playwright",
    "filePath": "tests/checkout/payment.spec.ts",
    "titlePath": ["Checkout", "Payment", "declines expired card"],
    "paramSignature": "browser=firefox"
  },
  "status": "failed",
  "durationMs": 8412,
  "error": {"message": "...", "stack": "...", "snippet": "..."},
  "startedAt": "...", "finishedAt": "..."
}

```

- **`clientTestId`** — fingerprint reporter tự tính theo §9.2, deterministic, không cần mạng. Client không bao giờ cần biết `stableTestId`.
- **`occurrenceIndex`** — 0-based, phân biệt nhiều test **trùng fingerprint trong cùng một run** (§9.7 chấp nhận trường hợp này). Reporter đánh số theo thứ tự gặp. *(Sửa N03 — thiếu trường này thì unique constraint ở §8.4 làm hỏng ingest.)*
- **`attempt`** — 0-based, bắt buộc trên mọi event `test.*` và mọi artifact.
- **`identity`** — raw signals để server chạy matching; gửi kèm mỗi lần vì rẻ và giúp server tự phục hồi khi client tính fingerprint sai version.

Response batch: `{"accepted": 500, "identityMap": {"ct_5f0e...": "st_a91..."}}`. Reporter **có thể** cache để hiển thị link, nhưng giao thức không phụ thuộc vào việc này.

### 7.4 Trần tài nguyên (server-enforced)

| **Trần** | **Mặc định** | **Khi vượt** |
| ------------------------------- | ---------------------- | -------------------------------------------------- |
| Event/batch                     | 500 event hoặc 5 MB    | `413` + `BATCH_TOO_LARGE`                          |
| Rate/token                      | 60 req/phút, burst 120 | `429` + `Retry-After`                              |
| Kích thước artifact             | Theo loại — §10.4      | `413` tại bước khai báo, kèm limit trong `details` |
| Run mở quá 6 giờ không `finish` | —                      | Auto-close, status `error`, reason `RUN_TIMED_OUT` |

### 7.5 Yêu cầu với Reporter SDK

- Buffer + gửi batch mỗi **5 s hoặc 100 event**; retry exponential backoff (1/2/4/8 s, tối đa 5 lần) cho 5xx/timeout; **không retry** 4xx trừ 429.
- Mất mạng toàn bộ: ghi spool file local, in cảnh báo, **không làm fail test run của người dùng**. Reporter chết thì test vẫn xanh — **đây là bất biến sản phẩm, không được đánh đổi.**
- Timeout mọi request: 30 s.
- HAR strip `Authorization`/`Cookie` **phía client trước khi upload**, mặc định bật (§11.1).

### 7.6 Identity handshake

Vấn đề v3.0: client phát `testId`, schema khóa theo `stableTestId` server cấp — client không thể biết ID server tại thời điểm phát sự kiện. Giải pháp: **hai không gian ID, một chiều resolve.**

1. Client chỉ nói bằng `clientTestId` + `occurrenceIndex` + `attempt` + raw identity signals.
2. Server resolve `clientTestId → stableTestId` bằng §9, **bất đồng bộ với ingest**: event được nhận và lưu ngay theo `clientTestId`; resolver chạy trong pipeline ingest (p50 < 1 s) và backfill `stableTestId` vào `TestResult`.
3. Artifact tham chiếu `(localRunId, clientTestId, occurrenceIndex, attempt)` — server nối về `TestResultAttempt` sau khi resolve. Artifact đến **trước** event `test.finished` vẫn hợp lệ (out-of-order tolerant): giữ trạng thái `unlinked` tối đa 1 giờ rồi mới cảnh báo.
4. Reporter đổi version thuật toán fingerprint → raw signals cho phép server match tiếp mà không vỡ lịch sử (§9.5).

### 7.7 Artifact và upload

Metadata bắt buộc: `{localRunId, clientTestId, occurrenceIndex, attempt, name, kind, contentType, sizeBytes, sha256}`. `kind` enum: `screenshot | video | trace | har | log | attachment`.

**Quy kết theo attempt là bất biến** — video của attempt 0 (fail) và attempt 1 (pass) là hai artifact khác nhau. Chính chênh lệch giữa hai attempt là input đắt giá nhất của AI triage và flaky detection.

- `sizeBytes < 8 MB`: single `PUT` lên presigned URL (TTL 15 phút).
- `≥ 8 MB`: chunked — `POST /v1/artifacts` trả `uploadId` + danh sách presigned part URL (part 8 MB); client upload song song ≤ 4 part; `complete` gửi danh sách ETag + sha256 toàn file. Resume bằng cách gọi lại endpoint 4 với cùng idempotency key → server trả danh sách part đã nhận.
- Khi `complete`, server **HEAD object** để verify `sizeBytes` thực tế + content type policy, sau đó verify sha256; client khai nhỏ nhưng upload lớn không được vượt quota/cap. Lệch → `422 CHECKSUM_MISMATCH`/`SIZE_MISMATCH`, object bị xóa.
- Multipart upload chưa `complete` phải có lifecycle cleanup tự động (mặc định 24 giờ) để không tạo storage leak. Object key do server sinh; client không được điều khiển path/bucket.

### 7.8 Phần B — Agent Protocol (thiết kế P3)

**Mô hình:** pull-only. Agent long-poll `POST /v1/agent/jobs/claim` (giữ kết nối tối đa 30 s; không có job → `204`, poll lại ngay).

**Chốt: long polling, không WebSocket.** Lý do: đơn giản vận hành qua mọi proxy/firewall doanh nghiệp; nhu cầu latency của job scheduling là giây, không phải mili-giây. Điều kiện xét lại: poll chiếm > 5% chi phí hạ tầng, hoặc khách cần cancel latency < 5 s.

**Ánh xạ Job state ↔ Run status.** Nguyên tắc gốc: **Job là hợp đồng giữa server và agent; Run là sự thật nghiệp vụ cho người dùng.** Mọi job state phải chiếu về đúng một run status:

| **Job state** | **Nghĩa** | **Run status** | **Ghi chú hòa giải** |
| -------------------------------------------- | -------------------------------- | ----------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| `pending`                                    | Chờ agent nhận                   | `queued`                |                                                                                                                                |
| `claimed`                                    | Agent đã claim, chưa chạy        | `queued`                | **Sửa v3.0:** claimed KHÔNG phải running. Người dùng thấy "queued (claimed by agent-07)" qua `run.claimInfo`, không qua status |
| `running`                                    | Đang thực thi                    | `running`               |                                                                                                                                |
| `uploading`                                  | Test xong, đang đẩy artifact     | `running`               | Kết quả chưa đầy đủ thì run chưa kết thúc                                                                                      |
| `completed`                                  | Job xong trọn vẹn                | `passed` / `failed`     | Theo kết quả test                                                                                                              |
| `failed`                                     | Job gãy vì hạ tầng               | `error` + `errorReason` | Phân biệt "test fail" với "không chạy được"                                                                                    |
| `cancel_requested`                           | Server yêu cầu hủy, chờ ack      | `running`               | Trạng thái chuyển tiếp job-only; UI hiện badge "cancelling" từ `run.cancelRequestedAt`                                         |
| `cancelled`                                  | Agent ack hủy xong               | `cancelled`             |                                                                                                                                |
| `abandoned`                                  | Mất heartbeat quá `3 × interval` | `error` + `AGENT_LOST`  | **Sửa v3.0:** abandoned có đích đến rõ ràng. Job có thể re-queue thành job MỚI; mặc định run cũ đóng `error`, không resurrect  |

**Bất biến kiểm chứng được bằng test:** (1) không job state nào thiếu dòng trong bảng này; (2) run đã ở trạng thái kết thúc thì không job event nào đổi được nó; (3) `cancel_requested` quá 120 s không ack → server tự chuyển `abandoned`.

### 7.9 Bảng timeout chuẩn

| **Tham số** | **Giá trị** | **Ghi chú** |
| ------------------------- | ---------------- | ------------------------------------------------------------------------- |
| HTTP request timeout      | 30 s             | Mọi call                                                                  |
| Long-poll hold            | 30 s             |                                                                           |
| Claim TTL                 | 60 s             | Claim không chuyển `running` trong TTL → job về `pending`                 |
| Heartbeat interval        | 20 s             |                                                                           |
| Abandoned threshold       | 60 s             | `3 × heartbeat`                                                           |
| Cancel ack deadline       | 120 s            |                                                                           |
| Job hard timeout          | Mặc định 60 phút | Cấu hình theo job. Vượt → agent kill process, job `failed`, `JOB_TIMEOUT` |

### 7.10 Bảo mật giao thức

- Ingest token chỉ có `project:ingest` — **write-only by design**. Lộ trên CI log thì kẻ xấu chỉ ghi được report rác vào 1 project, không đọc được gì.
- Mọi endpoint list có pagination bắt buộc, max page size 100.
- Payload từ reporter là **untrusted input**: validate schema khai báo tại biên; strip HTML trong error message trước khi render — stack trace là vector XSS kinh điển của dashboard test.
- Server **không bao giờ fetch URL do client gửi** trong payload (vd `ciRunUrl`) — chỉ lưu và render như link. Chống SSRF.

---

## 8. Canonical Data Schema

> Quy ước: Postgres, mọi timestamp `timestamptz` UTC, khóa chính `uuid v7`. Bảng dưới ghi trường then chốt; `createdAt`/`updatedAt` mặc định có ở mọi bảng.

### 8.1 Sơ đồ quan hệ

Organization

Workspace

Project

Environment

TestIdentity

Run

TestResult

TestResultAttempt

Artifact

TriageEvent

AiHypothesis

FlakySignal

### 8.2 Phân cấp tổ chức

| **Entity** | **Trường then chốt** | **Ghi chú** |
| --------------------------------- | ----------------------------------------------- | ----------------------------------------------- |
| `Organization`                    | `id, name, slug, planId`                        | Đơn vị billing                                  |
| `Workspace`                       | `id, orgId, name`                               | Nhóm project theo phòng ban/sản phẩm            |
| `Project`                         | `id, workspaceId, name, defaultBranch`          | Đơn vị của ingest token và test identity        |
| `Environment`                     | `id, projectId, key` (`staging`, `prod-smoke`…) | Cùng test, khác environment = lịch sử tách nhau |

### 8.3 TriageEvent — nguồn sự thật duy nhất của TTRC

```
TriageEvent
  id              uuid PK
  testResultId    uuid FK → TestResult, NOT NULL, indexed
  runId           uuid FK → Run
  type            enum NOT NULL:
                    FAILURE_SEEN
                    HYPOTHESIS_PROPOSED
                    HYPOTHESIS_ACCEPTED
                    HYPOTHESIS_REJECTED
                    MARKED_FLAKY
                    MARKED_KNOWN_ISSUE
                    ASSIGNED
                    RESOLVED
  actorType       enum NOT NULL: USER | AI | SYSTEM
  actorId         uuid NULL
  hypothesisId    uuid NULL FK → AiHypothesis
  payload         jsonb
  occurredAt      timestamptz NOT NULL
```

**Mốc thời gian canonical:**

- `TestResult.failureObservedAt` = timestamp **server-side** lúc platform nhận attempt fail đầu tiên của TestResult. Ghi đúng một lần.
- `TestResult.firstFailedAt` vẫn giữ timestamp do runner gửi để forensic/ingest-lag, nhưng **không dùng làm nguồn sự thật của TTRC** vì clock runner có thể lệch.
- `ingestLagMs = failureObservedAt - clientFinishedAt` là observability, không phải KPI sản phẩm.

**Metric:**

- `TTRC = min(HYPOTHESIS_ACCEPTED.occurredAt) − failureObservedAt`.
- Failure chưa có `HYPOTHESIS_ACCEPTED` không đi vào median; phải xuất hiện trong `untriaged_rate`.
- `triage_coverage = failures có HYPOTHESIS_ACCEPTED / failures đủ điều kiện`.
- `time_to_first_view = first FAILURE_SEEN − failureObservedAt`.
- AI acceptance chỉ có ý nghĩa cùng `hypothesis_decision_rate`; hypothesis bị user bỏ qua không được vô hình khỏi denominator của quality reporting.

**UI bắt buộc phát event:** mở failure detail → `FAILURE_SEEN` (dedupe user+testResult/24h); Accept/Reject/Mark flaky/Assign phát event tương ứng. Không có đường cập nhật TTRC ngoài `TriageEvent`.

### 8.4 Run / TestResult / Attempt / Artifact

```
Run
  id            uuid PK
  environmentId uuid FK NOT NULL
  localRunId    uuid NOT NULL, UNIQUE(environmentId, localRunId)   -- idempotency
  status        enum: queued|running|passed|failed|error|cancelled  -- khớp §7.8
  flakyCount    int NOT NULL DEFAULT 0   -- QĐ-7 phương án B; derived từ finalStatus=flaky_pass
  errorReason   text NULL                -- AGENT_LOST, RUN_TIMED_OUT…
  trigger       jsonb;  commitSha text;  branch text
  claimInfo     jsonb NULL               -- luồng agent P3
  startedAt / finishedAt  timestamptz

TestResult                          -- một test trong một run (gộp mọi attempt)
  id              uuid PK
  runId           uuid FK NOT NULL
  stableTestId    uuid FK → TestIdentity NULL   -- backfill sau resolve (§7.6)
  clientTestId    text NOT NULL, indexed
  occurrenceIndex int NOT NULL DEFAULT 0        -- sửa N03
  displayName     text;  suiteName text
  finalStatus     enum NOT NULL: passed|failed|skipped|timed_out|interrupted|flaky_pass
  failureCategory enum NULL                     -- taxonomy §8.7
  attemptCount    int NOT NULL DEFAULT 1
  firstFailedAt   timestamptz NULL              -- client timestamp, forensic
  failureObservedAt timestamptz NULL               -- server timestamp, mốc gốc canonical của TTRC
  durationMs      int
  UNIQUE(runId, clientTestId, occurrenceIndex)  -- sửa N03

```

> **N03 — vì sao có ****`occurrenceIndex`****:** §9.7 chấp nhận rằng hai test khác nhau trùng cả file lẫn title sẽ sinh cùng fingerprint. Với `UNIQUE(runId, clientTestId)` như bản v3.1, hai test đó trong cùng một run sẽ **vi phạm ràng buộc** — test thứ hai bị từ chối hoặc đè lên test thứ nhất. Một "giới hạn chấp nhận được" ở tầng tài liệu biến thành **lỗi ingest cứng** ở tầng DB. Thêm `occurrenceIndex` giữ nguyên giới hạn (hai test vẫn chung `stableTestId`, lịch sử vẫn lẫn) nhưng không làm mất dữ liệu.

```
TestResultAttempt               -- attempt là entity, không phải cột đếm
  id            uuid PK
  testResultId  uuid FK NOT NULL
  attempt       int NOT NULL (0-based), UNIQUE(testResultId, attempt)
  status        enum: passed|failed|skipped|timed_out|interrupted
  error         jsonb           -- message, stack, snippet
  workerId      text NULL;  agentId uuid NULL
  startedAt / finishedAt;  durationMs int

Artifact
  id                  uuid PK
  testResultAttemptId uuid FK NULL   -- NULL khi unlinked (§7.6), job nền nối
  runId               uuid FK NOT NULL
  clientTestId        text;  occurrenceIndex int;  attempt int   -- khóa nối từ giao thức
  kind        enum: screenshot|video|trace|har|log|attachment
  name text;  contentType text;  sizeBytes bigint;  sha256 text
  storageKey  text
  status      enum: pending|uploaded|verified|expired|deleted
  expiresAt   timestamptz NOT NULL   -- retention §10.4, ghi tại lúc ghi-nhận

```

`expiresAt` được tính **tại thời điểm ghi nhận**, không tính lúc dọn — đổi policy chỉ ảnh hưởng artifact mới, khỏi tranh cãi hồi tố.

### 8.5 TestIdentity

```
TestIdentity
  id                 uuid PK
  projectId          uuid FK NOT NULL
  status             enum: active|merged|retired
  mergedIntoId       uuid FK → TestIdentity NULL
  canonicalTitlePath text[]
  canonicalFilePath  text
  paramSignature     text NULL
  templateKey        text
  currentFingerprint text NOT NULL, indexed
  firstSeenAt / lastSeenAt timestamptz
  UNIQUE(projectId, currentFingerprint) WHERE status = 'active'

TestIdentityAlias
  id          uuid PK
  identityId  uuid FK NOT NULL
  projectId   uuid FK NOT NULL
  fingerprint text NOT NULL
  source      enum: exact|auto_match|manual_merge|manual_split
  matchScore  numeric NULL
  createdBy   uuid NULL
  validFrom   timestamptz NOT NULL
  validTo     timestamptz NULL
  UNIQUE(projectId, fingerprint) WHERE validTo IS NULL
  INDEX(projectId, fingerprint, validFrom, validTo)
```

**Vì sao alias có validity interval:** split §9.4 cho phép chọn mốc T. Không có `validFrom/validTo`, một fingerprint chỉ có thể trỏ về một identity cho toàn bộ lịch sử nên “split từ thời điểm T” không implement được.

**Resolve realtime:** event mới chỉ tra alias `validTo IS NULL`. **Backfill lịch sử:** chọn alias có `eventTime ∈ [validFrom, validTo)`.

Rename = đóng/mở alias theo lịch sử nhưng vẫn giữ một current mapping. Merge/split phải ghi audit. Collision mà hai test thật **cùng fingerprint trong cùng thời điểm** vẫn là giới hạn v1; `occurrenceIndex` chống mất dữ liệu nhưng không tạo stable identity riêng cho collision đó (§9.7).

### 8.6 FlakySignal

```
FlakySignal              -- 1 dòng = trạng thái flaky hiện hành của 1 identity/environment
  id            uuid PK
  identityId    uuid FK NOT NULL
  environmentId uuid FK NOT NULL
  UNIQUE(identityId, environmentId)
  windowDays    int NOT NULL DEFAULT 30
  totalRuns     int;  failedRuns int;  flakyPassRuns int
  flipCount     int                    -- số lần đổi pass↔fail liên tiếp trong cửa sổ
  flakyScore    numeric(4,3) NULL      -- 0–1, NULL khi chưa đủ dữ liệu
  verdict       enum: insufficient_data|stable|suspect|flaky|quarantine_suggested
  computedAt    timestamptz

```

**Công thức** `[ASSUMPTION]` — hệ số khởi điểm, hiệu chỉnh bằng dữ liệu partner ở P1, mọi thay đổi ghi vào changelog schema:

```
if totalRuns < 2:                     -- sửa N05: chặn chia 0
    flakyScore = NULL
    verdict    = 'insufficient_data'
else:
    flakyScore = 0.5 × (flipCount / (totalRuns − 1))
               + 0.3 × (flakyPassRuns / totalRuns)
               + 0.2 × recencyWeight(fail gần nhất)

```

Ngưỡng verdict khởi điểm: `≥ 0.6` → `flaky`; `0.3–0.6` → `suspect`. Job nền tính lại mỗi khi run finish (incremental, không full scan).

`MARKED_FLAKY` của người (TriageEvent) là **nhãn giám sát** — lưu làm ground truth để hiệu chỉnh công thức, **không ghi đè** score.

### 8.7 Taxonomy `failureCategory` — enum đóng, dùng chung cho người và AI

| **Code** | **Nghĩa** | **Ví dụ tín hiệu** |
| --------------------------- | ---------------------- | -------------------------------------------------- |
| `app_bug`                   | Bug thật của ứng dụng  | Assertion nghiệp vụ fail ổn định sau deploy        |
| `test_bug`                  | Lỗi trong code test    | Selector sai, assertion sai sau đổi UI có chủ đích |
| `flaky_timing`              | Race/timing trong test | Fail ngẫu nhiên ở wait, pass khi retry             |
| `env_infra`                 | Môi trường / hạ tầng   | DNS, service phụ thuộc down, disk đầy              |
| `env_data`                  | Dữ liệu test           | Fixture hết hạn, seed đổi, tài khoản bị khóa       |
| `dependency_external`       | Bên thứ ba             | Sandbox payment lỗi, API ngoài 5xx                 |
| `unknown`                   | Chưa xác định          | Mặc định trước triage                              |

Query "failure category trends" chạy thẳng: `GROUP BY failureCategory, date_trunc('week', firstFailedAt)`.

```
AiHypothesis
  id uuid PK;  testResultId uuid FK
  category enum (§8.7);  confidence numeric CHECK (confidence BETWEEN 0 AND 1)
  evidence jsonb;  suggestedAction text
  model text;  promptVersion text;  costUsd numeric

```

Người accept → `TestResult.failureCategory` nhận giá trị của hypothesis.

### 8.8 Token scope

```
ApiToken
  id uuid PK
  tokenHash    text          -- HMAC-SHA-256(secret, serverPepper); token random high-entropy, không dùng Argon2 cho hot path
  scopeLevel   enum: organization|workspace|project|environment
  scopeId      uuid NOT NULL
  permissions  text[]        -- {project:ingest}, {project:read}…
  name text;  createdBy uuid
  lastUsedAt;  expiresAt;  revokedAt  timestamptz NULL

```

Chuỗi phân cấp đầy đủ: **Organization → Workspace → Project → Environment**. Run **không** phải tầng scope — run là dữ liệu, không phải ranh giới quyền. Token tầng trên bao trùm tầng dưới; kiểm tra quyền = walk-up cây bằng **một hàm duy nhất** `authorize(token, resource, permission)` dùng chung mọi endpoint (§11.3.3).

Ingest token mặc định: `scopeLevel = project`, `permissions = {project:ingest}` — least privilege, write-only.

### 8.9 Index tối thiểu cho query nóng

| **Query** | **Index** |
| ------------------- | ----------------------------------------------------------------- |
| Lịch sử một test    | `TestResult(stableTestId, createdAt DESC)`                        |
| Run list theo env   | `Run(environmentId, startedAt DESC)`                              |
| Resolve fingerprint | `TestIdentityAlias(projectId, fingerprint)` + partial unique §8.5 |
| TTRC dashboard      | `TriageEvent(testResultId, type, occurredAt)`                     |
| Dọn retention       | `Artifact(expiresAt) WHERE status = 'verified'`                   |
| Gộp parameterized   | `TestIdentity(templateKey)`                                       |

### 8.10 Quy tắc tiến hóa schema

- Mọi thay đổi = migration file, chạy được trên DB có dữ liệu. Expand → migrate → contract.
- Enum chỉ thêm giá trị qua migration có review; **không bao giờ đổi nghĩa giá trị cũ**.
- P1.5 chỉ bắt đầu khi mục này không có migration mới trong 2 tuần (§4.6).

---

## 9. Thuật toán Test Identity

> **Khối rủi ro cao nhất của sản phẩm.** Không có identity ổn định → không có lịch sử, không có trend, không có flaky, không có AI. Vì vậy nó được prototype ở **tuần 3–4 của Phase 0**, trước cả dashboard.

### 9.1 Bài toán và phạm vi v1

- **Input:** dòng event từ reporter, mỗi event mang `identity` raw `{framework, filePath, titlePath[], paramSignature}` + `clientTestId`.
- **Output:** mỗi event được gán về đúng một `stableTestId`, bền vững qua: đổi tên test, đổi tên/di chuyển file, thêm bớt describe block, thay đổi tham số hóa.
- **Ngoài phạm vi v1** (ghi rõ để khỏi phình): match xuyên framework (Playwright ↔ Cypress), match bằng AST của code test, match xuyên project.

### 9.2 Fingerprint (chạy ở client, deterministic)

```
normalizedFilePath = filePath
  → thay '\' bằng '/'
  → bỏ prefix root của repo (reporter tự phát hiện từ cwd/config)
  → lowercase phần đuôi mở rộng
normalizedTitle    = mỗi phần tử titlePath: trim, gộp whitespace liên tiếp thành 1 space
paramSignature     = key=value sort theo key, join bằng '&' (rỗng nếu không tham số hóa)

fingerprint  = "fp1:" + sha256(
                 framework + "\n" + normalizedFilePath + "\n" +
                 join(normalizedTitle, "") + "\n" + paramSignature)
clientTestId = "ct_" + first16bytes(fingerprint) (hex)

```

- Prefix `fp1:` là **version của thuật toán fingerprint** — đổi cách chuẩn hóa = tăng version; server match được cả hai đời nhờ raw signals (§9.5).
- **Không** đưa số thứ tự test, line number, hay thời gian vào fingerprint — chúng đổi liên tục, gây vỡ identity giả.

### 9.3 Pipeline resolve phía server

Có

Không

Có

Không

Có

Không

Event vào

Alias có fingerprint?

Gán stableTestId của alias

Lấy ứng viên cùng project

Chấm điểm từng ứng viên

maxScore >= 0.85?

Auto-match: thêm alias, gán identity

maxScore >= 0.60?

Tạo identity mới + đưa vào Review Queue

Tạo identity mới

Xong

**Bước 1 — Exact match (đường nóng, > 99% event).** Tra `TestIdentityAlias(projectId, fingerprint) WHERE validTo IS NULL`. Trúng → xong, O(1) theo partial index. Đây là lý do resolve chạy được inline trong pipeline ingest (p50 < 1 s).

**Bước 2 — Candidate set.** Fingerprint lạ → lấy `TestIdentity.status = active` cùng `projectId` thỏa **ít nhất một**: cùng `canonicalFilePath`; hoặc cùng leaf title; hoặc trigram similarity của leaf title ≥ 0.4 (`pg_trgm`). Giới hạn 50 ứng viên điểm thô cao nhất.

**Bước 3 — Chấm điểm.**

```
score = 0.45·S_title + 0.25·S_file + 0.20·S_param + 0.10·S_recency

```

| **Thành phần** | **Cách tính** |
| ----------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `S_title`               | Leaf giống hệt = 1.0; khác thì normalized Levenshtein trên leaf (trọng số 0.7) + Jaccard trên các describe cha (0.3)             |
| `S_file`                | Giống hệt = 1.0; cùng thư mục khác tên file = 0.6; cùng tên file khác thư mục (file bị move) = 0.7; còn lại = trigram similarity |
| `S_param`               | Giống hệt = 1.0; cùng tập key khác value = 0.5; khác tập key = 0                                                                 |
| `S_recency`             | `lastSeenAt` trong 7 ngày = 1.0, suy giảm tuyến tính về 0 ở 90 ngày                                                              |

`S_recency` có lý do rõ ràng: rename thường thấy ngay run kế tiếp — identity "biến mất" **đúng lúc** fingerprint lạ xuất hiện là tín hiệu mạnh.

**Tín hiệu tăng cường:**

- Trong **cùng run**, identity ứng viên **không** xuất hiện (đúng kịch bản rename) → `+0.05`.
- Ứng viên **vẫn** xuất hiện trong run này với fingerprint cũ (nghĩa là đây là test mới thật, không phải rename) → `score × 0.3`. Phạt nặng, chống gộp nhầm hai test khác nhau.

**Bước 4 — Ngưỡng.**

| **Điều kiện** | **Hành động** |
| --------------------------------------------------- | ---------------------------------------------------------------------------- |
| `score ≥ 0.85` **và** cách ứng viên #2 ≥ 0.10       | Auto-match: thêm alias `source = auto_match`, ghi `matchScore`               |
| `0.60 ≤ score < 0.85`, hoặc top-2 sát nhau (< 0.10) | Tạo identity mới **ngay** (không chặn ingest) + đẩy cặp vào **Review Queue** |
| `score < 0.60`                                      | Identity mới, không hỏi                                                      |

Ngưỡng `0.85`/`0.60` và các trọng số là `[ASSUMPTION]` khởi điểm — hiệu chỉnh ở prototype §9.6 bằng dữ liệu thật, ghi giá trị chốt vào changelog.

### 9.4 Công cụ thủ công (bắt buộc có ở P1)

Dù xấu cũng phải có — thuật toán **chắc chắn** sai một tỉ lệ, và không có đường undo thì lịch sử bẩn vĩnh viễn.

- **Merge:** chọn 2+ identity → gộp; mọi alias trỏ về identity đích; nguồn `status = merged, mergedIntoId = đích`; `TestResult` backfill bằng job nền; `TriageEvent` giữ nguyên (đã trỏ `testResultId`). Ghi audit `source = manual_merge, createdBy`.
- **Split:** chọn một alias tách khỏi identity → tạo identity mới. Nếu split toàn bộ lịch sử, reassign alias. Nếu chọn mốc T, đóng mapping cũ bằng `validTo=T`, tạo alias mới cùng fingerprint với `validFrom=T`, rồi backfill `TestResult` theo event time. Đây là **undo của auto-match sai**. Split **không giải được** hai test thật có fingerprint giống hệt và cùng tồn tại trong cùng giai đoạn; đó là giới hạn v1 §9.7.
- **Review Queue UI:** danh sách cặp nghi rename, hiển thị diff title/file/param + score, hai nút Merge / Keep separate. Mỗi quyết định lưu làm **ground truth** để hiệu chỉnh trọng số.

> Ở phương án **P1-min** (§4.4), ba mục trên có thể xuống dạng script + CSV, nhưng **không được bỏ hẳn**.

### 9.5 Parameterized test và tiến hóa thuật toán

**Parameterized:** mỗi tổ hợp param = một `TestIdentity` riêng (vì flaky thường chỉ ở một browser/tham số), NHƯNG các identity cùng `(filePath, titlePath)` khác `paramSignature` được nhóm bằng `templateKey = sha256(filePath + titlePath)`. Dashboard hiển thị gộp theo template, drill-down theo param. Không cần bảng mới, chỉ cần index (§8.9).

**Client nâng fingerprint ****`fp1 → fp2`****:** server thấy prefix lạ → chạy pipeline như fingerprint mới. Vì raw signals đi kèm mọi event (§7.3), exact-match theo `(filePath, titlePath, param)` chuẩn hóa lại server-side sẽ bắt được → thêm alias `fp2` vào identity cũ. **Kịch bản này phải có integration test riêng.**

**Server đổi trọng số:** chỉ ảnh hưởng match tương lai; không re-run quá khứ trừ khi chạy chiến dịch backfill có chủ đích.

### 9.6 Prototype tuần 3–4 — tiêu chí đạt

1. Chạy trên ≥ 500 test result thật từ ≥ 2 design partner (xin export JSON report Playwright).
2. Tự tạo bộ thử nghiệm rename: lấy 50 test thật, mô phỏng 5 kiểu đổi (đổi leaf title, đổi describe, move file, đổi cả hai, thêm param) → đo precision/recall của auto-match.
3. **Ngưỡng đạt: precision ≥ 0.98, recall ≥ 0.80.** Precision quan trọng hơn recall vì **gộp nhầm tệ hơn tách nhầm** — gộp nhầm làm bẩn lịch sử của hai test, tách nhầm chỉ tốn công review.
4. **Không đạt → đơn giản hóa:** v1 chỉ auto-match khi `score ≥ 0.95`, dồn phần còn lại vào Review Queue. Sản phẩm vẫn ship được với nhiều review thủ công hơn. Ghi kết quả vào biên bản Gate-0.

Cùng bộ chỉ số này được dùng lại ở Gate-1 (§4.5) — một định nghĩa duy nhất, không hai thước đo.

### 9.7 Edge case bắt buộc có test

| **Case** | **Hành vi đúng** |
| ----------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| Hai test trùng cả title lẫn file (framework cho phép) | Fingerprint trùng → cùng identity; `occurrenceIndex` chỉ chống mất dữ liệu trong run. **Không thể split bền vững thành hai stable identity ở v1** vì occurrence order có thể đổi. Ghi rõ giới hạn trong docs |
| Title chứa dữ liệu động (`"order #8412 succeeds"`)    | v1: hai identity — Review Queue sẽ đầy. Backlog v2: phát hiện pattern số/uuid trong title, chuẩn hóa thành placeholder                         |
| Xóa test rồi thêm lại sau 2 tháng                     | `S_recency` thấp → thường thành identity mới; nếu fingerprint giống hệt → exact match vẫn nối lại đúng (alias còn đó)                          |
| 10.000 test/run                                       | Bước 2–3 chỉ chạy cho fingerprint lạ; run bình thường có < 10 fingerprint lạ                                                                   |
| Hai run song song cùng gửi fingerprint lạ giống nhau  | `UNIQUE(projectId, fingerprint)` §8.5 + upsert `ON CONFLICT` → chỉ một identity được tạo, request kia nhận identity đó. **Test race bắt buộc** |

---

## 10. Mô hình chi phí, trần artifact và pricing

> Mọi con số tiền là `[ASSUMPTION]` theo đơn giá phổ biến 2026 — cập nhật theo nhà cung cấp thực tế trước khi chốt giá. **Cấu trúc** của mô hình là phần chốt cứng.

### 10.1 Nguyên tắc kinh tế

1. **Không tài khoản nào được lỗ biến-phí** (storage + egress + LLM) ở trạng thái dùng bình thường của gói.
2. Chi phí biến thiên phải bị chặn trần bằng **cơ chế sản phẩm** (cap, quota, retention), không bằng hy vọng.
3. Chọn storage có **egress rẻ/miễn phí** (Cloudflare R2 hoặc tương đương). Dashboard test là sản phẩm người dùng *xem lại* artifact nhiều lần — egress S3 chuẩn sẽ ăn hết margin.

### 10.2 Mô hình khối lượng — "team chuẩn" làm đơn vị tính

**Team chuẩn:** 300 test E2E, chạy 1 lần/đêm + 10 run nhỏ/ngày, fail rate 5%, retry 1 lần cho fail, bật video-on-fail và trace-on-retry.

| **Thành phần** | **Tính** | **/đêm** | **/tháng** |
| ---------------------------- | ------------------------------------------------ | ----------------- | ----------------- |
| Fail + retry                 | 300 × 5% = 15 fail → 30 attempt có artifact nặng |                   |                   |
| Video (cap 20 MB)            | 30 × \~12 MB                                     | 360 MB            | \~11 GB           |
| Trace (cap 50 MB)            | 15 × \~15 MB                                     | 225 MB            | \~7 GB            |
| Screenshot + log             | 300 × \~60 KB                                    | 18 MB             | \~0,5 GB          |
| Run nhỏ ban ngày             | \~30% khối đêm                                   | \~180 MB          | \~5,5 GB          |
| **Tổng ghi mới**             |                                                  | **\~0,8 GB/ngày** | **\~24 GB/tháng** |

Với retention §10.4: **storage thường trực ≈ 12–15 GB/team chuẩn**.

### 10.3 Biến phí / team chuẩn / tháng `[ASSUMPTION]`

| **Khoản** | **Đơn giá giả định** | **Thành tiền** |
| ----------------------------------- | ------------------------------------------------------------- | ----------- |
| Storage 15 GB                       | R2 $0.015/GB                                                  | $0.23       |
| Class-A/B operations (\~500k)       |                                                               | \~$0.50     |
| Egress xem lại artifact             | R2 = $0 — **chính là lý do chọn**                             | $0          |
| LLM triage                          | 15 fail mới/ngày × \~6k token in + 500 out ≈ $0.01/hypothesis | \~$4.50     |
| DB + compute phân bổ                |                                                               | \~$3–5      |
| **Tổng biến phí**                   |                                                               | **≈ $9–10** |

**List price target** của Team nên xấp xỉ ≥ 5× biến phí ở giả định hiện tại → ~$49/tháng. Đây là target margin cho giá niêm yết, **không phải** lý do cho phép discount xuống tùy ý; mọi discount vẫn phải qua contribution-margin floor §10.5.

**Kill-switch chi phí:**

- Alert khi biến phí một org > **40% doanh thu** org đó trong tháng (§6.4).
- LLM trần cứng: tối đa **200 hypothesis/org/ngày** (Free: 20).
- Fail trùng fingerprint trong cùng ngày **không chạy lại** hypothesis (QĐ-4).

### 10.4 Trần artifact và retention (server-enforced)

| **Kind** | **Cap** | **Xử lý client trước upload** | **Retention (Free / Team / Business)** |
| ---------------------------------------------------------------------- | --------- | ----------------------------------------------------------------------------------- | ----------------- |
| `screenshot`                                                           | 5 MB      | Nén PNG→WebP nếu > 1 MB                                                             | 14 / 30 / 90 ngày |
| `video`                                                                | **20 MB** | Reporter cấu hình Playwright ghi 800×450; vượt cap thì cắt giữ 60 s cuối trước fail | 7 / 14 / 30       |
| `trace`                                                                | 50 MB     | Vượt cap: strip snapshot ảnh, giữ action + network                                  | 7 / 14 / 30       |
| `har`                                                                  | 20 MB     | Strip response body > 100 KB **và** strip `Authorization`/`Cookie` (§11.1)          | 7 / 14 / 30       |
| `log` / `attachment`                                                   | 10 MB     | gzip                                                                                | 14 / 30 / 90      |

- Vượt cap sau xử lý → artifact bị từ chối `413`, **run vẫn hợp lệ**, dashboard hiện "video exceeded limit" kèm hướng dẫn cấu hình. **Không bao giờ** làm fail run của khách vì artifact.
- Quota tổng theo gói (§10.5): chạm 80% → email + banner; chạm 100% → artifact mới bị từ chối, **metadata + kết quả test vẫn nhận**. Suy giảm có kiểm soát, không chết.
- Kịch bản 400 MB/video của v3.0 không thể xảy ra nữa vì cap chặn ở tầng sản phẩm.

### 10.5 Packaging `[ASSUMPTION — phải validate trước billing]`

**Trục thử nghiệm:** active test count = số `stableTestId` có kết quả trong 30 ngày. Trục này tránh per-seat và dễ giải thích, nhưng **chưa được coi là billing truth** vì `stableTestId` còn bị merge/split trong giai đoạn học thuật toán (N19).

| | Free | Team $49/th | Business $199/th | Enterprise (P6) |
|---|---:|---:|---:|---|
| Active tests (shadow meter ở P1) | 100 | 1.000 | 5.000 | tùy |
| Projects | 1 | 10 | không giới hạn | tùy |
| Thành viên | 3 | không giới hạn | không giới hạn | tùy |
| Storage quota | 2 GB | 50 GB | 250 GB | tùy |
| AI hypothesis/ngày | 20 | 200 | 1.000 | tùy |
| Flaky history | 30 ngày | 1 năm | không giới hạn | tùy |
| RBAC | cơ bản | cơ bản | nâng cao | SSO/audit/on-prem |

**Discount rules:**

- Founding partner: tối đa −50% giá Team nếu giữ lời hứa §5.5.
- Regional SEA: −40% self-serve.
- **Không stack discount.** Một account chỉ nhận một rule giá.
- Giá thực thu phải giữ **contribution margin dự kiến ≥ 55%** trên biến phí rolling 3 tháng; thấp hơn thì giảm quota/AI/storage hoặc đổi price, không “hy vọng scale sẽ cứu”.

**Price anchor bên ngoài:** thông báo TestRail cho monthly subscription từ 01-08-2026 nêu Professional **$41/user/tháng** cho nhóm đến 20 users; 10 user ≈ **$410/tháng**. Không hardcode anchor này vào deck lâu dài — re-verify trước mỗi lần regenerate tài liệu đối ngoại.

**Billing meter rule:** trước Gate-1.5 chỉ chạy shadow meter. Merge/split không được tạo hóa đơn hồi tố.

### 10.6 Kiểm chứng pricing và meter

- B5/B6 + D3 phải cho ra: tool spend hiện tại, ai duyệt, ngưỡng tự quẹt được, và khoảng giá “không cần xin sếp”.
- Không hỏi “anh/chị có mua không?” như validation. Ghi mức giá + quy trình duyệt + công cụ họ đang trả tiền.
- **Shadow billing 4 tuần trước billing thật:** mỗi ngày ghi `activeStableIds`, raw distinct client fingerprints, số merge/split/review. Nếu identity correction làm meter lệch > 2% hoặc gây jump không giải thích được → **không charge theo active-test count**, đổi meter trước launch.
- Gate-1.5 pass xong mới mở billing thật; trước đó partner dùng free.

## 11. Security, AuthN và RBAC

### 11.1 Mô hình đe dọa rút gọn

Dữ liệu test E2E nhạy cảm hơn vẻ ngoài. Nó chứa: URL môi trường staging nội bộ, error message có thể lộ SQL/stack, HAR chứa **token/cookie thật** của môi trường test, và video quay màn hình có dữ liệu.

**Tài sản phải bảo vệ:** (1) artifact, (2) ingest token nằm rải trên CI của khách, (3) ranh giới tenant. **Kẻ tấn công điển hình:** người nhặt được token từ CI log public; thành viên tenant khác; scraper.

**Hệ quả thiết kế bắt buộc ở P1:**

- HAR được strip `Authorization`/`Cookie` **phía reporter, trước khi upload**. Mặc định bật, tắt được nhưng phải có chủ đích.
- Artifact URL luôn là presigned ngắn hạn (15 phút). **Không có URL công khai bền.**

### 11.2 Xác thực dashboard

- Email + password (argon2id) và OAuth Google/GitHub. Email verify bắt buộc trước khi tạo/join org. **SSO (SAML/OIDC) là P6** — không kéo lên.
- Session: cookie `httpOnly + Secure + SameSite=Lax`, server-side revoke được. Đổi password → revoke mọi session khác.
- Mọi request state-changing từ dashboard phải kiểm tra **Origin/Referer allowlist** và dùng CSRF token (double-submit hoặc server-issued). CORS/SameSite không được coi là lớp CSRF duy nhất.
- Login / forgot-password / OTP: rate limit theo IP **và** theo account (5 lần/15 phút → backoff). Thông báo lỗi **không phân biệt** "sai email" với "sai password" (chống user enumeration); forgot-password luôn trả *"nếu email tồn tại, đã gửi"*.
- **MFA TOTP cho org owner từ P1** — rẻ, và là câu trả lời cho câu hỏi security review đầu tiên của mọi khách doanh nghiệp.

> Ở **P1-min** (§4.4): chỉ email + password, bỏ OAuth và MFA. Rate limit và chống enumeration **không được bỏ**.

### 11.3 RBAC

#### 11.3.1 Vai trò (cố ý phẳng; custom role là P6)

| **Role** | **Gán ở tầng** | **Dành cho** |
| -------------------------- | ------------ | ----------------------------------------------------------- |
| `org_owner`                | Organization | Founder/quản trị cao nhất — billing, xóa org, quản lý owner |
| `org_admin`                | Organization | Quản lý workspace/member/token toàn org                     |
| `workspace_admin`          | Workspace    | Quản lý project + member trong workspace                    |
| `maintainer`               | Project      | Vận hành project: token ingest, retention, merge identity   |
| `member`                   | Project      | Làm việc hằng ngày: triage, assign, xem mọi thứ             |
| `viewer`                   | Project      | Chỉ đọc (miễn phí không giới hạn — §10.5)                   |

Kế thừa xuống theo cây Org → Workspace → Project. Một người có thể giữ role khác nhau ở project khác nhau.

#### 11.3.2 Ma trận quyền (hành động P1)

| **Hành động** | **owner** | **admin** | **ws_admin** | **maintainer** | **member** | **viewer** |
| ------------------------------------------------------ | - | - | - | - | - | - |
| Xem run/test/artifact/dashboard                        | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Triage: accept/reject, mark flaky, assign              | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ |
| Merge/split test identity                              | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ |
| Tạo/thu hồi ingest token                               | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ |
| Sửa retention/cấu hình project                         | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ |
| Tạo/xóa project                                        | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ |
| Quản lý member (tầng tương ứng)                        | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ |
| Billing, xóa org                                       | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Xóa dữ liệu run/artifact thủ công                      | ✅ | ✅ | ❌ | ✅ | ❌ | ❌ |

**Quy tắc UI:** hành động không có quyền → **ẩn nút**. Gọi thẳng API/URL → `403` (resource nhìn thấy được) hoặc `404` (resource ngoài tenant — không lộ tồn tại). **Cả hai nhánh có integration test.**

#### 11.3.3 Thi hành

- **Một hàm ****`authorize(principal, resource, action)`**** duy nhất**, gọi ở service layer, walk-up cây tổ chức. Không endpoint nào tự viết if-check riêng — đây là cách duy nhất để "không endpoint nào quên được".
- Mọi query dữ liệu mang **điều kiện tenant trong chính câu query** (`WHERE project_id IN (accessible)`), không lọc sau ở application. Chống BOLA tận gốc.
- **Test bắt buộc trong CI:** user A gọi từng endpoint với resource của org B → 100% phải `403`/`404`. Bộ test này là **gate release**, không phải nice-to-have.

### 11.4 Vòng đời ingest token

- Cấp theo §8.8: mặc định `project:ingest`, write-only. Secret phải random ≥ 256 bit. Hiển thị bản rõ **một lần duy nhất**; lưu prefix 8 ký tự để lookup + `HMAC-SHA-256(secret, serverPepper)` để verify. **Argon2id chỉ dùng cho password người dùng**, không dùng trên hot path của API token.
- `lastUsedAt` cập nhật (throttle 5 phút) — bảng token cho maintainer thấy token nào chết để dọn.
- Revoke tức thời. Expiry mặc định 1 năm, nhắc trước 30 ngày.
- **Đăng ký prefix ****`atp_ing_`**** với GitHub Secret Scanning** ngay khi có user ngoài. Token loại này nằm trên CI, **sẽ** bị lộ — câu hỏi chỉ là phát hiện nhanh cỡ nào.
- Blast radius khi lộ: ghi report rác vào 1 project. Phòng thêm: rate limit theo token (§7.4) + alert khi ingest volume một token tăng > 10× baseline tuần.

### 11.5 Chuẩn hạ tầng P1

- TLS mọi nơi + HSTS. CORS chỉ định origin cụ thể của dashboard — **không** `*` cho API có credential.
- Error production không leak stack trace/SQL/path. Debug/Swagger không public.
- Secrets qua env/secret manager, có `.env.example`. Log có correlation id, **không** chứa token/password/nội dung artifact.
- Container non-root, image pin version, healthcheck.
- Backup Postgres hằng ngày **và restore thử trước Gate-1** — backup chưa restore thử = chưa có backup. RPO 24 h, RTO 4 h cho giai đoạn design partner.
- Render dữ liệu từ report (title, error, stack) **luôn escape**. Stack trace là untrusted input.
- Artifact `attachment`/`log` không được inline render HTML/SVG tùy ý; mặc định `Content-Disposition: attachment` + `X-Content-Type-Options: nosniff`. Chỉ viewer đã allowlist (trace/image/video/text) mới render inline.

### 11.6 Checklist OWASP API Top 10 (chạy trước mọi release)

| **#** | **Rủi ro** | **Phòng thủ** | **Bằng chứng phải có** |
| -------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------- | - |
| API1 BOLA                              | Tenant condition trong query + `authorize()` tập trung (§11.3.3)                                                                                            | Bộ test cross-tenant pass 100%                     |   |
| API2 AuthN                             | §11.2: argon2id, session revoke, rate limit login, MFA owner                                                                                                | Test brute-force bị chặn                           |   |
| API3 Property-level                    | DTO/serializer theo role; update qua struct chỉ chứa field cho phép                                                                                         | Test gửi `{"role":"org_owner"}` bị bỏ qua/`400`    |   |
| API4 Resource                          | Trần §7.4 + cap artifact §10.4 + quota + LLM cap §10.3                                                                                                      | Test vượt từng trần trả đúng `413`/`429`           |   |
| API5 Function-level                    | Ma trận §11.3.2; admin action check **role**, không chỉ check login                                                                                         | Test viewer gọi endpoint maintainer → `403`        |   |
| API6 Business flow                     | Đăng ký + mời member + forgot-password có rate limit; Free quota chặn farm storage                                                                          | Ngưỡng ghi trong config, có test                   |   |
| API7 SSRF                              | Server không fetch URL client cung cấp (§7.10); `ciRunUrl` chỉ render                                                                                       | Grep codebase: không outbound fetch từ input       |   |
| API8 Misconfig                         | §11.5; review config đủ lớp app → proxy → container → cloud → DB                                                                                            | Checklist ký trước release                         |   |
| API9 Inventory                         | Chỉ `/v1/`; OpenAPI là nguồn sự thật; token có `lastUsedAt`; endpoint bỏ = chặn thật + ghi ngày tắt                                                         | Diff OpenAPI mỗi release                           |   |
| API10 Unsafe consumption               | P1 phụ thuộc ngoài duy nhất: LLM provider + OAuth. Response LLM **validate schema** trước khi lưu `AiHypothesis` (category ∈ enum §8.7, confidence ∈ [0,1]) | Test LLM trả rác → hypothesis bị loại, không crash |   |

### 11.7 Rủi ro còn lại (cố ý chưa làm ở P1)

- **Chưa có WAF/bot management** — chấp nhận ở quy mô design partner; bật Cloudflare proxy khi mở đăng ký public.
- **Chưa có audit log đầy đủ** (P6) — P1 chỉ log hành động phá hủy (xóa, revoke, merge) vào bảng đơn giản.
- **Chưa pentest bên thứ ba** — điều kiện bắt buộc: trước khách trả phí đầu tiên có dữ liệu production trong artifact, tối thiểu một vòng pentest thuê ngoài hoặc bug bounty private.
- **HAR stripping là best-effort phía client** — khách tắt đi thì token thật của họ nằm trong artifact. Docs phải cảnh báo đậm, mặc định luôn bật.

---

## 12. Ma trận truy vết

### 12.1 Phát hiện từ bản thẩm định v3.0 (F01–F16)

| **#** | **Phát hiện** | **Xử lý tại** | **Trạng thái** |
| --------------------------------- | -------------------------------------------- | ------------------------------------------------------------ | ------------------------------------------------------------------ |
| F01                               | TTRC không đo được từ schema                 | §8.3 `TriageEvent`, §5.4 baseline                            | ✅ Đóng                                                             |
| F02                               | Chưa chốt reporter/agent-first               | QĐ-1, lan truyền §4, §7                                      | ✅ Đóng                                                             |
| F03                               | Chưa có design partner                       | §5 playbook, Gate-0 kill criteria                            | 🔶 **Mở theo thiết kế** — đóng bằng hành động, không bằng tài liệu |
| F04                               | Bản đồ cạnh tranh thiếu                      | §2.4                                                         | ✅ Đóng *(1 khẳng định còn **`[CHƯA KIỂM CHỨNG]`**)*                |
| F05                               | Job state ≠ Run status                       | §7.8 bảng ánh xạ                                             | ✅ Đóng                                                             |
| F06                               | Identity đứt ở giao thức                     | §7.6 handshake                                               | ✅ Đóng                                                             |
| F07                               | Artifact mất `attemptId`                     | §7.7, §8.4                                                   | ✅ Đóng                                                             |
| F08                               | Identity chưa có thuật toán                  | §9                                                           | ✅ Đóng                                                             |
| F09                               | Không có mô hình chi phí                     | §10.2–10.4                                                   | ✅ Đóng                                                             |
| F10                               | Thiếu pricing / RBAC / authN                 | §10.5, §11.2–11.3                                            | ✅ Đóng                                                             |
| F11                               | Mục lỗi thời trong tài liệu 07 cũ            | File đã chuyển vào `docs/v3.0/`, không còn ở đường đọc chính | ✅ Đóng                                                             |
| F12                               | Roadmap bị cắt ở bản đối ngoại               | Quy tắc §4.7 đã có — **nhưng file chưa regenerate**          | 🔴 **ĐANG MỞ**                                                     |
| F13                               | `FlakySignal`/`TestIdentity` không có trường | §8.5, §8.6                                                   | ✅ Đóng                                                             |
| F14                               | Thiếu enum và taxonomy                       | §8.4, §8.7                                                   | ✅ Đóng                                                             |
| F15                               | Token scope bỏ tầng Workspace                | §8.8, §11.3                                                  | ✅ Đóng                                                             |
| F16                               | `.docx` còn `**` literal                     | Quy tắc §4.7 đã có — **nhưng file chưa regenerate**          | 🔴 **ĐANG MỞ**                                                     |

**13 đóng · 1 mở theo thiết kế · 2 đang mở.** README của bộ v3.1 đánh dấu F12 và F16 là đóng — đã sửa lại thành đúng trạng thái ở đây.

### 12.2 Phát hiện khi review hợp nhất (N01–N19)

| # | Phát hiện | Trạng thái |
|---|---|---|
| N01 | Timeline chạy trước gate | ✅ sửa §4.3 |
| N02 | P1 effort lệch scope | ✅ sửa §4.2/§4.4 |
| N03 | Ingest collision | ✅ `occurrenceIndex` |
| N04 | Alias unique không implement được | ✅ `projectId` denormalized |
| N05 | Flaky score chia 0 | ✅ guard |
| N06 | Flaky run có thể xanh sạch | ✅ **QĐ-7 chốt B** |
| N07 | Safety Guard mất khỏi roadmap | ✅ **QĐ-8 dời P3** |
| N08 | JTBD/observability biến mất | ✅ khôi phục |
| N09 | Identity metric lệch nhau | ✅ precision/recall |
| N10 | Claim Currents chưa kiểm chứng | ✅ bỏ khỏi sales claim |
| N11 | Full P1 không fit 12 tuần/2 FTE | ✅ tách track |
| N12 | Baseline 2 tuần không fit nếu partner #3 ký tuần 5 | ✅ deadline ký tuần 4 |
| N13 | Gate-0 bỏ H2/H3 | ✅ Gate-0 đủ 6 điều kiện |
| N14 | Time-bounded split không có schema | ✅ alias validity interval |
| N15 | Clock skew + AI metric survivor/decision bias | ✅ server timestamp + coverage/decision metrics |
| N16 | Competitor map stale | ✅ Azure Workspaces + overlap cập nhật |
| N17 | Price anchor/discount rule lệch | ✅ cập nhật |
| N18 | Token/CSRF/upload hardening | ✅ cập nhật |
| N19 | Billing meter phụ thuộc identity chưa ổn định | ✅ shadow meter trước charge |

## 13. Việc phải làm trước và trong Phase 0

| # | Việc | Ai | Deadline |
|---|---|---|---|
| 1 | **Điền tên người giữ quyền kill gate** | Owner dự án | Trước ngày bắt đầu P0 |
| 2 | **Chốt ngày bắt đầu Phase 0** | Owner dự án | Trước tuần 1 |
| 3 | Regenerate 3 bản đối ngoại theo §4.7 | Doc owner | Trước khi gửi deck/spec ra ngoài |
| 4 | Bỏ mọi claim competitor chưa có bằng chứng; benchmark Currents/Azure/ReportPortal | Product/tech | Trong P0; trước public comparison |
| 5 | Có đủ 3 design partner **chậm nhất cuối tuần 4** để đủ baseline | Product | Tuần 4 |
| 6 | Chọn **P1-min hoặc P1-full** dựa trên Gate-0 + số FTE/budget | Kill-gate owner | Tại Gate-0, trước build |
| 7 | Chạy shadow pricing meter; chưa charge theo `stableTestId` | Product/engineering | P1 → trước Gate-1.5 |

**Blocker thực sự để bắt đầu Phase 0:** mục 1 và 2.  
**Blocker để gửi tài liệu đối ngoại:** mục 3 và mọi claim chưa kiểm chứng ở mục 4.  
**P1-min/P1-full không chặn phỏng vấn; nó chặn việc bắt đầu build sau Gate-0.**

---

*Hết. Lịch sử tài liệu: `docs/v3.0/` (bản gốc), `docs/v3.1/` (bản viết lại rời). v3.2-reviewed là bản review nội dung ngày 24-08-2026.*
