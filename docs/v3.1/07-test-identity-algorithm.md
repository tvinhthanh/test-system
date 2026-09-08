# 07 — Thuật toán Test Identity v1

> Sửa F08: v3.0 mới có 6 tín hiệu, chưa có thuật toán. File này là spec code được. Đây là khối rủi ro cao nhất của sản phẩm (không có identity ổn định → không có lịch sử, trend, flaky, AI) — vì vậy nó được **prototype ở tuần 3–4 của P0** trên dữ liệu thật, trước cả dashboard.

## 1. Bài toán và phạm vi v1

**Input:** dòng event từ reporter, mỗi event mang `identity` raw: `{framework, filePath, titlePath[], paramSignature}` + `clientTestId` (fingerprint client tự tính).
**Output:** mỗi event được gán về đúng một `stableTestId`, bền vững qua: đổi tên test, đổi tên/di chuyển file, thêm bớt describe block, thay đổi tham số hóa.
**Ngoài phạm vi v1 (ghi rõ để khỏi phình):** match xuyên framework (Playwright ↔ Cypress), match bằng nội dung code test (AST), match xuyên project.

## 2. Fingerprint (chạy ở client, deterministic)

```
normalizedFilePath = filePath
  → thay '\' bằng '/'
  → bỏ prefix root của repo (reporter tự phát hiện từ cwd/config)
  → lowercase phần đuôi mở rộng
normalizedTitle    = mỗi phần tử titlePath: trim, gộp whitespace liên tiếp thành 1 space
paramSignature     = key=value sort theo key, join bằng '&' (rỗng nếu không tham số hóa)

fingerprint = "fp1:" + sha256(
  framework + "\n" + normalizedFilePath + "\n" +
  join(normalizedTitle, "\u0000") + "\n" + paramSignature
)
clientTestId = "ct_" + first16bytes(fingerprint) (hex)
```

- Prefix `fp1:` là **version của thuật toán fingerprint** — đổi cách chuẩn hóa = tăng version, server match được cả hai đời nhờ raw signals (§5).
- Không đưa số thứ tự test, line number, thời gian vào fingerprint — chúng đổi liên tục, gây vỡ identity giả.

## 3. Pipeline resolve phía server

Sơ đồ trả lời: một event đi qua những bước quyết định nào để nhận `stableTestId`.

```mermaid
%%{init: {
  'theme': 'base',
  'themeVariables': {
    'primaryColor': '#ffffff', 'primaryBorderColor': '#000000',
    'primaryTextColor': '#000000', 'lineColor': '#000000',
    'tertiaryColor': '#ffffff', 'fontSize': '14px'
  },
  'flowchart': { 'curve': 'stepAfter', 'nodeSpacing': 60, 'rankSpacing': 70, 'padding': 12 }
}}%%
flowchart TD
    A[Event vào] --> B{Alias có fingerprint?}
    B -- Có --> C[Gán stableTestId của alias]
    B -- Không --> D[Lấy ứng viên cùng project]
    D --> E[Chấm điểm từng ứng viên]
    E --> F{maxScore >= 0.85?}
    F -- Có --> G[Auto-match: thêm alias, gán identity]
    F -- Không --> H{maxScore >= 0.60?}
    H -- Có --> I[Tạo identity mới + đưa cặp vào Review Queue]
    H -- Không --> J[Tạo identity mới]
    C --> K[Xong]
    G --> K
    I --> K
    J --> K
```

### 3.1 Bước 1 — Exact match (đường nóng, > 99% event)
Tra `TestIdentityAlias.fingerprint`. Trúng → xong, O(1) theo index. Đây là lý do resolve chạy được inline trong pipeline ingest (p50 < 1 s — 05 §5).

### 3.2 Bước 2 — Candidate set
Fingerprint lạ → lấy ứng viên là các `TestIdentity.status=active` cùng `projectId` thỏa **ít nhất một**: cùng `canonicalFilePath`; hoặc cùng phần tử cuối của `titlePath` (leaf title); hoặc trigram similarity của leaf title ≥ 0.4 (pg_trgm). Giới hạn 50 ứng viên điểm thô cao nhất.

### 3.3 Bước 3 — Chấm điểm

`score = 0.45·S_title + 0.25·S_file + 0.20·S_param + 0.10·S_recency`

| Thành phần | Cách tính |
|---|---|
| `S_title` | So `titlePath`: leaf giống hệt = 1.0; khác thì normalized Levenshtein similarity trên leaf (0.7 trọng số) + Jaccard trên các describe cha (0.3) |
| `S_file` | Giống hệt = 1.0; cùng thư mục khác tên file = 0.6; cùng tên file khác thư mục (file bị move) = 0.7; còn lại = trigram similarity |
| `S_param` | Giống hệt = 1.0; cùng tập key khác value = 0.5; khác tập key = 0 |
| `S_recency` | Identity có `lastSeenAt` trong 7 ngày = 1.0, suy giảm tuyến tính về 0 ở 90 ngày *(rename thường thấy ngay run kế tiếp — identity "biến mất" đúng lúc fingerprint lạ xuất hiện là tín hiệu mạnh)* |

Tín hiệu tăng cường: nếu trong **cùng run**, identity ứng viên không xuất hiện (đúng kịch bản rename) → +0.05; nếu ứng viên VẪN xuất hiện trong run này với fingerprint cũ (nghĩa là đây là test mới thật, không phải rename) → score × 0.3 (phạt nặng, chống gộp nhầm hai test khác nhau).

### 3.4 Bước 4 — Ngưỡng

| Điều kiện | Hành động |
|---|---|
| `score ≥ 0.85` và cách ứng viên #2 ≥ 0.10 | Auto-match: thêm alias `source=auto_match`, ghi `matchScore` |
| `0.60 ≤ score < 0.85`, hoặc top-2 sát nhau (< 0.10) | Tạo identity mới NGAY (không chặn ingest) + đẩy cặp vào **Review Queue**; người dùng merge → chuyển alias, identity mới `status=merged`, lịch sử gộp về |
| `score < 0.60` | Identity mới, không hỏi |

Ngưỡng 0.85/0.60 và các trọng số là `[ASSUMPTION]` khởi điểm — hiệu chỉnh ở bước prototype (§6) bằng dữ liệu thật, ghi giá trị chốt vào changelog.

### 3.5 Parameterized tests
Mỗi tổ hợp param = một `TestIdentity` riêng (vì flaky thường chỉ ở một browser/tham số), NHƯNG các identity cùng `(filePath, titlePath)` khác `paramSignature` được nhóm bằng trường suy diễn `templateKey = sha256(filePath + titlePath)` — dashboard hiển thị gộp theo template, drill-down theo param. Không cần bảng mới, chỉ cần index theo `templateKey`.

## 4. Công cụ thủ công (bắt buộc có ở P1, dù xấu)

- **Merge:** chọn 2+ identity → gộp: mọi alias trỏ về identity đích, nguồn `status=merged, mergedIntoId=đích`, TestResult backfill bằng job nền, TriageEvent giữ nguyên (đã trỏ testResultId). Ghi audit `source=manual_merge, createdBy`.
- **Split:** chọn một alias tách khỏi identity → tạo identity mới, TestResult có fingerprint đó từ thời điểm T chuyển sang (mặc định T = toàn bộ; cho chọn mốc). Đây là undo của auto-match sai — bắt buộc có vì thuật toán chắc chắn sai một tỉ lệ.
- **Review Queue UI:** danh sách cặp nghi rename, hiển thị diff title/file/param + score, hai nút Merge/Keep separate. Mỗi quyết định lưu làm **ground truth** để hiệu chỉnh trọng số.

## 5. Tiến hóa thuật toán không vỡ lịch sử

- Client nâng fingerprint `fp1 → fp2`: server thấy prefix lạ → chạy pipeline như fingerprint mới; vì raw signals đi kèm mọi event (05 §2.3), exact-match theo `(filePath, titlePath, param)` chuẩn hóa lại server-side sẽ bắt được → thêm alias `fp2` vào identity cũ. Kịch bản này phải có integration test riêng.
- Server đổi trọng số: chỉ ảnh hưởng match tương lai; không re-run quá khứ trừ khi chạy chiến dịch backfill có chủ đích.

## 6. Prototype tuần 3–4 (P0) — tiêu chí đạt

1. Chạy trên ≥ 500 test result thật từ ≥ 2 design partner (xin export JSON report Playwright).
2. Tự tạo bộ thử nghiệm rename: lấy 50 test thật, mô phỏng 5 kiểu đổi (đổi leaf title, đổi describe, move file, đổi cả hai, thêm param) → đo precision/recall của auto-match.
3. Ngưỡng đạt: **precision auto-match ≥ 0.98** (gộp nhầm tệ hơn tách nhầm — gộp nhầm làm bẩn lịch sử hai test), recall ≥ 0.80 (miss thì rơi vào Review Queue, chấp nhận được).
4. Không đạt → đơn giản hóa: v1 chỉ auto-match khi `score ≥ 0.95` và dồn phần còn lại vào Review Queue; sản phẩm vẫn ship được với nhiều review thủ công hơn. Ghi kết quả vào biên bản Gate-0.

## 7. Edge case bắt buộc có test

| Case | Hành vi đúng |
|---|---|
| Hai test trùng cả title lẫn file (framework cho phép) | Fingerprint trùng → về cùng identity; chấp nhận, ghi rõ giới hạn trong docs người dùng |
| Title chứa dữ liệu động (`"order #8412 succeeds"`) | v1: hai identity — Review Queue sẽ đầy; ghi backlog v2: phát hiện pattern số/uuid trong title và chuẩn hóa thành placeholder |
| Xóa test rồi thêm lại sau 2 tháng | `S_recency` thấp → thường thành identity mới; nếu fingerprint giống hệt → exact match vẫn nối lại đúng (alias còn đó) |
| 10.000 test/run | Bước 2–3 chỉ chạy cho fingerprint lạ; run bình thường có < 10 fingerprint lạ |
| Hai run song song cùng gửi fingerprint lạ giống nhau | Unique constraint §5 của 06 + upsert `ON CONFLICT` → chỉ một identity được tạo, request kia nhận identity đó (test race bắt buộc) |
