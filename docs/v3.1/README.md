# Automation Testing Platform — Bộ tài liệu triển khai v3.1

> Phiên bản: v3.1 · Ngày: 24-08-2026 · Trạng thái: **Chốt để triển khai** (thay thế bộ v3.0)
> Nguồn: bộ 8 tài liệu v3.0 + Bản thẩm định 24-08-2026 (16 phát hiện F01–F16, 6 quyết định QĐ-1–6)

## Nguyên tắc của phiên bản này

1. **Một nguồn sự thật.** Mọi mâu thuẫn giữa các file v3.0 (reporter-first vs agent-first, roadmap bị cắt) đã được hòa giải. Nếu tài liệu nào nói khác bộ này → tài liệu đó sai.
2. **Mọi quyết định đều có bản ghi.** Xem `01-decisions.md`. Không quyết định nào được để "mở" nếu nó chặn Phase 0–1.
3. **Chỉ số chính đo được bằng schema.** Time-to-root-cause (TTRC) giờ là entity `TriageEvent` trong schema — không còn là con số "sẽ đo bằng cách nào đó".
4. **Gate có tiêu chí dừng.** Mỗi gate có cả điều kiện *pass* lẫn điều kiện *kill*. Gate không có tiêu chí dừng chỉ là thủ tục.

## Danh sách tài liệu

| # | File | Nội dung | Thay thế / sửa |
|---|---|---|---|
| 00 | `README.md` | Chỉ mục, ma trận truy vết | — |
| 01 | `01-decisions.md` | Bản ghi quyết định QĐ-1 → QĐ-6 (dạng ADR) | F02 |
| 02 | `02-positioning-competitive.md` | Định vị lại differentiator + bản đồ cạnh tranh đầy đủ | F04, tài liệu 00+02 cũ |
| 03 | `03-roadmap-gates.md` | Roadmap P0–P6 thống nhất, gate + kill criteria, Phase 1.5 AI triage | F02, F03, F12 |
| 04 | `04-phase0-playbook.md` | Playbook Phase 0: phỏng vấn, design partner, đo baseline TTRC | F03 |
| 05 | `05-protocol-spec.md` | Ingestion API + Agent Protocol v1.1: state mapping, attemptId, identity handshake, 5 câu hỏi mở đã chốt | F05, F06, F07, F11 |
| 06 | `06-canonical-schema.md` | Schema v1.1: TriageEvent, TestIdentity, FlakySignal, enum + taxonomy, token scope 5 tầng | F01, F13, F14, F15 |
| 07 | `07-test-identity-algorithm.md` | Thuật toán test identity: fingerprint, matching, merge/split | F08 |
| 08 | `08-cost-model-pricing.md` | Mô hình chi phí lưu trữ, trần artifact, quota theo gói, pricing & packaging | F09, F10, ví dụ 400 MB |
| 09 | `09-security-rbac.md` | AuthN dashboard, mô hình RBAC, bảo mật agent token, ánh xạ OWASP API Top 10 | F10, F15 |

## Ma trận truy vết phát hiện → tài liệu

| Phát hiện | Mức | Xử lý tại | Trạng thái |
|---|---|---|---|
| F01 — TTRC không đo được từ schema | Nghiêm trọng | 06 §3 (`TriageEvent`), 04 §4 (baseline) | ✅ Đóng |
| F02 — Chưa chốt reporter/agent-first | Nghiêm trọng | 01 QĐ-1, lan truyền sang 03, 05 | ✅ Đóng |
| F03 — Chưa có design partner | Nghiêm trọng | 04 (playbook), 03 Gate-0 kill criteria | 🔶 Mở theo thiết kế — đóng bằng hành động, không bằng tài liệu |
| F04 — Bản đồ cạnh tranh thiếu | Cao | 02 §3 | ✅ Đóng |
| F05 — Job state ≠ Run status | Cao | 05 §4 (bảng ánh xạ chuẩn) | ✅ Đóng |
| F06 — Identity đứt ở giao thức | Cao | 05 §5 (clientTestId handshake) | ✅ Đóng |
| F07 — Artifact mất attemptId | Cao | 05 §6, 06 §4 | ✅ Đóng |
| F08 — Identity chưa có thuật toán | Cao | 07 (toàn bộ) | ✅ Đóng |
| F09 — Không có mô hình chi phí | Cao | 08 §2–4 | ✅ Đóng |
| F10 — Thiếu pricing / RBAC / authN | Cao | 08 §5–6, 09 §2–4 | ✅ Đóng |
| F11 — Mục lỗi thời trong 07 cũ | Trung bình | Bộ này thay thế; mục đó bị xóa | ✅ Đóng |
| F12 — Roadmap bị cắt ở bản đối ngoại | Trung bình | 03 §6 (quy tắc đối ngoại) | ✅ Đóng |
| F13 — FlakySignal/TestIdentity không có trường | Trung bình | 06 §5–6 | ✅ Đóng |
| F14 — TestResult.status không enum, thiếu taxonomy | Trung bình | 06 §4, §7 | ✅ Đóng |
| F15 — Token scope bỏ tầng Workspace | Thấp | 06 §8, 09 §4 | ✅ Đóng |
| F16 — .docx còn `**` literal | Thấp | Quy trình xuất bản: 03 §6.3 | ✅ Đóng |

## Thứ tự đọc đề xuất

- **Founder / người quyết định:** 01 → 03 → 08 → 04
- **Engineer bắt đầu code:** 01 → 05 → 06 → 07 → 09
- **Người đi phỏng vấn Phase 0:** 04 → 02

## Glossary (áp dụng toàn bộ)

| Thuật ngữ | Nghĩa duy nhất |
|---|---|
| **Run** | Một lần thực thi bộ test, do reporter báo lên hoặc agent thực thi. Entity phía server. |
| **Job** | Đơn vị việc giao cho agent (chỉ tồn tại ở luồng agent). Một Job sinh ra một Run. |
| **TestResult** | Kết quả của một test trong một Run, gồm nhiều **Attempt** nếu retry. |
| **Attempt** | Một lần chạy vật lý của một test (attempt 0 = lần đầu, 1+ = retry). |
| **clientTestId** | Fingerprint định danh test do reporter/agent tự tính, deterministic. |
| **stableTestId** | Định danh test bền vững do server cấp, tồn tại xuyên rename. |
| **TTRC** | Time-to-root-cause: từ lúc test fail đến lúc giả thuyết nguyên nhân được **chấp nhận**. |
| **TriageEvent** | Sự kiện ghi nhận hành vi triage của người/AI — nguồn dữ liệu duy nhất để tính TTRC. |
| **Design partner** | Team dùng thật, có bộ E2E thật, cam kết phản hồi hàng tuần bằng văn bản. |
