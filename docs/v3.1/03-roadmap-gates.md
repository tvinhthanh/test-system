# 03 — Roadmap & Gates (v3.1)

> Thay thế tài liệu 03 cũ. Đây là **bản đồ định hướng có gate**, không phải kế hoạch thi công: chỉ P0, P1, P1.5 được lập kế hoạch chi tiết; P2+ chỉ được lập kế hoạch sau khi gate trước pass.
> Giả định nguồn lực: 1–2 người, reporter-first (QĐ-1). Nếu quy mô đội thay đổi → cập nhật bảng §5 trước, roadmap sau.

## 1. Roadmap tổng

| Phase | Tên | Nội dung lõi | Điều kiện vào | Ước lượng (người-tháng) |
|---|---|---|---|---|
| **P0** | Validate | 15 phỏng vấn, 3 design partner, baseline TTRC | Bắt đầu ngay | 0 code (trừ prototype identity) |
| **P1** | Report Center v0 | Ingestion API, Playwright Reporter SDK, test identity v1, artifact (cap + retention), dashboard v0, trace viewer nhúng, authN + RBAC tối thiểu | Gate-0 pass | 4–6 |
| **P1.5** | AI Triage v0 | Root-cause hypothesis + accept/reject + TriageEvent + đo TTRC | Gate-1 pass, schema ổn định | 0,5–1 |
| **P2** | Test Management | Manual test, import TestRail/Xray/Excel | Gate-1.5 pass **và** dữ liệu Phase 0/khách ủng hộ | 2–4 |
| **P3** | Scheduler & Agent | Local agent (pull-only), scheduler, CI ingestion, notification | Khách trả tiền yêu cầu scheduling | 3–5 |
| **P4** | AI nâng cao | Smart triage, phân cụm failure, gợi ý fix | acceptance_rate v0 ≥ 60% ổn định | 2–3 |
| **P5** | Mở rộng | Visual regression, performance, security, distributed runner | Chỉ khi có cầu trả tiền | — |
| **P6** | Enterprise | SSO, RBAC nâng cao, audit log, plugin marketplace, on-prem | Hợp đồng enterprise cụ thể | — |

Thay đổi so với v3.0: **Local agent chuyển từ P1 sang P3** (QĐ-1); **AI triage v0 tách khỏi P4 thành P1.5** (QĐ-4); **managed agent bị loại khỏi 12 tháng đầu** (QĐ-5, điều kiện mở lại ghi ở 01).

## 2. Gate — mỗi gate có PASS và KILL

### Gate-0 (cuối tuần 6 của P0)
- **PASS khi có đủ cả 4:**
  1. ≥ 15 phỏng vấn ICP hoàn thành, có ghi chú theo mẫu 04 §3.
  2. 3 design partner ký cam kết (04 §5), có bộ E2E thật chạy ≥ 3 lần/tuần.
  3. Baseline TTRC đo xong cho cả 3 partner (04 §4) — có con số, không phải cảm nhận.
  4. Pain ranking: "không biết fail vì sao / flaky" nằm trong top-2 pain của ≥ 8/15 người phỏng vấn.
- **KILL (nguyên văn QĐ-6):** sau 6 tuần không đủ 3 partner đạt chuẩn → dừng, đổi wedge hoặc đóng. Không chuyển P1.
- **Nhánh lật wedge:** nếu điều 4 trượt nhưng "quản lý manual + automation song song" là top-1 của ≥ 8/15 → kích hoạt phương án B của QĐ-2, viết lại 02 và 03 trong 1 tuần, chạy lại Gate-0 rút gọn (không cần phỏng vấn lại, cần ký lại cam kết partner theo wedge mới).

### Gate-1 (cuối tuần 12 của P1)
- **PASS khi có đủ cả 3:**
  1. Cả 3 design partner đẩy report từ dự án thật ≥ 2 tuần liên tục, ≥ 3 run/tuần/partner.
  2. Test identity v1: qua 2 tuần dữ liệu thật, tỉ lệ identity vỡ (một test thật sinh ≥ 2 stableTestId, hoặc 2 test thật bị gộp 1) < 5% trên tổng test — đo bằng review thủ công với partner.
  3. ≥ 2/3 partner trả lời "có" cho câu: "nếu ngày mai chúng tôi tắt dịch vụ, anh/chị có bị ảnh hưởng công việc không?"
- **KILL:** < 2 partner còn dùng thật ở tuần 12 → quay lại QĐ-2, không mở P1.5.

### Gate-1.5 (sau 6 tuần chạy AI triage v0)
- **PASS:** trung vị TTRC của 3 partner giảm ≥ 30% so với baseline P0 **và** hypothesis_acceptance_rate ≥ 40%.
- **KILL cho tính năng (không kill dự án):** acceptance_rate < 40% hai tuần liên tiếp → AI triage về opt-in, dự án vẫn tiến bằng differentiator #1 (aggregation); P4 hoãn vô thời hạn.

### Gate-2 trở đi
Chỉ viết chi tiết khi gate trước pass. Nguyên tắc chung: mỗi gate phải có (a) chỉ số định lượng, (b) người giữ quyền kill (mặc định: người ký ở 01 QĐ-6), (c) hành động cụ thể khi trượt.

## 3. Timeline 90 ngày (đội 1–2 người)

| Tuần | Việc | Deliverable kiểm chứng được |
|---|---|---|
| 1–4 | **P0 thuần**: phỏng vấn, tuyển partner, đo baseline TTRC | 15 bản ghi chú phỏng vấn; 3 cam kết đã ký; bảng baseline TTRC |
| 3–4 (song song) | **Prototype test identity** (ngoại lệ code duy nhất của P0) trên report thật xin từ partner | Kết quả chạy thuật toán 07 trên ≥ 500 test result thật, báo cáo tỉ lệ match đúng |
| 5–6 | Ingestion API + schema (05, 06) + authN tối thiểu | `POST /v1/runs` + `/events` chạy production-ready, có test cho case lỗi |
| 6–8 | Playwright Reporter SDK + artifact pipeline (cap, chunked upload, retention job) | Partner #1 cài reporter vào repo thật, report đầu tiên lên dashboard |
| 8–11 | Dashboard v0: run list, test history theo stableTestId, flaky view, trace viewer nhúng, màn hình EM | Cả 3 partner dùng hàng tuần |
| 11–12 | AI Triage v0 (nếu schema đã đứng yên ≥ 2 tuần) + đo lại TTRC | Số TTRC tuần 12 so với baseline |
| 12 | **Gate-1** | Biên bản gate: pass/kill, ký tên |

## 4. Nguyên tắc thi công xuyên suốt

1. **Prototype rủi ro cao nhất trước** — test identity đi trước dashboard (thẩm định §4.2).
2. **Schema đóng băng trước khi build AI**: P1.5 chỉ bắt đầu khi 06 không đổi migration nào trong 2 tuần.
3. **Boring stack**: Postgres + object storage (S3/R2) + một backend monolith + queue. Không K8s, không microservices, không Kafka. Nâng cấp khi có số đo, không khi có cảm giác.
4. Mọi tuần đều ship cho partner — không có sprint nào kết thúc bằng "demo nội bộ".

## 5. Bảng khả thi theo quy mô đội (giữ từ thẩm định, làm hệ quy chiếu)

| Quy mô | Phạm vi khả thi | Thời gian |
|---|---|---|
| 1 người / part-time | P0 + P1 reporter-only | 3–5 tháng |
| 1 người + local agent | — | Không khả thi |
| 2–3 người full-time | P0 + P1 + P1.5 + agent cơ bản (P3 sớm) | 6–9 tháng |
| < 5 người, toàn bộ P0–P6 | — | Không khả thi trong 1–2 năm; P5–P6 là tầm nhìn 3–5 năm |

## 6. Quy tắc tài liệu đối ngoại (sửa F12, F16)

1. **Không cắt roadmap.** Bản tóm tắt/deck được rút gọn nhưng phải hiện đủ P0–P6; P5–P6 gắn nhãn "Vision 3–5 năm, gated by demand" — cắt hẳn tạo cảm giác sản phẩm không có đường lên enterprise.
2. **Một nguồn sinh nhiều định dạng:** file .md trong repo là gốc; .docx/.pptx sinh bằng pipeline chuyển đổi (pandoc hoặc script) — cấm copy-paste tay Markdown vào Word (nguyên nhân F16). Checklist trước khi gửi ra ngoài: không còn ký tự `**`, `#`, ` ``` ` literal; roadmap khớp bản này; MVP #1 là Reporter SDK.
3. Mỗi bản đối ngoại ghi version + ngày, trỏ về commit của bộ tài liệu gốc.
