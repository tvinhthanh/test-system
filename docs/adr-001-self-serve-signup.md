# ADR-001: Self-serve sign-up với duyệt role (request queue)

**Status:** Accepted — phương án A, lát `join_org` (PR #9 merge 26-10-2026); 29-10-2026: xác minh email thành bắt buộc, không còn là việc để sau (SPEC N65)
**Date:** 2026-10-25
**Deciders:** Chủ dự án (tvinhthanh) + người chịu trách nhiệm bảo mật
**Tương ứng SPEC:** QĐ-17 (§1.17; QĐ-14 đã dùng cho "tester không cần source"), đảo lại ràng buộc "no self-signup" ở §11.2

> **Sửa 29-10-2026.** Bản build đầu để "human approval là cổng" và tạo tài khoản đã verify ngay khi gửi form. Sai: "email đã verify" là thứ Members, lời mời và OAuth tin là chủ địa chỉ, nên ai cũng chiếm trước được email của người khác. Nay form luôn trả `202`, email nhận link `/signup#<token>`, tài khoản chỉ được tạo khi mở link; `SIGNUP=on` cần `MAIL=smtp`. Chi tiết ở SPEC §1.17.

## Context

Hiện tại ATP **không có tự đăng ký**. Tài khoản chỉ đến từ: seed/admin CLI (owner đầu tiên), lời mời qua email, hoặc OAuth cho tài khoản đã tồn tại. §11.2 ghi rõ *"no self-signup"*, và code `authz/oauth.go`, `cmd/api/members.go` đều dựa trên giả định đó.

Nhu cầu mới: cho người dùng **tự đăng ký**, nhưng **không tự cấp quyền** cho mình — role phải được **duyệt**. Người đăng ký **chọn** scope + role mong muốn; admin **duyệt / đổi / từ chối**.

Các forces:
- Muốn giảm ma sát onboarding (khỏi phải admin tạo tay từng tài khoản) để bán/demo.
- Không được để người lạ tự có quyền trong tổ chức — đây là sản phẩm B2B đa tenant.
- Phải tái dùng tối đa hạ tầng đã có, tránh viết lại auth.

Primitive đã có để tái dùng:
- **Xác thực email:** `EmailVerifiedAt` (login từ chối nếu chưa verify) — `internal/authz/authz.go`.
- **RBAC hợp-membership:** user **không có grant nào → không vào được gì**. Nên "pending" = email đã verify + zero role, không cần trạng thái mới.
- **Cấp role + audit:** `POST /v1/{scope}/members {email, role}`, `role.grant`/`role.revoke` — `cmd/api/members.go`.
- **Mẫu hàng chờ duyệt:** Review Queue của test identity (`review list` / `review decide`) — tái dùng đúng pattern UI + API.
- **MFA bắt buộc cho `org_owner`** (tự áp khi ai đó được nâng lên owner).
- **Gửi mail:** công tắc `MAIL=smtp` (đã dùng cho invitation + forgot password).

## Decision

Thêm **self-serve sign-up (biến thể A)**:

1. Người dùng mở `/sign-up`, nhập email + mật khẩu, **chọn scope muốn vào (org/workspace/project) + role mong muốn**, kèm ghi chú tùy chọn.
2. Hệ thống tạo tài khoản **unverified**, gửi mail xác thực.
3. Bấm link → **verified** → đăng nhập được, nhưng **zero role** → thấy màn "Chờ duyệt". Đồng thời tạo một **access request (pending)**.
4. Admin của scope đó mở **hàng chờ duyệt**: **Approve** (cấp role đã xin **hoặc** role khác), **Deny**, đều ghi audit. Approve = cấp role qua đúng cơ chế members hiện có.

**Phạm vi chốt:** signup hỗ trợ **hai loại request**, cả hai đều phải được duyệt:

- **`join_org` — xin vào một org đã tồn tại** với role mong muốn → duyệt bởi **admin của org đó** (`member.manage`).
- **`create_org` — xin tự tạo org riêng** (thành owner của một org mới) → duyệt bởi **operator cấp nền tảng** (platform admin), vì org chưa tồn tại nên chưa có admin org nào để duyệt. Khi duyệt: hệ thống **tạo org mới + cấp `org_owner`** cho người xin → **MFA tự thành bắt buộc** cho họ (luật đã có).

Khác biệt cốt lõi hai loại là **ai duyệt**: `join_org` do admin org duyệt; `create_org` do operator nền tảng duyệt (bề mặt đặc quyền mới, phải giới hạn chặt).

## Options Considered

### Option A: Pick + duyệt qua request queue  ✅ (chọn)
Người dùng chọn role mong muốn khi đăng ký; admin duyệt qua một hàng chờ riêng.

| Dimension | Assessment |
|-----------|------------|
| Complexity | Medium — thêm bảng `access_requests`, endpoint list/decide, một màn hình |
| Cost | Vừa — nhưng tái dùng pattern Review Queue đã có |
| Scalability | Tốt — hàng chờ chịu được nhiều yêu cầu, có audit |
| Team familiarity | Cao — y hệt Review Queue test identity |

**Pros:** người dùng nói được nguyện vọng; dấu vết ý định rõ (request + lý do); admin không phải tự rà người mới; audit đầy đủ.
**Cons:** phải build bảng + hàng chờ + UI duyệt; có thêm một khái niệm "request" song song với "invitation".

### Option B: Admin tự upgrade (không pick, không hàng chờ)
Signup chỉ email + mật khẩu, rơi về zero role; admin nâng role từ màn Members.

| Dimension | Assessment |
|-----------|------------|
| Complexity | Low — gần như chỉ cần endpoint signup + danh sách "chờ" |
| Cost | Thấp |
| Scalability | Kém hơn — admin phải chủ động để ý user mới |
| Team familiarity | Cao — tái dùng Members |

**Pros:** ít công nhất; không thêm khái niệm mới.
**Cons:** user không nói được muốn gì; admin dễ bỏ sót người mới; dấu vết ý định mờ.

### Option C: Bản lai (signup + ghi chú, admin nâng từ tab "Chờ duyệt")
Giữa A và B: không có request object chính thức, nhưng signup kèm ghi chú và admin nâng từ một tab pending trong Members.

**Pros:** ít công hơn A, vẫn có chút ngữ cảnh.
**Cons:** không có trạng thái duyệt/từ chối tường minh, khó báo cáo; nửa vời khi lượng yêu cầu lớn.

### Sub-decision: vào org có sẵn vs tạo org mới
Chốt: **hỗ trợ cả hai, cả hai đều phải duyệt** (khác với bản nháp đầu chỉ cho join).
- **`join_org`** — duyệt bởi admin org đích. Rủi ro thấp.
- **`create_org`** — duyệt bởi **operator nền tảng**. Đây **không** phải SaaS self-serve tự do (ai đăng ký cũng có org ngay): vẫn có cổng duyệt của con người, nên tránh được spam org. Approve = tạo org + cấp `org_owner` + bật MFA bắt buộc.
- **Bác:** tạo org **tự động không cần duyệt** ngay khi signup — để sau, cần CAPTCHA/chống bot và chính sách dọn org rác; là một ADR riêng.

## Trade-off Analysis

A đắt hơn B/C nhưng là lựa chọn đúng vì **yêu cầu cốt lõi là "duyệt role"** — tức phải có một đối tượng request có trạng thái (pending/approved/denied) để duyệt và audit. B/C không mô hình hóa được "duyệt" một cách tường minh. Chi phí của A được giảm mạnh nhờ **tái dùng nguyên pattern Review Queue** đã chạy trong sản phẩm, nên rủi ro kỹ thuật thấp.

Điểm đánh đổi lớn nhất **không phải kỹ thuật mà là bảo mật/chính sách**: mở signup đảo lại §11.2. Phải bù bằng hàng rào chống lạm dụng (bên dưới) thì mới chấp nhận được.

## Security & chống lạm dụng (điều kiện để chấp nhận)

- **Bắt buộc verify email** trước mọi quyền (đã có; cần `MAIL=smtp`).
- **Allowlist domain email** cho signup (vd chỉ `@congty.com`) — biến "public" thành "công khai trong phạm vi".
- **Rate limit** signup theo IP + email (tái dùng limiter của login/forgot).
- **Zero-trust mặc định:** signup không bao giờ tự cấp role; luôn phải duyệt.
- **MFA:** nếu được duyệt lên `org_owner` thì MFA tự bắt buộc (luật đã có). Cân nhắc bắt MFA cho mọi role quản trị.
- **Signup có thể bật/tắt theo org** (`SIGNUP=off` mặc định) + có thể thu hồi link.
- **CAPTCHA/bot** nếu mở hẳn ra internet (ngoài phạm vi ADR này, ghi nhận).
- **OAuth:** giữ nguyên "không tạo tài khoản mới qua OAuth" trong phase đầu để giảm bề mặt; xét sau.
- **`create_org` là bề mặt đặc quyền:** chỉ operator nền tảng duyệt được; duyệt nhầm = một org mới + một owner mới. Phase đầu để duyệt **qua `cmd/admin`** (chỉ người có máy chủ mới làm được) thay vì mở UI. Tách công tắc riêng `SIGNUP_ALLOW_CREATE_ORG` để bật join mà vẫn khóa create.

## Consequences

**Dễ hơn:**
- Onboarding không cần admin tạo tay từng tài khoản.
- Có dấu vết ai xin quyền gì, ai duyệt, khi nào — audit sạch.

**Khó hơn / phải gánh:**
- Bề mặt tấn công mới (endpoint công khai tạo tài khoản) → bắt buộc có verify + rate limit + allowlist.
- Thêm một khái niệm "access request" song song "invitation" — phải tài liệu hóa khác biệt (invitation do admin khởi xướng; request do user khởi xướng).
- **Phải sửa SPEC §11.2** và các chỗ code/ghi chú khẳng định "no self-signup" (`authz.go`, `oauth.go`, `members.go`).
- Cần `MAIL=smtp` hoạt động thật — nếu không, không có đường verify.

**Phải xem lại sau:**
- Có mở create-new-org (SaaS self-serve thật) không — ADR riêng.
- Chính sách giữ tài khoản verified nhưng không bao giờ được duyệt (dọn rác sau N ngày?).

## Action Items

**Backend**
1. [ ] `POST /v1/session/signup` → tạo user unverified, gửi mail verify. Body mang **một trong hai**:
   - `type=join_org`: {email, password, scopeLevel, scopeId, requestedRole, note?}
   - `type=create_org`: {email, password, proposedOrgName, note?}
2. [ ] Khi verify xong: tạo bản ghi `access_requests` (pending) đúng `type`.
3. [ ] Migration `access_requests` (id, userId, **type**, scopeLevel?, scopeId?, requestedRole?, **proposedOrgName?**, note, status, decidedBy, decidedAt, createdAt). Cột scope/role nullable cho `create_org`; `proposedOrgName` nullable cho `join_org`. CHECK ràng theo `type`.
4. [ ] **Duyệt `join_org`:** `GET /v1/{scope}/access-requests` + `POST .../decide {decision, role?}` — gate `member.manage`; approve = cấp role qua đường members + audit.
5. [ ] **Duyệt `create_org`:** hàng chờ **cấp nền tảng** — chỉ operator (super-admin/`admin` CLU hoặc một quyền platform mới). Approve = tạo org (+ ws/project mặc định tùy chọn) + cấp `org_owner` cho người xin + buộc MFA; audit `org.create` + `role.grant`. Phase đầu có thể để **chỉ qua `cmd/admin`** cho gọn, UI sau.
6. [ ] Config: `SIGNUP=off|on`, `SIGNUP_ALLOWED_DOMAINS`, `SIGNUP_JOIN_SCOPE` (org cho join), `SIGNUP_ALLOW_CREATE_ORG=off|on`, rate-limit signup.

**Frontend**
7. [ ] `/sign-up` (SignUpPage.vue) + route (hiện chỉ có sign-in/invite/forgot). Form cho chọn **"Xin vào org có sẵn"** (nhập mã/link org + role) hoặc **"Tạo org mới"** (nhập tên org); theo `SIGNUP_ALLOW_CREATE_ORG` mà ẩn/hiện lựa chọn tạo org.
8. [ ] Màn "Chờ duyệt" cho user zero-role.
9. [ ] Màn "Access requests" cho admin org (dựng theo Review Queue). Hàng chờ `create_org` cấp nền tảng làm sau/qua CLI.

**Tài liệu/bảo mật**
10. [ ] Cập nhật SPEC §11.2 (QĐ-14) + sửa các ghi chú "no self-signup" trong code.
11. [ ] Ghi rõ ba khái niệm: invitation (admin khởi xướng), access request `join_org` (user khởi xướng, admin org duyệt), access request `create_org` (user khởi xướng, operator nền tảng duyệt).

> Trạng thái: **Proposed** — để bàn. Chưa code cho tới khi chốt ADR và cập nhật SPEC.
