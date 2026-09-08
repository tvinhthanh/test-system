# 08 — Mô hình chi phí, Trần artifact, Pricing & Packaging

> Sửa F09 (không có cost model, không có trần artifact) và một nửa F10 (thiếu tài liệu pricing). Mọi con số tiền là `[ASSUMPTION]` theo đơn giá phổ biến 2026 — cập nhật theo nhà cung cấp thực tế trước khi chốt giá; **cấu trúc** của mô hình là phần chốt cứng.

## 1. Nguyên tắc kinh tế

1. Không có tài khoản nào được phép lỗ biến-phí (storage + egress + LLM) ở trạng thái dùng bình thường của gói.
2. Chi phí biến thiên phải bị chặn trần bằng **cơ chế sản phẩm** (cap, quota, retention), không bằng hy vọng.
3. Chọn storage có egress rẻ/miễn phí (Cloudflare R2 hoặc tương đương) — dashboard test là sản phẩm người dùng *xem lại* artifact nhiều lần; egress S3 chuẩn sẽ ăn hết margin.

## 2. Mô hình khối lượng — "team chuẩn" làm đơn vị tính

Định nghĩa team chuẩn (khớp kịch bản thẩm định §4.4): 300 test E2E, chạy 1 lần/đêm + 10 run nhỏ/ngày, fail rate 5%, retry 1 lần cho fail, bật video-on-fail và trace-on-retry.

| Thành phần | Tính | / đêm | / tháng (30 ngày) |
|---|---|---|---|
| Fail + retry | 300 × 5% = 15 fail → 30 attempt có artifact nặng | | |
| Video (cap 20 MB — §4) | 30 × ~12 MB trung bình | 360 MB | ~11 GB |
| Trace (cap 50 MB) | 15 × ~15 MB | 225 MB | ~7 GB |
| Screenshot + log | 300 × ~60 KB | 18 MB | ~0,5 GB |
| Run nhỏ ban ngày | ~30% khối đêm | ~180 MB | ~5,5 GB |
| **Tổng ghi mới** | | **~0,8 GB/ngày** | **~24 GB/tháng** |

Với retention §4 (video/HAR 14 ngày, trace 14 ngày, screenshot/log 30 ngày, metadata vĩnh viễn): **storage thường trực ≈ 12–15 GB/team chuẩn**.

## 3. Chi phí biến thiên / team chuẩn / tháng `[ASSUMPTION]`

| Khoản | Đơn giá giả định | Thành tiền |
|---|---|---|
| Storage 15 GB (R2 $0.015/GB) | | $0.23 |
| Class-A/B operations (~500k ops) | | ~$0.50 |
| Egress xem lại artifact | R2 = $0 (chính là lý do chọn) | $0 |
| LLM triage: 15 fail mới/ngày × ~6k token in + 500 out, model hạng trung | ~$0.01/hypothesis | ~$4.50 |
| DB + compute phân bổ | | ~$3–5 |
| **Tổng biến phí** | | **≈ $9–10 / team chuẩn / tháng** |

Kết luận: giá gói Team phải ≥ 5× biến phí → sàn giá an toàn ~$49/tháng. Kịch bản 400 MB/video của v3.0 (thẩm định đã bắt lỗi) không thể xảy ra nữa vì cap §4 chặn ở tầng sản phẩm.

**Kill-switch chi phí:** alert khi biến phí một org > 40% doanh thu org đó trong tháng; LLM có trần cứng: tối đa 200 hypothesis/org/ngày (Free: 20), fail trùng fingerprint trong ngày không chạy lại (đã ghi ở 01 QĐ-4).

## 4. Trần artifact + retention (server-enforced, reporter đọc từ `limits` — 05 §6)

| Kind | Cap kích thước | Xử lý client trước upload | Retention mặc định (Free / Team / Business) |
|---|---|---|---|
| screenshot | 5 MB | nén PNG→WebP nếu > 1 MB | 14 / 30 / 90 ngày |
| video | **20 MB** | reporter cấu hình Playwright ghi 800×450; > cap thì cắt giữ 60 s cuối trước fail | 7 / 14 / 30 |
| trace | 50 MB | > cap: strip snapshot ảnh, giữ action+network | 7 / 14 / 30 |
| har | 20 MB | strip response body > 100 KB | 7 / 14 / 30 |
| log/attachment | 10 MB | gzip | 14 / 30 / 90 |

- Vượt cap sau xử lý → artifact bị từ chối 413, run vẫn hợp lệ, dashboard hiện "video exceeded limit" kèm hướng dẫn cấu hình — **không bao giờ** làm fail run của khách vì artifact.
- Quota tổng theo gói (§5); chạm 80% → email + banner; chạm 100% → artifact mới bị từ chối (metadata + kết quả test vẫn nhận — sản phẩm suy giảm có kiểm soát, không chết).
- Retention ghi vào `Artifact.expiresAt` tại thời điểm ghi nhận (06 §4) — đổi policy chỉ ảnh hưởng artifact mới, khỏi tranh cãi hồi tố.

## 5. Packaging `[ASSUMPTION — Phase 0 validate câu B5–B7 của 04]`

Trục tính tiền chính: **active test count** (số stableTestId có kết quả trong 30 ngày) — tương quan thẳng với chi phí và giá trị, không phạt việc mời cả team vào xem (viewer miễn phí thúc network effect trong công ty; per-seat làm ICP giấu tài khoản).

| | Free | Team $49/th | Business $199/th | Enterprise (P6) |
|---|---|---|---|---|
| Active tests | 100 | 1.000 | 5.000 | tùy |
| Projects | 1 | 10 | không giới hạn | |
| Thành viên | 3 | không giới hạn | không giới hạn | |
| Retention artifact | cột Free §4 | cột Team | cột Business | tùy |
| Storage quota | 2 GB | 50 GB (+$10/50 GB) | 250 GB | |
| AI hypothesis/ngày | 20 | 200 | 1.000 | |
| Flaky detection + lịch sử | 30 ngày | 1 năm | không giới hạn | |
| RBAC | cơ bản | cơ bản | + custom role | + SSO, audit, on-prem |

- Founding partner (design partner chuyển trả phí): −50% trọn đời gói Team — ghi sẵn để giữ lời hứa ở 04 §5.
- Regional pricing SEA (QĐ-3): −40% theo billing country, chỉ áp cho self-serve; thẻ giá neo so sánh: "TestRail ~$37/user/tháng × 10 user = $370 — gói Team phủ cả team giá $49".

## 6. Kiểm chứng ở Phase 0 (nối vào 04)

- Câu B5–B7 phải cho ra: ngân sách tool QA hiện tại, ai duyệt, ngưỡng "không cần xin sếp" (giả thuyết: < $50/tháng tự quẹt được ở SEA startup — nếu đúng, $49 là điểm đặt có chủ đích).
- Bài test sẵn-sàng-chi-trả: cuối phỏng vấn, đưa bảng §5 và hỏi "team anh/chị rơi vào gói nào, và giá này so với giá trị thì sao?" — ghi nguyên văn, không dẫn dắt.
- Gate-1.5 pass xong mới mở billing thật; trước đó mọi partner dùng free — đừng bán thứ chưa chứng minh được TTRC.
