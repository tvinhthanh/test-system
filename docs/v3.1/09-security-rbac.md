# 09 — Security, AuthN & RBAC

> Sửa nửa còn lại của F10 (thiếu mô hình RBAC và xác thực dashboard) + chuẩn hóa bảo mật toàn hệ thống theo OWASP API Security Top 10 (2023). Phạm vi P1: §2–§5. §6 là ánh xạ đầy đủ dùng làm checklist pre-release.

## 1. Mô hình đe dọa rút gọn (vì sao sản phẩm này nhạy cảm hơn vẻ ngoài)

Dữ liệu test E2E chứa: URL môi trường staging nội bộ, error message có thể lộ SQL/stack, HAR chứa **token/cookie thật** của môi trường test, video quay màn hình có dữ liệu. Tài sản phải bảo vệ: (1) artifact, (2) ingest token nằm rải trên CI của khách, (3) ranh giới tenant. Kẻ tấn công điển hình: người nhặt được token từ CI log public; thành viên tenant khác; scraper.

**Hệ quả thiết kế bắt buộc (P1):** HAR được strip `Authorization`/`Cookie` header **phía reporter trước khi upload** (mặc định bật, tắt được có chủ đích); artifact URL luôn là presigned ngắn hạn (15 phút), không có URL công khai bền.

## 2. Xác thực dashboard (P1)

- Email + password (argon2id) và OAuth Google/GitHub. Email verify bắt buộc trước khi tạo/join org. SSO (SAML/OIDC doanh nghiệp) là P6 — đúng như roadmap, không kéo lên.
- Session: cookie `httpOnly + Secure + SameSite=Lax`, server-side session revoke được; đổi password → revoke mọi session khác.
- Login/forgot-password/OTP: rate limit theo IP + theo account (5 lần/15 phút → backoff), thông báo lỗi không phân biệt "sai email" với "sai password" (chống user enumeration; forgot-password luôn trả "nếu email tồn tại, đã gửi").
- MFA TOTP: có từ P1 cho org owner (rẻ, và là câu trả lời cho câu hỏi security review đầu tiên của mọi khách doanh nghiệp).

## 3. RBAC (P1 — cố ý phẳng, custom role là P6)

### 3.1 Vai trò

| Role | Gán ở tầng | Dành cho |
|---|---|---|
| `org_owner` | Organization | Founder/quản trị cao nhất — billing, xóa org, quản lý owner |
| `org_admin` | Organization | Quản lý workspace/member/token toàn org |
| `workspace_admin` | Workspace | Quản lý project + member trong workspace |
| `maintainer` | Project | Vận hành project: token ingest, retention, merge identity |
| `member` | Project | Làm việc hằng ngày: triage, assign, xem mọi thứ |
| `viewer` | Project | Chỉ đọc (miễn phí không giới hạn — 08 §5) |

Kế thừa xuống theo cây Org → Workspace → Project (khớp 06 §8). Một người có thể giữ role khác nhau ở project khác nhau.

### 3.2 Ma trận quyền (các hành động P1)

| Hành động | owner | admin | ws_admin | maintainer | member | viewer |
|---|---|---|---|---|---|---|
| Xem run/test/artifact/dashboard | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Triage: accept/reject hypothesis, mark flaky, assign | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ |
| Merge/split test identity | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ |
| Tạo/thu hồi ingest token | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ |
| Sửa retention/cấu hình project | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ |
| Tạo/xóa project | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ |
| Quản lý member (tầng tương ứng) | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ |
| Billing, xóa org | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Xóa dữ liệu run/artifact thủ công | ✅ | ✅ | ❌ | ✅ | ❌ | ❌ |

Quy tắc UI: hành động không có quyền → **ẩn nút**; gọi thẳng API/URL → 403 (cùng resource nhìn thấy được) hoặc 404 (resource ngoài tenant — không lộ tồn tại). Cả hai nhánh có integration test.

### 3.3 Thi hành

- **Một hàm `authorize(principal, resource, action)` duy nhất**, gọi ở service layer, walk-up cây tổ chức — không endpoint nào tự viết if-check riêng (đây là cách duy nhất để "không endpoint nào quên được").
- Mọi query dữ liệu mang điều kiện tenant trong chính câu query (`WHERE project_id IN (accessible)`), không lọc sau ở application — chống BOLA tận gốc.
- Test bắt buộc trong CI: user A gọi từng endpoint với resource của org B → 100% phải 403/404. Bộ test này là gate release, không phải nice-to-have.

## 4. Vòng đời ingest token (nửa còn lại của 05 §9, sửa F15 phần vận hành)

- Cấp theo 06 §8: mặc định `project:ingest`, write-only. Hiển thị bản rõ **một lần duy nhất** lúc tạo; lưu argon2id hash + prefix 8 ký tự để nhận diện (`atp_ing_xxxxxxxx…`).
- `lastUsedAt` cập nhật (throttle 5 phút/lần) — bảng token cho maintainer thấy token nào chết để dọn (chuẩn OWASP API9: inventory).
- Revoke tức thời; expiry mặc định 1 năm, nhắc trước 30 ngày. Prefix `atp_ing_` đăng ký với GitHub Secret Scanning ngay khi có user ngoài — token loại này nằm trên CI, **sẽ** bị lộ, câu hỏi chỉ là phát hiện nhanh cỡ nào.
- Lộ token: blast radius = ghi report rác vào 1 project. Phòng thêm: rate limit theo token (05 §2.4) + alert khi ingest volume một token tăng bất thường (> 10× baseline tuần).

## 5. Chuẩn hạ tầng P1 (rút từ chuẩn chung, những điểm không được phép trượt)

- TLS mọi nơi, HSTS; CORS chỉ định origin cụ thể của dashboard — không `*` cho API có credential.
- Error production không leak stack trace/SQL/path (format chuẩn 05 §1.5); debug/Swagger không public.
- Secrets qua env/secret manager, có `.env.example`; log có correlation id, **không** chứa token/password/nội dung artifact.
- Container non-root, image pin version, healthcheck; backup Postgres hằng ngày + **restore thử trước Gate-1** (backup chưa restore thử = chưa có backup); RPO 24 h, RTO 4 h cho giai đoạn design partner.
- Render dữ liệu từ report (title, error, stack) luôn escape — stack trace là untrusted input, XSS qua tên test là vector thật.

## 6. Checklist ánh xạ OWASP API Top 10 (chạy trước mọi release)

| # | Rủi ro | Phòng thủ trong thiết kế này | Bằng chứng phải có |
|---|---|---|---|
| API1 BOLA | Tenant condition trong query + `authorize()` tập trung (§3.3) | Bộ test cross-tenant pass 100% |
| API2 AuthN | §2: argon2id, session revoke, rate limit login, MFA owner | Test brute-force bị chặn |
| API3 Property-level | DTO/serializer theo role — không serialize model thô; update qua struct chỉ chứa field cho phép | Test gửi `{"role":"org_owner"}` bị bỏ qua/400 |
| API4 Resource | Trần 05 §2.4 + cap artifact 08 §4 + quota + LLM cap 08 §3 | Test vượt từng trần trả đúng 413/429 |
| API5 Function-level | Ma trận §3.2, admin action check role không chỉ check login | Test viewer gọi endpoint maintainer → 403 |
| API6 Business flow | Đăng ký + mời member + forgot-password có rate limit; Free quota chặn farm storage | Ngưỡng ghi trong config, có test |
| API7 SSRF | Server không fetch URL client cung cấp (05 §9) — `ciRunUrl` chỉ render | Grep codebase: không có outbound fetch từ input |
| API8 Misconfig | §5; review config đủ lớp app → proxy → container → cloud → DB | Checklist ký trước release |
| API9 Inventory | Chỉ `/v1/`, spec OpenAPI là nguồn sự thật, token có lastUsedAt; endpoint bỏ = chặn thật + ngày tắt | Diff OpenAPI mỗi release |
| API10 Unsafe consumption | P1 phụ thuộc ngoài duy nhất: LLM provider + OAuth — response LLM validate schema trước khi lưu AiHypothesis (category phải thuộc enum 06 §7, confidence ∈ [0,1]); OAuth verify đầy đủ | Test LLM trả rác → hypothesis bị loại, không crash |

## 7. Rủi ro còn lại & khuyến nghị (cố ý chưa làm ở P1)

- Chưa có WAF/bot management — chấp nhận ở quy mô design partner; bật Cloudflare proxy khi mở đăng ký public.
- Chưa có audit log đầy đủ (P6) — P1 chỉ log các hành động phá hủy (xóa, revoke, merge) vào bảng đơn giản.
- Chưa pentest bên thứ ba — điều kiện: trước khách trả phí đầu tiên có dữ liệu production trong artifact, tối thiểu một vòng pentest thuê ngoài hoặc bug bounty private.
- HAR stripping là best-effort phía client — khách tắt đi thì token thật của họ nằm trong artifact; docs phải cảnh báo đậm và mặc định luôn bật.
