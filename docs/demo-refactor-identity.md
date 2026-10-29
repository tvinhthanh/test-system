# Kịch bản demo: "Lịch sử test sống sót qua refactor"

> Mục đích: đưa cho 3–5 team QA xem để **kiểm chứng nhu cầu**, không phải để khoe tính năng.
> Câu hỏi cần trả lời sau demo: *họ có đủ đau để trả tiền cho điều này không.*

## Thông điệp một câu

> "Bạn đổi tên hay di chuyển một test — mọi công cụ khác coi nó là test mới và **mất sạch lịch sử flaky**. ATP nhận ra vẫn là test đó và **giữ nguyên lịch sử**."

## Vì sao điều này quan trọng (nỗi đau)

Team E2E lớn refactor liên tục: đổi tên test cho rõ nghĩa, tách/gộp file, đổi cấu trúc `describe`. Mỗi lần như vậy, công cụ reporting coi đó là một test mới:

- Lịch sử flaky về 0 — không còn biết test này vốn hay flaky hay không.
- Mọi phân tích "test này fail bao nhiêu lần trong 500 lần chạy" bị reset.
- Thống kê độ ổn định của cả suite bị nhiễu bởi các "test mới" thực ra là test cũ đổi tên.

Nói cách khác: **càng chăm refactor, dữ liệu càng vô dụng.**

## Đối thủ gắn lịch sử theo gì (bằng chứng để nói khi bị hỏi)

| Công cụ | Khóa định danh | Đổi tên / chuyển file |
|---|---|---|
| Currents.dev | file path + test title | Vỡ |
| Allure TestOps | `md5(fullName, params)` | Vỡ (tạo test case mới, bỏ cái cũ) |
| ReportPortal | code ref / full path (hoặc ID khai tay) | Vỡ, trừ khi khai tay |
| Piwi (`@piwitests/reporter`) | Playwright `testId` / (title + location + suite) | Vỡ |
| **ATP** | **so khớp mờ + alias có thời hạn** | **Nối lại được** |

Điểm chốt: không công cụ nào **tự động** giữ lịch sử qua refactor. ReportPortal cho làm thủ công; phần còn lại đơn giản là đứt.

## Chuẩn bị trước khi demo (một lần)

1. Stack ATP đang chạy, đăng nhập được dashboard.
2. Một CI token của project demo (dashboard → project → CI tokens).
3. Project Playwright ví dụ có sẵn trong repo: `reporter/playwright/example/`.
4. Biến môi trường trỏ reporter về server (thay giá trị thật của bạn):

   ```bash
   export ATP_URL="https://<host>:8443"
   export ATP_TOKEN="<ci-token>"
   export NODE_EXTRA_CA_CERTS="<đường dẫn atp-root-ca.crt>"   # nếu dùng cert nội bộ
   ```

## Kịch bản 6 bước (bấm gì + nói gì)

### Bước 1 — Chạy suite gốc, tạo lịch sử

```bash
cd reporter/playwright
npx playwright test --config example/playwright.config.ts example/tests/checkout.spec.ts
```

**Nói:** "Đây là 3 test: một pass, một flaky, một fail. ATP vừa ghi nhận chúng."

Trên dashboard → **Test runs**: chỉ vào run vừa xuất hiện và trạng thái pass/flaky/fail.
Trên **Test intelligence** (hoặc Identities): chỉ vào test `accepts valid card` ở `checkout.spec.ts`.

### Bước 2 — Refactor như đời thật: đổi tên + chuyển file

Tạo bản refactor của test `accepts valid card`:
- **Đổi tên** thành `accepts a valid Visa card at checkout`.
- **Chuyển** từ `checkout.spec.ts` sang `billing.spec.ts`.
- Hai test còn lại giữ nguyên.

**Nói:** "Đúng kiểu thay đổi diễn ra mỗi sprint — đổi tên cho rõ, tách file cho gọn. Với Playwright, `testId` của test này giờ hoàn toàn khác."

### Bước 3 — Chạy lại suite đã refactor

```bash
npx playwright test --config example/refactor/playwright.config.ts
```

### Bước 4 — Cho thấy ATP KHÔNG mù quáng tạo test mới

Trên dashboard → **Review queue** (hàng chờ duyệt identity): xuất hiện một đề xuất.

**Nói:** "ATP không im lặng tạo test mới. Nó phát hiện test đã-đổi-tên-và-chuyển-file này **rất có thể** là `accepts valid card` cũ, và hỏi tôi xác nhận — kèm điểm tương đồng. Nó thận trọng để không bao giờ gộp nhầm."

Điểm thật đo được trên hệ thống: `score 0.6532` (title giống 0.34, file giống 0.6), đủ ngưỡng để **gợi ý**, chưa đủ để tự gộp vì thay đổi quá mạnh và mới có một run lịch sử.

### Bước 5 — Duyệt một lần

Bấm **Merge** (hoặc `admin review decide -decision merged`).

**Nói:** "Một cú bấm. Giờ ATP tạo một *alias* — nó ghi nhớ vĩnh viễn rằng test mới này chính là test cũ."

Kết quả: identity tạm biến mất, nhập vào identity gốc `accepts valid card`.

### Bước 6 — Khoảnh khắc "wow": chạy lại lần nữa

```bash
npx playwright test --config example/refactor/playwright.config.ts
```

Trên dashboard: **vẫn đúng 3 identity, không có đề xuất review mới**, và lịch sử của `accepts valid card` tăng thêm một run.

**Nói:** "Từ giờ, test đã đổi tên và đổi file này **tự động** gắn vào lịch sử cũ. Không hỏi lại, không tạo mới. Lịch sử flaky của nó liền mạch qua lần refactor — điều mà Currents, Allure, ReportPortal, Piwi đều không làm được."

## So sánh cạnh nhau (một slide)

| | Sau khi đổi tên + chuyển file |
|---|---|
| Currents / Allure / ReportPortal / Piwi | Test mới. Lịch sử flaky = 0. Thống kê suite bị nhiễu. |
| **ATP** | Nhận ra test cũ, đề xuất gộp, duyệt một lần → lịch sử liền mạch mãi về sau. |

## Câu hỏi kiểm chứng nhu cầu (hỏi sau demo)

Mục tiêu là nghe sự thật, không phải nghe lời khen lịch sự:

1. "Team mình đổi tên hoặc chuyển test bao lâu một lần?" (đo tần suất nỗi đau)
2. "Khi đó, lịch sử flaky trong công cụ hiện tại có mất không? Điều đó có gây khó cho mình không?"
3. "Mình đang trả bao nhiêu cho công cụ reporting hiện tại?" (đo mức sẵn sàng chi)
4. "Nếu có đúng tính năng này, mình sẽ đổi sang, hay thấy *có thì tốt* nhưng không đổi?"
5. "Ai trong team sẽ là người quyết định chi tiền cho thứ này?"

**Tín hiệu đáng đầu tư tiếp:** họ tự kể ra một lần refactor làm hỏng dữ liệu, và hỏi giá.
**Tín hiệu nên dừng:** "hay đấy" nhưng không nhớ nổi lần nào nó thực sự gây đau.

## Phụ lục: lệnh tái lập bộ refactor

Bộ test refactor dùng ở bước 2 (tạo dưới `reporter/playwright/example/refactor/`):

`refactor/playwright.config.ts` — trỏ reporter về `../../dist/src/index.js`, `testDir: ./tests`.

`refactor/tests/checkout.spec.ts` — giữ 2 test không đổi (`declines expired card (flaky on purpose)`, `shows error on empty field`).

`refactor/tests/billing.spec.ts` — test đã đổi tên + chuyển file:

```ts
import { test, expect } from "@playwright/test";

test.describe("Billing", () => {
  test("accepts a valid Visa card at checkout", async ({}, testInfo) => {
    await testInfo.attach("receipt.png", { body: Buffer.from("\x89PNG-not-really-a-png"), contentType: "image/png" });
    expect(1 + 1).toBe(2);
  });
});
```

Kiểm tra trạng thái identity và review queue bằng operator CLI:

```bash
./server/atp.sh admin dash identities -project <project-id>
./server/atp.sh admin review list -project <project-id>
./server/atp.sh admin review decide -id <item-id> -decision merged -actor <email>
```

> Cơ sở kỹ thuật của moat: SPEC §9.1–§9.6 (test identity, so khớp mờ, alias `validFrom/validTo`) và phát hiện N37 (lý do từ chối `spec.id` của Playwright vì nó vỡ khi đổi tên/chuyển file).
