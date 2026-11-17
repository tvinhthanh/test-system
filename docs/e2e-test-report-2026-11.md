# Báo cáo kiểm thử đầu-cuối — 16/11/2026

> Kiểm thử các luồng chính của ATP trên một stack đang chạy thật, bản `main` tại `f635803`
> (đã áp migration tới `0051`). Mỗi luồng chạy bằng script gọi thẳng API như trình duyệt
> hoặc CI sẽ gọi: có cookie phiên, CSRF, Origin, token Bearer. Không dùng mock.

## Tóm tắt

| Luồng | Số bước | Kết quả |
|---|---|---|
| Phân quyền 6 role | 6 role × 4 thao tác | Đạt |
| Tự đăng ký qua mail (QĐ-17) | 11 | Đạt |
| Mời thành viên qua mail | 11 | Đạt |
| Quên / đặt lại mật khẩu qua mail | 12 | Đạt |
| Bật MFA cho owner | 15 | Đạt |
| CI token và reporter | 10 | Đạt |
| Triage lỗi | 12 | Đạt (có khoảng trống sản phẩm, xem dưới) |
| Release gate cho CI | 11 | Đạt |
| Import JUnit | 10 | Đạt |
| Quản lý test: plan và execution | 13 | Đạt |
| Auto test (QĐ-16, QĐ-19) | 10 | Đạt |

**Không tìm thấy lỗi sản phẩm nào cần sửa.** Mọi bước từng báo FAIL đều do script kiểm thử
đọc sai tên trường hoặc đoán sai mã lỗi; đã kiểm lại từng trường hợp trong code, SPEC hoặc DB.

## Môi trường

- Docker Desktop trên Windows, stack `server/atp.sh` (api, worker, postgres, caddy, prometheus,
  backup, checker). `APP_ENV=dev`, artifact `OBJSTORE=memory`.
- Mail qua **Mailpit** (hộp mail giả trên cùng mạng Docker, `SMTP_TLS=none`) — chỉ dùng được
  vì `APP_ENV=dev`.
- Truy cập từ mạng ngoài qua Cloudflare quick tunnel; luồng reporter và CI chạy qua URL công khai đó.
- Tài khoản test cho cả 6 role, tạo bằng `cmd/admin` với mật khẩu ngẫu nhiên. Owner dùng
  cho MFA nằm ở một tổ chức test riêng để không đụng owner thật.

## Kết quả từng luồng

### Phân quyền

| Role | Xem run | Tạo test case | Xem thành viên | Xem CI token |
|---|---|---|---|---|
| `org_owner` | — (đăng nhập dừng ở bước 2FA, đúng luật) | — | — | — |
| `org_admin` | 200 | 201 | 200 | 200 |
| `workspace_admin` | 200 | 201 | 200 | 200 |
| `maintainer` | 200 | 201 | 403 | 200 |
| `member` | 200 | 201 | 403 | 403 |
| `viewer` | 200 | 403 | 403 | 403 |

Mã trả về khớp `actions[]` server gửi cho từng role. `maintainer` có `data.delete` còn
`workspace_admin` không: đúng chủ ý (`rules.go`: "ws_admin manages, does not destroy", §11.3.2).

### Tự đăng ký qua mail

Form luôn trả 202; mail "Finish signing up for ATP" mang link `/signup#<token>` theo `PUBLIC_URL`.
Token sai, dùng lại link → 404. Sau khi đặt mật khẩu: đăng nhập được nhưng không thấy project
nào (run → 404) cho tới khi admin duyệt; duyệt xong đúng quyền `member`. Đăng ký lại email đã có
vẫn 202 và không gửi mail thứ hai — không dò được email nào tồn tại.

### Mời thành viên qua mail

Viewer mời → 403. Admin mời email chưa có tài khoản → 202, lời mời hạn 7 ngày, mail
"You're invited to web-app (Engineering) on ATP". Trang mời đọc đúng email, role, project, người
mời. Nhận lời → có quyền `maintainer` ngay, không cần duyệt. Token sai / dùng lại / đã thu hồi → 404.
Org admin mời người làm owner → 403 `OWNER_ONLY`.

### Quên / đặt lại mật khẩu

Luôn 202, email không tồn tại không nhận mail. Yêu cầu lần hai làm link cũ chết. Token sai,
mật khẩu yếu, dùng lại link đều bị từ chối. Đặt lại xong: phiên đang mở bị đăng xuất, mật khẩu cũ
401, mật khẩu mới 200. Quá 5 yêu cầu trong 15 phút từ một IP → 429.

### Bật MFA cho owner

Owner chưa bật MFA đăng nhập được nhưng mọi màn khác trả 403 `MFA_ENROLLMENT_REQUIRED`.
Setup trả secret + `otpauth://`. Mã sai không bật; mã đúng bật và trả 10 mã khôi phục.
Bật rồi thì setup lại → 409, owner tự tắt → 409 `MFA_MANDATORY`. Lần đăng nhập sau: mật khẩu chỉ
ra challenge (202), mã sai 400, mã đúng 200, cùng một mã không dùng lại được, mã khôi phục dùng
được một lần. Mã TOTP do script tự tính theo RFC 6238, nên cũng xác nhận đồng hồ server đúng.

### CI token và reporter

Member tạo token → 403. Maintainer tạo → secret hiện một lần, quyền `project:ingest`, hạn 1 năm;
danh sách chỉ hiện prefix. Token CI đọc dashboard → 401. Reporter Playwright chạy qua URL công khai:
5 event, 9 artifact, không lỗi; run hiện trên dashboard; token ghi `lastUsedAt`. Thu hồi → API 401;
reporter với token hỏng không đẩy được run nhưng **mã thoát của bộ test không đổi**.

### Triage lỗi

Chi tiết lỗi có đủ các lần chạy, thông báo lỗi, trace và file đính kèm; lịch sử test qua các run
nhận ra lỗi lặp lại. Viewer chỉ được "đã xem" (phân loại → 403). "Đã xem" bấm lại trong ngày không
ghi trùng. Gắn known issue, đánh dấu flaky, đánh dấu đã xử lý đều được ghi; người làm và giờ lấy
từ phiên, gửi kèm tên/giờ giả bị bỏ qua. Loại sự kiện lạ → 400. Điều tra AI khi chưa có khóa →
409 `AI_NOT_CONFIGURED`.

### Release gate cho CI

Chưa chấm → `ci.pass=false`. Có lỗi mới chưa ai xử lý → `blocked`. Gắn known issue rồi chấm lại →
`warn` (flaky 33,3% > 3%), CI qua. Member quyết định → 403. Người chặn tay → CI dừng dù cổng qua;
người duyệt → CI qua, ghi `decidedBy`. Token không có `project:read` đọc cổng → 404 (chủ ý:
"the release does not exist for you"). CI chỉ cần một dòng:

```bash
curl -s -H "Authorization: Bearer $ATP_TOKEN" "$ATP_URL/v1/releases/$RELEASE_ID/gate" | jq -e .ci.pass
```

### Import JUnit

Member → 403. File pytest qua dashboard → run mang **ngày trong file**, đúng số pass/fail/skip,
giữ thông báo lỗi. Import lại cùng file → 200, không tạo run trùng. File Maven Surefire qua token:
`flakyFailure` thành test flaky có 2 lần chạy. File không có ngày (test runner của Node) → 400 trừ
khi truyền `startedAt`. XML hỏng → 400 `INVALID_JUNIT`. Báo cáo ngày hôm sau nối vào lịch sử cùng test.

### Quản lý test: plan và execution

Requirement p0 → 2 case có bước `{action, expected}` → plan 2 case. Giao việc cho viewer bị từ
chối. "passed" kèm bước fail → 400. Chạy fail ở bước 3 kèm link bug và ảnh bằng chứng; file đuôi
`.png` nhưng không phải ảnh → 400. Sửa case (thêm bước OTP); sửa đè bản cũ → 409. Retest nối với lần
fail. Mỗi execution **chụp lại case tại lúc chạy** (lần fail giữ 3 bước cũ, retest 4 bước mới).
Đóng plan → không đổi phân công, không ghi kết quả (409).

### Auto test

Environment trỏ `https://demo.playwright.dev`. Viewer sửa bước → 403; bước `run_shell` → 400
(chỉ nhận danh sách thao tác cố định). 2 case × `chromium/desktop` + `webkit/phone`: xong sau
20 giây, 2 pass, 2 fail (case cố ý sai), 0 lỗi chạy; ảnh chụp tải được; ghi thành test run và plan.
Bước kiểm tra trả ghi chú cụ thể: accessibility không có lỗi mức critical; LCP 716 ms (desktop) và
1,2 s (phone); bảo mật đạt nhưng báo thiếu CSP và chống clickjacking (mức trung bình).

Lần chạy đầu nhắm vào `example.com` và fail đúng: trang đã đổi giao diện, không còn heading được
kiểm. Trang đó cũng ghi rõ không dùng để test, nên đã đổi sang trang demo của Playwright.

## Khoảng trống sản phẩm

- **Không có chỗ cho người tự ghi nguyên nhân lỗi.** Độ phủ triage và TTRC chỉ đếm
  `HYPOTHESIS_ACCEPTED` (SPEC N63), mà giả thuyết hiện chỉ đến từ AI hoặc `cmd/admin`. Không có
  khóa AI thì chỉ số này luôn 0% dù team xử lý lỗi đầy đủ. Cổng release không bị ảnh hưởng (đếm cả
  4 loại quyết định).
- **Đăng nhập owner bằng API cần mã 2FA**, nên các luồng tự động không đi được bằng tài khoản owner;
  dùng `org_admin` cho các thao tác quản trị.

## Lưu ý vận hành phát hiện khi test

- Mailpit dựng tay cần `--restart unless-stopped`, nếu không sau khi máy khởi động lại mail mời và
  đặt lại mật khẩu sẽ hỏng.
- Quick tunnel đổi URL mỗi lần `cloudflared` khởi động lại; mail đã gửi mang URL cũ sẽ chết.
  Muốn URL cố định cần named tunnel gắn domain.
- Sau tunnel, Caddyfile cần `tls internal` và site `localhost` (SNI của tunnel). Không đưa hai dòng
  này lên `main` vì chúng chặn Let's Encrypt khi dùng domain thật.
- `server/atp.sh up` chờ health ở cổng 443; khi chạy ở cổng khác (`HTTPS_PORT`) script báo lỗi dù
  stack đã lên.

## Dữ liệu test để lại trên demo

Trong project `web-app` của tổ chức "My Company": test case "RBAC check (…)", 4 case auto test,
requirement và plan quản lý test, release `gate-test-…`, environment `playwright-demo` và
`example-site` (đã xóa địa chỉ), các run từ reporter / JUnit / auto test, sự kiện triage. Tổ chức
"MFA Test Org" chỉ dùng cho bài test MFA. Mọi token CI tạo trong lúc test đã thu hồi.
