# Hướng dẫn sử dụng ATP

ATP (Report Center) là nơi cả nhóm test làm việc trên cùng một dashboard. Tester viết case, lên plan, chạy test thủ công trên site thật và ghi kết quả kèm bằng chứng. Hệ thống cũng có thể **tự chạy** test case trong trình duyệt thật (Auto test). Người viết automation đẩy kết quả Playwright từ CI vào. Maintainer dựa vào đó quyết định có release hay không. Tester không cần source code ATP, không cần biết code.

Tên nút và menu dưới đây giữ nguyên tiếng Anh như trên giao diện và được **in đậm**. Trong app, mục **How to test** ở thanh bên trái là bản tóm tắt ngắn của tài liệu này.

---

## 1. Bắt đầu

### 1.1 Đăng nhập

1. Mở địa chỉ server do admin gửi, ví dụ `https://atp.congty.vn` hoặc `https://192.168.1.20`.
   - Server dùng chứng chỉ nội bộ (ví dụ cài trong mạng LAN) thì trình duyệt sẽ cảnh báo. Khi đó admin chạy `bash server/atp.sh ca` để xuất chứng chỉ, rồi cài vào máy của bạn một lần.
2. Nhập **Email** và **Password**, bấm **Sign in**.
   - Nếu server đã bật đăng nhập bằng Google hoặc GitHub, sẽ có thêm nút **Continue with Google / GitHub**. Chỉ dùng được với tài khoản đã có sẵn trong ATP.
   - Quên mật khẩu: bấm **Forgot password?**. Chức năng này chỉ chạy khi server đã cấu hình gửi email.
   - Chưa có tài khoản: nếu server mở tự đăng ký, bên dưới nút **Sign in** có dòng **New here? Ask to join**. Cách làm ở mục 1.4.
3. Nếu bạn đã bật xác thực hai bước, nhập mã 6 số từ ứng dụng authenticator. Nếu mất điện thoại, nhập một recovery code thay cho mã. Mỗi recovery code chỉ dùng được một lần.

> **Organization owner bắt buộc bật xác thực hai bước.** Lần đăng nhập đầu, app tự mở trang **Security**. Bấm **Set up two-factor authentication**, quét mã QR bằng Google Authenticator, 1Password, Authy hoặc Microsoft Authenticator, rồi nhập mã 6 số và bấm **Turn on**. **Lưu 10 recovery code ngay lúc đó**, bằng **Copy** hoặc **Download .txt**, vì chúng chỉ hiện đúng một lần. Chưa bật xong thì không mở được project nào.

### 1.2 Cách tổ chức: organization → workspace → project

```
Organization (công ty)
└── Workspace (nhóm / phòng)
    └── Project (một sản phẩm, một app)
```

- Mọi dữ liệu test (case, plan, run, release) đều thuộc về **một project**.
- Trang đầu (**Projects**) liệt kê các project bạn được xem. Bấm vào một project để vào làm việc.
- Dòng breadcrumb trên cùng (`Workspace › tên workspace › tên project › màn hình`) cho biết bạn đang ở đâu.

### 1.3 Vai trò: ai làm được gì

| Vai trò (trên giao diện) | Làm được |
|---|---|
| **viewer** | Chỉ xem: plan, kết quả, báo cáo. |
| **member** (tester) | Thêm việc test: viết case và plan, ghi kết quả và bằng chứng, triage lỗi, chạy URL check, thêm environment, nhờ AI soạn case, quản lý tài khoản test, chạy auto test và quét site. |
| **maintainer** | Thêm quyền của member, cộng thêm: quyết định release, cấp CI token, gộp test identity, cài đặt project. |
| **workspace admin** | Tạo project và quản lý member trong workspace. |
| **org admin** | Quản lý workspace, member và token trong cả organization; cài đặt AI. |
| **org owner** | Toàn quyền, kể cả trao vai trò owner và xem **Usage**. Bắt buộc bật xác thực hai bước. |

Vai trò được kế thừa xuống dưới: ai là org admin thì cũng là admin của mọi workspace và project bên trong.

**Thanh bên trái** chỉ hiện những mục bạn có quyền dùng:
- **Start testing**: member trở lên.
- **CI tokens**: maintainer trở lên.
- **Members**: người quản lý member của project.
- **Team**: admin của organization hoặc workspace.

---

### 1.4 Tự đăng ký (khi server mở)

Chỉ có khi người vận hành bật `SIGNUP=on` (xem mục 10).

1. Mở **link đăng ký** mà admin của nhóm gửi, dạng `https://<server>/signup?project=…`. Không có link thì bấm **Ask to join** ở trang đăng nhập, chọn loại (project, workspace hay organization) rồi dán id do admin đưa.
2. Nhập email, chọn **Role you ask for**, có thể ghi vài dòng cho admin (bạn là ai, cần làm gì), rồi bấm **Send me the link**.
3. Mở email, bấm link (dùng một lần, hết hạn sau 24 giờ), đặt mật khẩu rồi bấm **Create account**.
4. Bạn đã đăng nhập, nhưng chưa thấy project nào. Khi admin duyệt yêu cầu, project sẽ hiện trong dashboard.

> Trang luôn báo "Check your email", kể cả khi email đó đã có tài khoản. Không nhận được thư thì có thể bạn đã có tài khoản rồi: dùng **Forgot password?**.

## 2. Dành cho admin: dựng nhóm

### 2.1 Thêm người

1. Vào **Team**, chọn organization hoặc workspace. Với một project cụ thể, vào project đó rồi chọn **Members**.
2. Nhập email, chọn **Role**, bấm **Add**. Thêm lại một người đã có trong danh sách thì chỉ đổi vai trò của họ.
   - Nếu server đã cấu hình email: người chưa có tài khoản sẽ nhận thư mời, hiệu lực 7 ngày. Họ đặt mật khẩu và vào thẳng. Danh sách **Pending invitations** cho phép **Withdraw** (rút lời mời).
   - Nếu server chưa cấu hình email: ô nhập có nhãn **Email of an existing account**. Người vận hành tạo tài khoản trước bằng lệnh:
     `bash server/atp.sh admin user create -email ten@congty.vn -password '…' -verified`
3. Muốn đổi vai trò, chọn lại trong cột **Role**. Muốn xóa người, bấm **Remove** rồi **Confirm**.
4. Nếu server mở tự đăng ký, cùng màn hình có mục **Requests to join**:
   - **Link đăng ký** của project, workspace hoặc organization này: bấm **Copy** rồi gửi cho người cần vào.
   - Mỗi yêu cầu hiện email (đã được xác minh qua thư), vai trò họ xin, ghi chú và thời điểm gửi. Có thể chọn vai trò khác trước khi bấm **Approve**. Bấm **Deny** rồi **Confirm** để từ chối.
   - Chỉ organization owner mới cấp được vai trò owner. Admin vẫn duyệt được yêu cầu xin owner, nhưng với vai trò org admin.

### 2.2 Tạo project

- Vào **Team**, chọn workspace, điền ô **New project in this workspace**, rồi bấm **Create project**.
- Project đã xóa nằm trong danh sách project bị xóa của workspace và khôi phục được trong 30 ngày.
- Workspace mới được tạo bằng lệnh của người vận hành: `bash server/atp.sh admin ws create -org <org-id> -name "Tên"`.

### 2.3 Bật AI soạn test case (không bắt buộc)

Vào **Team**, chọn organization, bấm **AI settings**. Trang này chỉ hiện với org owner và org admin.

1. **Provider**: chọn nơi cấp key: Anthropic (Claude), OpenAI (GPT), Google (Gemini), xAI (Grok), Mistral, DeepSeek, Groq, hoặc OpenRouter. OpenRouter dùng một key cho hàng trăm model của nhiều hãng.
   - Dán key trước cũng được. Key có phần đầu riêng (`sk-ant-`, `sk-proj-`, `AIza`, `xai-`, `gsk_`, `sk-or-`) thì trang tự chuyển sang đúng provider.
   - Server có endpoint riêng tương thích OpenAI (gateway công ty, Ollama, vLLM) thì danh sách có thêm mục **OpenAI-compatible endpoint (this server)**.
2. **Model**: chọn model trong danh sách gợi ý của provider.
   - Với Anthropic: Claude Opus 5.5 (mặc định), Sonnet 5.5, Haiku 5.5 (nhanh và rẻ nhất), Fable 5.1.
   - Với các provider khác có thêm ô **Or type any model id this key can use**: gõ bất kỳ model nào key dùng được, ví dụ `openai/gpt-6-astra` trên OpenRouter.
3. **Whose API key pays**: chọn key trả tiền cho mỗi lần soạn.
   - **The server's key**: dùng key người vận hành đặt trong `LLM_API_KEY`. Key này chỉ dùng được với provider của server (`LLM_PROVIDER`); với provider khác lựa chọn bị mờ và ghi rõ key của server thuộc provider nào.
   - **Our own key**: dán key của organization. Trang ghi đường dẫn lấy key của provider đang chọn.
4. Bấm **Save**.
   - Trước khi lưu, ATP hỏi chính provider đó xem key có đúng và dùng được model không. Key sai, hết tiền, hoặc model không có, bạn sẽ thấy lỗi ngay. Gõ sai tên model thì lỗi kể vài model mà key dùng được.
   - Key của provider này dán vào provider khác (ví dụ key `sk-proj-…` khi đang chọn Anthropic) bị từ chối, kèm lời nhắc chọn đúng provider.
   - Key riêng được mã hóa khi lưu. Sau đó app chỉ hiện 4 ký tự cuối. Đổi provider thì phải dán key mới cho provider đó.
   - Chuyển sang dùng key của server thì key riêng bị xóa.
   - **Reset** xóa toàn bộ cài đặt AI của organization.

Dữ liệu gửi cho provider đang dùng: chỉ khi tester bấm **Draft cases**. Gồm loại test, mô tả tester viết, và (nếu server bật URL check) cấu trúc trang: tiêu đề, heading, link, ô nhập, nút bấm, đoạn đầu nội dung. Ngoài ra không gửi gì khác từ ATP.

### 2.4 CI token (cho automation)

Xem [mục 7](#7-dành-cho-người-viết-automation-playwright).

---

## 3. Cách nhanh nhất: Start testing

Từ thanh bên trái, bấm **Start testing**. Một vòng test gồm 4 bước trên cùng một trang.

### Bước 1: What are you testing? (loại test)

Có 10 loại:

| Loại | Dùng khi |
|---|---|
| **Smoke** | Kiểm nhanh các luồng chính còn sống sau mỗi lần deploy |
| **Regression** | Kiểm lại những gì đã chạy đúng, trước một release |
| **End to end** | Đi trọn một hành trình của người dùng, từ đầu đến cuối |
| **Functional** | Kiểm một tính năng theo yêu cầu |
| **Performance** | Tốc độ tải và độ mượt |
| **Security** | Đăng nhập, phân quyền, dữ liệu nhạy cảm |
| **Usability** | Dễ hiểu, dễ dùng |
| **Accessibility** | Bàn phím, trình đọc màn hình, độ tương phản |
| **Compatibility** | Các trình duyệt, điện thoại, kích thước màn hình |
| **Recovery** | Mất mạng, lỗi server, thao tác bị gián đoạn |

### Bước 2: Which site? (site cần test)

- Chọn một **environment**, tức một site có địa chỉ, ví dụ `staging` → `https://staging.congty.vn`.
- Chưa có thì chọn **+ New environment…**, nhập **Key** (chữ thường, số, dấu gạch ngang, ví dụ `staging`) và **Address**, rồi bấm **Add**.
- Màn hình chạy test sẽ mở địa chỉ này. AI cũng đọc trang ở địa chỉ này để soạn case.

### Bước 3: Which cases? (chọn case từ 3 nguồn)

| Tab | Là gì |
|---|---|
| **Your library** | Case đã có trong project và đã gắn loại test vừa chọn. |
| **Starter set** | 3–5 case mẫu viết sẵn cho loại test đó. Bạn sửa lại cho hợp sản phẩm sau. |
| **AI suggestions** | AI soạn case từ site và mô tả của bạn. |

Cách dùng **AI suggestions**:
1. Ô **What should the cases cover?**: mô tả cần test gì. Viết tiếng Việt cũng được, AI sẽ trả case bằng tiếng Việt. Ví dụ: *"Đăng nhập, giỏ hàng và thanh toán bằng thẻ; người dùng mới và người dùng cũ"*.
2. Ô **How many**: chọn 5, 8 hoặc 12 case.
3. Bấm **Draft cases**. AI thường soạn xong trong dưới một phút.
4. Đọc kỹ từng bản nháp, chỉ tích những case muốn giữ. **Đây là gợi ý, không phải đáp án.**

Ở tab **Starter set** và **AI suggestions**:
- Case nào trùng tiêu đề với case đã có trong thư viện sẽ có nhãn **already in your library** và không được tích sẵn.
- Case mẫu hoặc case AI chỉ thành test case thật khi bạn tích nó **và** bấm tạo plan. Khi đó case được gắn đúng loại test, nên lần sau sẽ hiện trong **Your library**.

Nếu tab AI báo *"AI drafting is not set up"* thì admin cần làm [mục 2.3](#23-bật-ai-soạn-test-case-không-bắt-buộc).

Giới hạn để tránh tốn tiền:
- Mỗi project được 30 lần soạn mỗi ngày.
- Tối đa 2 yêu cầu đang chờ cùng lúc.

### Bước 4: Start

Kiểm lại **Plan name** (đã được điền sẵn), rồi bấm **Create plan and start (N)**. ATP tạo plan và mở ngay **màn hình chạy test**.

---

## 4. Màn hình chạy test (Run)

Màn hình này dùng cho từng plan. Vào bằng Start testing, hoặc bằng nút **Run** trên trang plan.

- **Open \<địa chỉ site\>**: mở site cần test trong tab mới. Bạn test trên tab đó và ghi kết quả ở tab ATP.
- **Thanh tiến độ**: cho biết đã ghi kết quả bao nhiêu case trên tổng số (`N of M recorded`).
- **Danh sách case bên trái**: bấm vào case nào thì làm case đó. Nút **Next** bỏ qua case hiện tại.
- **Ghi kết quả** cho mỗi case:
  1. Đọc **Preconditions** (điều kiện trước) và **Test data** nếu có.
  2. Làm từng bước. Với mỗi bước, so với **Expected**, chọn trạng thái và ghi kết quả thực tế.
  3. Ghi **Note** nếu cần. Nếu case fail, dán link bug từ Jira, GitHub hoặc tracker khác vào ô **Bug URL**.
  4. Bấm một trong bốn nút **passed**, **failed**, **blocked** hoặc **skipped**.
     - Nút **passed** bị khóa khi còn bước nào failed hoặc blocked.
  5. ATP tự chuyển sang case kế tiếp chưa có kết quả.
- **Attach screenshot or video**: đính kèm bằng chứng vào kết quả vừa ghi. Nhận PNG, JPEG, WebP, MP4, WebM, PDF, TXT.
- **URL check**: chỉ có với loại test có phần kiểm tự động. Nút này cho máy chủ mở site và chấm nhanh; xem [mục 5.5](#55-url-checks-kiểm-nhanh-một-trang).
- Ghi xong mọi case thì app hiện *"Every case has a result"*, kèm link về plan và link sang trang quyết định release.

---

## 5. Làm từng bước (khi cần kiểm soát kỹ hơn)

Mục **Test management** có 6 tab: **Test cases**, **Requirements**, **Test plans**, **Releases**, **URL checks**, **Specialized results**.

### 5.1 Test cases

- **New case**: nhập các thông tin sau, rồi bấm **Create**.
  - **Title**, **Suite**, **Priority** (p0 là quan trọng nhất, p3 thấp nhất).
  - **Critical flow**: luồng sống còn. Release gate theo dõi riêng các case này.
  - **Phân loại**: loại test như smoke, regression, end to end…, mục đích, v.v.
  - **Các bước**: mỗi bước gồm hành động và kết quả mong đợi.
- **Import CSV**: nhập case từ Excel, TestRail hoặc bảng wiki.
  - Các cột: `key`, `title`, `suite`, `steps`, `expected`, `priority`, `critical`, `requirement`.
  - Cột `steps` viết theo dạng `hành động => kết quả`, mỗi bước một dòng hoặc ngăn cách bằng `|`.
  - Dòng có `key` sẽ cập nhật đúng case đó khi import lại. Dòng không có `key` được ghép theo tiêu đề.
- **New suite**: tạo nhóm case.
- **Lọc và tìm**: theo tiêu đề hoặc key, suite, trạng thái automation, critical, phân loại.
- **Thao tác hàng loạt**: tích chọn nhiều case, rồi đổi priority, suite, critical, trạng thái automation hoặc phân loại. Có thể xuất CSV (**Export selected** / **Export filtered**).
- Mở một case để sửa nội dung, xem lịch sử thay đổi và lịch sử chạy.

### 5.2 Requirements (không bắt buộc)

- **New requirement** gồm **Key**, **Title** và **Priority**.
- Gắn case vào requirement để biết yêu cầu nào đã có case kiểm, yêu cầu nào chưa (cột **Covered by**).
- Release gate theo dõi độ phủ của các requirement **p0**.

### 5.3 Test plans

1. Bấm **New plan**, đặt tên vòng test, ví dụ *"Release 2026.11 regression"*.
2. Chọn **Environment (site under test)**: site được test trong vòng này.
3. Tùy chọn: tích **Include current active cases** để đưa toàn bộ case đang dùng vào plan.
4. Bấm **Create**.

Trên trang plan:
- **Run**: mở màn hình chạy test ([mục 4](#4-màn-hình-chạy-test-run)).
- **Site under test**: bấm để mở site. **Edit address** sửa địa chỉ của environment.
- **Plan scope**: thêm hoặc bớt case, rồi bấm **Save scope**. Case đã có kết quả thì không gỡ khỏi plan được.
- **Assign**: giao case cho tester theo email. Tester bấm **Start** khi bắt đầu làm.
- **Record execution**: ghi kết quả ngay trên trang plan, giống màn hình chạy test. Bấm nút kẹp giấy để đính kèm bằng chứng.
- **Retest**: khi bug đã được sửa, tích **Retest latest execution** rồi ghi lại kết quả. Lịch sử giữ cả lần fail lẫn lần retest.
- Ô thống kê: **Pending**, **In progress**, **Passed**, **Failed**, **Blocked**, **Skipped**.
- **Close plan**: đóng plan khi xong vòng test. Plan đã đóng thì không ghi thêm kết quả được. **Reopen** để mở lại.

### 5.4 Releases: quyết định có release không

Việc này dành cho maintainer trở lên.

1. Bấm **New release**, nhập **Version** (bắt buộc) và **Branch** (không bắt buộc), rồi bấm **Create**.
2. Mở release, bấm **Evaluate gate**.
   - ATP đọc lần chạy automation mới nhất và áp 5 quy tắc: case critical bị hỏng lại, fail chưa ai triage, tỉ lệ flaky, độ phủ requirement p0, v.v.
   - Gate chỉ đưa ra khuyến nghị, **không tự quyết**.
3. Ở phần **Decision**, ghi **Note** (lý do), rồi chọn một trong ba nút:
   - **Approve**: cho release.
   - **Block**: chặn release.
   - **Mark shipped**: đã phát hành. Đây là quyết định cuối cùng.

   Quyết định được ghi kèm tên người quyết. Nếu bạn quyết khác với gate, trang sẽ hiện rõ *"Gate overridden"*.
4. Nếu chỉ test thủ công, kết quả trong plan là bằng chứng cho quyết định.
5. Phần **Gate for CI** có sẵn đoạn lệnh để pipeline tự kiểm gate trước khi deploy. Cần một CI token có quyền đọc gate.

### 5.5 URL checks: kiểm nhanh một trang

Không cần viết case. Nhập địa chỉ vào **Address to check** (có thể chọn environment), rồi bấm **Check**. Một trình duyệt thật trên server sẽ mở trang và báo cáo trong vài giây:

- Trang có tải được không, chứng chỉ HTTPS có hợp lệ không.
- Lỗi JavaScript; ảnh và script tải hỏng; link hỏng (40 link đầu).
- Accessibility theo chuẩn WCAG A/AA.
- Tốc độ: thời gian có byte đầu tiên, thời gian hiện nội dung chính, độ xô lệch bố cục.
- Thông tin cơ bản của trang: tiêu đề, ngôn ngữ, mô tả, thẻ h1.
- Có vừa màn hình điện thoại không. Kèm ảnh chụp trên máy tính và trên điện thoại.

Một số giới hạn:
- Mỗi lần check chỉ kiểm **một trang**, không phải cả site. Báo cáo được giữ 90 ngày.
- Nếu thấy *"URL checks are off on this server"*: người vận hành cần đặt `URLCHECK=on` trong `backend/deploy/.env` rồi chạy `bash server/atp.sh up`.
- Địa chỉ nội bộ (localhost, IP mạng LAN) bị chặn để bảo vệ server. Trường hợp riêng: staging nằm trong LAN văn phòng thì người vận hành bật `URLCHECK_ALLOW_PRIVATE=on`.

### 5.6 Specialized results

Bạn chạy performance, security, accessibility, compatibility, recovery hay usability bằng công cụ khác? Bấm **New result** để ghi kết quả và đính kèm báo cáo gốc. Nếu công cụ xuất báo cáo theo định dạng ATP thì dùng **Import ATP JSON**.

---

## 6. Auto test: hệ thống tự chạy test

Mở mục **Auto test** trên thanh bên trái. Ở đây hệ thống **tự làm các bước** của test case trong một trình duyệt thật trên server, chụp ảnh và ghi pass/fail.

Điều kiện:
- Người vận hành phải bật `URLCHECK=on`.
- Phần AI (soạn case, sửa bước, tìm cách đăng nhập) cần cấu hình như [mục 2.3](#23-bật-ai-soạn-test-case-không-bắt-buộc). Không có AI thì case tự viết vẫn chạy.

> **Các bước chạy thật trên site:** tin nhắn được gửi thật, form được lưu thật. Hãy chạy trên environment staging hoặc test, đừng chạy trên production.

### 6.1 Chọn site và tài khoản test

1. Ở đầu trang, chọn **Environment (site under test)**. Environment phải có địa chỉ.
2. Nếu trang cần đăng nhập (chat, giỏ hàng…), bấm **Test accounts** → **Add account**, điền các ô sau rồi bấm **Save account**:
   - **Key**, ví dụ `default`;
   - **User name or email**;
   - **Password**;
   - **Sign-in page**, ví dụ `/login`.

   Mật khẩu được mã hóa, không bao giờ hiện lại và không bao giờ gửi cho AI.
3. Bấm **Find sign-in steps**. AI đọc trang đăng nhập rồi viết các bước, dùng `{{username}}` và `{{password}}` thay cho giá trị thật. Sau đó hệ thống thử đăng nhập bằng các bước này. Trạng thái **ok** nghĩa là dùng được.

   Không có AI thì bấm **Steps** để tự viết: gõ `{{username}}` vào ô email, gõ `{{password}}` vào ô mật khẩu, rồi bấm nút đăng nhập.

### 6.2 Để AI soạn case (ví dụ: test chat)

1. Vào tab **Cases**, bấm **Generate with AI**.
2. Ô **Where it starts**: trang cần test, ví dụ `/message?to={{nguoi_nhan}}`.
3. Ô **Signed in as**: chọn tài khoản test nếu trang cần đăng nhập.
4. Phần **Inputs**: bấm **Add input** cho từng biến, ví dụ `nguoi_nhan` = `bob`, `noi_dung` = `Xin chào`.
   - Viết `{{random}}` trong giá trị để mỗi lần chạy ra một giá trị khác, ví dụ `test+{{random}}@example.com`.
5. Ô **What should the cases cover?**: mô tả cần test gì, ví dụ *"gửi tin thường, tin dài, emoji; tin rỗng không được gửi"*.
6. Bấm **Draft cases**. Hệ thống mở trang thật (đăng nhập trước nếu đã chọn tài khoản), AI đọc trang và soạn case.
7. Đọc từng case. Bấm **Edit** để sửa bước; bỏ tích những case không muốn giữ.
8. Bấm **Keep N cases** để lưu, hoặc **Keep and run** để lưu và chạy ngay.

### 6.3 Tự viết hoặc sửa case

Bấm **New case** để viết case mới, hoặc **Edit** trên một case có sẵn.

Các ô cần điền:
- **Title**, **Priority**, **Kind of testing**: chỉ khi tạo case mới.
- **Where it starts**, **Signed in as**, **Inputs**: giống [mục 6.2](#62-để-ai-soạn-case-ví-dụ-test-chat).
- **Steps**: mỗi bước chọn một hành động và một phần tử trên trang.

| Hành động | Dùng khi |
|---|---|
| **Click**, **Type into**, **Choose in**, **Tick**, **Untick**, **Press a key**, **Point at** | Thao tác trên trang |
| **Open a page** | Chuyển sang trang khác trong cùng site |
| **Wait (ms)** | Chỉ dùng khi không có dấu hiệu nào khác cho biết trang đã sẵn sàng |
| **Check: is shown / is gone / text is there / address contains / field holds / title contains** | Kiểm tra kết quả |

Cách hệ thống tìm phần tử:
- **Role and name**: nên dùng nhất. Ví dụ role `button` + name `Gửi`, hoặc role `textbox` + name `Tin nhắn`.
- **Field label**, **Placeholder**, **Visible text**, **Test id**, **CSS selector**: dùng khi role và name không hợp.

Một số lưu ý khi viết bước:
- Gõ `{{tên_input}}` ở ô giá trị để dùng input.
- Mỗi case nên kết thúc bằng ít nhất một bước **Check**.
- Khi lưu, các bước bằng chữ của test case được viết lại theo các bước này, để người test tay cũng đọc được.

### 6.4 Chạy và đọc kết quả

**Chạy:**
- Tích các case muốn chạy rồi bấm **Run N selected**. Không tích case nào thì bấm **Run all**.
- Ô **AI repairs a step whose element moved**: khi một nút hay ô bị đổi tên hoặc đổi chỗ, AI tìm lại phần tử đó cho riêng bước ấy. Bước **Check** không bao giờ được sửa: kiểm tra fail là fail.

**Trang Auto run** hiện từng case và từng bước ✓/✗, kèm:
- **Lý do fail**: không tìm thấy phần tử, kiểm tra không đúng, hoặc trang không mở / định rời khỏi site;
- **Ảnh chụp** lúc kết thúc và tại chỗ fail;
- **Lỗi console** của trang.

Bước AI đã sửa có ghi chú riêng. Bấm **Keep the repair in the case** để cập nhật case theo bản sửa đó.

**Kết quả còn được ghi vào hai nơi** (có nút mở ngay trên trang run):
- **Test plan** "Auto test · …": mỗi case là một execution, kèm ảnh chụp.
- **Test runs**: dùng cho release gate và phát hiện test chập chờn (flaky), như một run CI.

### 6.5 Quét site

Vào tab **Site scan**, điền rồi bấm **Scan site**:
- **Start page**: mặc định là địa chỉ của environment;
- **Signed in as**: chỉ hiện tài khoản đã đăng nhập được;
- **Pages**: số trang tối đa, từ 10 đến 50.

Hệ thống kiểm tra trang đầu và mọi trang có link tới trong cùng site, xa nhất 3 lần bấm link. Mỗi trang được kiểm như một URL check. Link đăng xuất hoặc link xóa không bao giờ bị mở. Trang kết quả mặc định chỉ hiện các trang có vấn đề.

### 6.6 Chạy tự động sau mỗi lần deploy

Cách 1, chạy trên ATP:
1. Vào trang **CI tokens**, tạo token và tích **Also start auto runs**.
2. Vào tab **Run from CI** của Auto test, chép đoạn lệnh vào bước ngay sau deploy. Đoạn lệnh bắt đầu một run, chờ chạy xong, in kết quả và làm fail pipeline nếu có case fail.

Cách 2, chạy trong repo của bạn:
- Ở tab **Cases**, chọn case rồi bấm **Playwright file** để tải về một file test Playwright, đưa vào repo và chạy bằng CI của bạn.
- Tài khoản đăng nhập lấy từ biến môi trường `ATP_USER_<KEY>` / `ATP_PASS_<KEY>`. File tải về không bao giờ chứa mật khẩu.

---

## 7. Dành cho người viết automation (Playwright)

### 7.1 Kết nối pipeline

1. Vào **CI tokens** (maintainer trở lên). Nhập **Name** theo tên pipeline, ví dụ `github-actions main`, chọn **Expires in**, rồi bấm **Issue token**.
   - Tích **Also read release gates** nếu pipeline cần kiểm gate trước khi deploy.
   - **Copy token ngay**, vì nó chỉ hiện một lần.
2. Cài reporter từ chính server ATP, không cần source ATP. Trang CI tokens đã in sẵn lệnh `npm install …/downloads/…` và đoạn cấu hình `playwright.config.ts`.
3. Trong CI, đặt hai biến:
   - `ATP_URL`: địa chỉ server.
   - `ATP_TOKEN`: token vừa tạo. Đặt trong secret store của CI, không commit vào repo.
4. Tùy chọn: `ATP_ENVIRONMENT=staging` để kết quả rơi vào đúng environment.
5. Chạy test. Kết quả hiện trong **Test runs**.

Mỗi pipeline dùng một token riêng, để thu hồi một token (**Revoke**) mà không làm dừng các pipeline khác.

Có báo cáo JUnit XML (ví dụ từ Cypress, Jest, pytest)? Dùng **Import JUnit** trên trang Test runs để đưa vào.

### 7.2 Xem kết quả và triage

- **Test runs**: các lần chạy, mới nhất ở trên cùng. Mở một run để xem từng test. Mở một test fail để vào **Failure detail**:
  - Lỗi và stack trace của từng lần thử.
  - Ảnh chụp, video, trace (**Open in trace viewer**).
  - **Test history**: test này đã chạy ra sao qua các lần.
  - **Triage** với 3 lựa chọn:
    - **Mark flaky**: test chập chờn.
    - **Known issue**: lỗi đã biết.
    - **Resolve**: đã xử lý.

    Mọi quyết định triage đều được ghi lại kèm tên người làm.
- **Test intelligence**:
  - **All tests**: mỗi test một dòng, giữ nguyên dù test bị đổi tên.
  - **Flaky view**: các test chập chờn.
  - **Review queue**: các cặp test có thể là cùng một test bị đổi tên, chờ người xác nhận.
- **Overview**: sức khỏe triage, gồm thời gian tìm ra nguyên nhân, độ phủ và nhóm lỗi.
- **ROI**: số giờ tiết kiệm được, đo từ dữ liệu triage thật so với baseline nhóm tự đo. Không có số nào là ước đoán.

---

## 8. Tài khoản của bạn

Bấm biểu tượng khiên ở góc dưới thanh bên trái để mở **Security**. Ở đó bạn có thể:
- Bật hoặc tắt xác thực hai bước.
- Xem còn bao nhiêu recovery code.
- Đổi mật khẩu.
- Liên kết tài khoản Google hoặc GitHub.

Nút bên cạnh biểu tượng khiên dùng để đăng xuất (**Sign out**).

---

## 9. Gặp lỗi thì làm gì

| Bạn thấy | Nghĩa là / cách xử lý |
|---|---|
| *"You do not have permission to access this resource"* | Vai trò của bạn không đủ cho thao tác này. Nhờ admin nâng vai trò ở **Members** ([mục 1.3](#13-vai-trò-ai-làm-được-gì)). |
| Đăng nhập xong chỉ vào được trang **Security** | Bạn là org owner nhưng chưa bật xác thực hai bước ([mục 1.1](#11-đăng-nhập)). |
| Không thấy **Start testing** | Bạn đang là viewer. Cần vai trò member trở lên. |
| *"This plan has no site address"* trên màn hình chạy | Plan chưa gắn environment có địa chỉ. Gắn ở trang plan (**Environment**, **Add address**). |
| *"AI drafting is not set up"* | Admin cần cấu hình AI ([mục 2.3](#23-bật-ai-soạn-test-case-không-bắt-buộc)). |
| AI báo *"The site could not be read…"* | Site không mở được từ server, hoặc URL check đang tắt. AI vẫn soạn case, nhưng chỉ dựa trên mô tả của bạn. |
| AI báo key bị từ chối, hoặc model không dùng được | Key sai hoặc đã bị thu hồi, key của provider khác với provider đang chọn, hoặc tài khoản không có quyền dùng model đó. Lưu lại key, chọn đúng provider, hoặc chọn model khác trong **AI settings**. |
| AI báo tài khoản hết tiền (*out of credit*) | Nạp thêm tiền ở trang billing của provider, hoặc lưu key khác. Thử lại không giúp gì. |
| Báo quá nhiều yêu cầu AI hoặc URL check | Đang có quá nhiều yêu cầu chờ, hoặc project đã dùng hết lượt soạn AI của ngày. Đợi yêu cầu trước xong rồi thử lại. |
| Nút **passed** bị mờ | Còn bước nào đang failed hoặc blocked. |
| *"This plan is closed"* | Plan đã đóng. Member trở lên bấm **Reopen** trên trang plan. |
| *"Case changed. Reload the case…"* | Có người vừa sửa case. Bấm **Reload case** rồi ghi lại kết quả. |
| *"Auto test is off on this server"* | Người vận hành cần bật `URLCHECK=on` rồi chạy `bash server/atp.sh up`. |
| Auto run báo *"could not sign in as …"* | Mở **Test accounts**: kiểm lại mật khẩu, bấm **Find sign-in steps**, hoặc sửa **Steps**. |
| Bước fail: *"The element was not found or could not be used"* | Giao diện đã đổi. Bật **AI repairs…** rồi chạy lại, hoặc **Edit** case và sửa tên phần tử. |
| Bước fail: *"tried to leave the site under test"* | Bước đó bấm vào link sang site khác. Hệ thống luôn giữ trình duyệt trong site đang test. |
| Case lỗi: *"The start page did not open"* | Kiểm tra lại ô **Where it starts**, các input của case và địa chỉ của environment. |

---

## 10. Phụ lục: lệnh cho người vận hành server

Chạy từ thư mục gốc repo, trên máy làm server. Máy cần Docker; nếu là Windows thì dùng Docker Desktop + Git Bash. Chi tiết xem [`server/README.md`](server/README.md).

| Lệnh | Làm gì |
|---|---|
| `bash server/atp.sh init <tên-miền-hoặc-IP>` | Lần đầu: tạo cấu hình, sinh secret |
| `bash server/atp.sh doctor` | Kiểm tra điều kiện trước khi chạy (chỉ đọc) |
| `bash server/atp.sh up` | Build và chạy toàn bộ hệ thống (dashboard, api, worker, database, HTTPS), đóng gói reporter |
| `bash server/atp.sh seed` | Một lần: tạo organization, workspace, project và tài khoản owner đầu tiên, cùng một CI token |
| `bash server/atp.sh status` / `logs [service]` | Xem tình trạng và log |
| `bash server/atp.sh restart` | Khởi động lại, ví dụ sau khi sửa `.env` |
| `bash server/atp.sh ca` | Xuất chứng chỉ để cài lên máy khác trong mạng |
| `bash server/atp.sh backup` / `backups` | Sao lưu database ngay / xem danh sách bản sao lưu |
| `bash server/atp.sh admin …` | Lệnh quản trị: tạo user, workspace, project, trao role, cấp token… |

Các tùy chọn trong `backend/deploy/.env`:
- `URLCHECK=on`: bật URL check, quét site và Auto test. Thêm một service Chromium, image khoảng 1 GB.
- `CHECKER_SESSIONS=4`: số trình duyệt Auto test mở cùng lúc, mỗi cái khoảng 150 MB.
- `URLCHECK_ALLOW_PRIVATE=on`: cho URL check mở địa chỉ trong mạng LAN.
- `LLM_API_KEY=…`: key của server cho phần AI, thuộc provider `LLM_PROVIDER`. Provider mặc định là `anthropic`; có thể đặt `openai`, `gemini`, `xai`, `mistral`, `deepseek`, `groq`, `openrouter`, hoặc `openai-compatible` cho endpoint riêng tại `LLM_BASE_URL` (khi đó bắt buộc có `LLM_MODEL`). Organization vẫn chọn provider, model và key riêng trong **AI settings**. `LLM_MODEL` chỉ là model key của server dùng khi organization chưa chọn gì.
- `MAIL=smtp`, kèm `SMTP_HOST`, `SMTP_FROM`, `SMTP_USERNAME`, `SMTP_PASSWORD` và `PUBLIC_URL`: gửi email mời, quên mật khẩu và link đăng ký. Dùng được SES, Resend, Mailgun, Postmark, Gmail (mật khẩu ứng dụng) và Microsoft 365. Với Microsoft 365, tenant phải còn cho đăng nhập SMTP bằng mật khẩu; Microsoft đang tắt dần cách này, nên về lâu dài nên dùng một dịch vụ gửi mail.
- `SIGNUP=on`: mở tự đăng ký có duyệt (mục 1.4). Cần `MAIL=smtp`; thiếu thì server không khởi động.

Sau khi sửa `.env`, chạy lại `bash server/atp.sh up` (với `URLCHECK`) hoặc `bash server/atp.sh restart` (với `LLM_API_KEY`, `LLM_PROVIDER`, `LLM_MODEL`, `MAIL`, `SIGNUP`).

> **Không bao giờ chạy `bash server/atp.sh reset` trên server có dữ liệu thật.** Lệnh này xóa toàn bộ dữ liệu, kể cả bản sao lưu.
