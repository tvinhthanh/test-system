# So sánh ATP với các hệ thống test khác

> Bản đánh giá trung thực, cập nhật 2026-10-26 sau một vòng review bên ngoài.
> Cơ sở: docs/registry của từng đối thủ, đọc source nơi có. ATP mới kiểm chứng
> trên corpus tổng hợp và một lần refactor mô phỏng trên backend thật
> (xem [demo-refactor-identity.md](demo-refactor-identity.md)); **chưa có dữ liệu refactor từ repo khách**.

## Sửa so với bản trước

Bản trước **thổi phồng tính độc quyền** của identity. Các điểm đã sửa:

- **Allure TestOps:** mặc định `testCaseId = md5(fullName, params)` nên đổi tên sẽ tách lịch sử, **nhưng** gắn `Allure ID` ổn định trong code thì refactor không tạo case trùng.
- **ReportPortal:** không phải "khai tay" từng lần chạy. Agent gửi `testCaseId` ổn định qua integration (vd từ TMS); chỉ khi thiếu mới tự sinh từ code reference/tên + params.
- **Piwi:** không phải trả phí. Tự host miễn phí kể cả trong công ty; dashboard/server theo FSL-1.1-MIT, reporter/integrations MIT. Khóa lịch sử đọc từ source (`testId` của Playwright, hoặc title + location + suite), **chưa** chạy thử rename trên Piwi thật.
- **TMS:** Qase/Testmo/TestRail không chỉ quản lý thủ công — đều nhận kết quả automation và tích hợp CI. Không nên gộp thành một khối.

## Định vị nhanh

| Hệ thống | Loại | Self-host | Chi phí | Giữ lịch sử qua refactor |
|---|---|---|---|---|
| **ATP** | Reporter-first + Test Intelligence | Có | Chưa bán | **Tự động** (fuzzy + alias, có bước duyệt khi mơ hồ) |
| Currents.dev | Dashboard + orchestration | Không (SaaS) | Trả phí | Không — key theo spec file + project + title |
| ReportPortal | Reporting + AI triage | Có | Miễn phí (OSS) | Có **nếu** gửi `testCaseId` ổn định |
| Allure TestOps | Test management + report | Có | Trả phí | Có **nếu** gắn `Allure ID` ổn định |
| Piwi | Reporter + dashboard + AI điều tra | Có | Miễn phí (source-available) | Theo source: không (key theo `testId`/title+location) |
| Qase / Testmo / TestRail | TMS + nhận kết quả automation | Tùy | Trả phí | Qua case ID gắn với test |

## Mạnh / yếu / hạn chế

| Hệ thống | Điểm mạnh | Điểm yếu | Điểm hạn chế |
|---|---|---|---|
| **ATP** | Nối lịch sử qua đổi tên/chuyển file **không cần team gắn ID trong code**; có hàng duyệt + audit khi mơ hồ; reporter không làm fail test; self-host | Chưa có người dùng thật, chưa billing; UI/hệ sinh thái mỏng | Moat chưa đo trên repo khách; reporter native chủ yếu Playwright (khác qua JUnit); fuzzy có chi phí ở suite lớn (xem ghi chú đo) |
| **Currents.dev** | Orchestration CI mạnh; flaky detection; sản phẩm trưởng thành | Lịch sử tách khi đổi tên test/file | Chủ yếu Playwright/Cypress, SaaS |
| **ReportPortal** | Miễn phí, OSS; AI phân loại lỗi; đa framework | Vận hành self-host nặng; UI cũ | Giữ lịch sử phụ thuộc kỷ luật gửi ID ổn định |
| **Allure TestOps** | Báo cáo/trend tốt; TMS mạnh; tích hợp rộng | Tốn license | Giữ lịch sử phụ thuộc gắn `Allure ID` trong code |
| **Piwi** | Hiện đại; locator healing; AI agent điều tra; cài 1 lệnh; tự host miễn phí | Sản phẩm còn mới | Playwright-centric |
| **Qase / Testmo / TestRail** | Quản lý case/plan/run trưởng thành; tích hợp CI; khách doanh nghiệp | Phân tích flaky ở mức khác nhau, cần so từng sản phẩm | Không nhắm bài toán nối lịch sử tự động |

## Lợi thế thật, nói đúng mức

Không phải "chỉ ATP giữ được lịch sử". Đúng hơn là:

> **ATP giảm công sức giữ lịch sử test cho team chưa quản lý ID ổn định** — nối lại qua đổi tên/chuyển file mà không bắt team gắn annotation vào từng test.

- Team đã gắn ID ổn định (Allure ID, testCaseId) **ít cần** fuzzy matching.
- Team hay đổi cấu trúc test, không muốn gắn ID, và tốn thời gian điều tra lại lỗi cũ **cần ATP hơn**.
- Đây mới là **khác biệt kỹ thuật tiềm năng**. Thành lợi thế kinh doanh khi chứng minh được nó tiết kiệm thời gian trên repo thật.

## Rủi ro kỹ thuật cần soi trước khi coi identity là xong

- **Nối nhầm nguy hiểm hơn bỏ sót.** Thứ tự ưu tiên: ID ổn định nếu có → mapping đã xác nhận → fuzzy chỉ để **đề xuất**; ca mơ hồ giữ riêng.
- **Tên giống chưa chắc cùng hành vi.** Cần ca thử: test mới dùng lại tên cũ, copy test, đổi assertion, đổi parameter, tách một test thành nhiều. Split/merge nên giữ quan hệ nguồn gốc, không cộng dồn thống kê.
- **"Alias có thời hạn" có hai nghĩa.** Khoảng hiệu lực để xử lý tên bị dùng lại là hợp lý; mapping đã xác nhận vẫn phải lưu để giải thích lịch sử và xử lý kết quả đến muộn.
- **Reporter không làm fail test là chưa đủ.** Cần timeout + retry có giới hạn, báo rõ khi thiếu dữ liệu, và giữ phân biệt `passed` có retry/flaky.
- **Con số 8–30s ở 25k test** chưa nói lên gì khi chưa tách: thời gian reporter cộng vào CI, thời gian report sẵn sàng, thời gian identity hoàn tất (chạy nền). Khách đầu tiên ~100–1.000 test mới là mốc cần tối ưu.

## Thứ tự ưu tiên đề xuất

| Ưu tiên | Việc | Kết quả cần thấy |
|---|---|---|
| P0 — Dữ liệu đáng tin | Không đếm đôi shard/retry; run thiếu dữ liệu có trạng thái rõ; lỗi upload nhìn thấy | Người dùng tin số liệu |
| P0 — Giá trị lần đầu | Cài reporter → chạy CI → xem bằng chứng lỗi; nhập lịch sử cũ nếu được | Thử một project trong ~10 phút |
| P0 — Identity kiểm tra được | Hiện lý do đề xuất nối, chấp nhận/từ chối, hoàn tác, audit | Người dùng hiểu và sửa được quyết định |
| P1 — Triage hữu ích | Nhận lỗi đã gặp, gom failure giống nhau, trace/artifact đúng chỗ, giao người xử lý | Giảm thời gian điều tra |
| P1 — Theo dõi bản sửa | Đánh dấu fix, xem tái diễn, tách branch/browser | Trả lời "fix đã hiệu quả chưa?" |
| Sau pilot | Billing, mở rộng framework, TMS sâu hơn | Xây theo nhu cầu thật |

## Pilot ba team — cách đo

| Cần kiểm chứng | Cách đo |
|---|---|
| Onboarding nhẹ? | Thời gian từ bắt đầu cài tới report hữu ích đầu tiên |
| Identity đúng? | Precision/recall trên thay đổi được chủ repo xác nhận; tách số exact và fuzzy |
| Triage nhanh hơn? | Thời gian ra giả thuyết nguyên nhân: workflow cũ vs ATP |
| Có quay lại? | Dùng ATP cho failure tiếp theo mà không cần nhắc |
| Có nhu cầu thương mại? | Có team đồng ý pilot trả phí |

Đầu ra sprint kế: **một team tự kết nối project, một failure điều tra trọn quy trình, một lần refactor thật giữ được lịch sử có bằng chứng.**

## Nguồn

- Currents — [Test history](https://docs.currents.dev/dashboard/tests/test-history), [Test Signature API](https://docs.currents.dev/resources/api/api-resources/test-signature)
- Allure — [Test identifiers](https://allurereport.org/docs/how-it-works-test-identifiers/), [TestOps test results](https://docs.qameta.io/use-testops/results-and-analytics/test-results/)
- ReportPortal — [Test Case ID](https://reportportal.io/docs/work-with-reports/TestCaseId/)
- Piwi — [PiwiTests/platform LICENSE](https://github.com/PiwiTests/platform), source `@piwitests/reporter`
- Qase — [GitHub integration](https://docs.qase.io/en/articles/7210938-github)
