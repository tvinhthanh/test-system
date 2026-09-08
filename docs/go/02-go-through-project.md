# 02 — Go căn bản qua project

[Mục lục](README.md) · [Tiếp: Đọc identity](03-identity-walkthrough.md)

## Module, package và điểm chạy

`go.mod` định danh một module: tập hợp package được quản lý chung phiên bản và dependency. Các file `package identity` trong thư mục prototype hợp thành một package. `cmd/protoident/main.go` dùng `package main` và `func main()` làm điểm chạy CLI.

Tên bắt đầu bằng chữ hoa được export: `Identity`, `Resolve`, `DefaultConfig`. Tên chữ thường như `candidateSet` chỉ dùng trong package. Go không dùng `public/private` như class TypeScript.

| Nếu quen NestJS / TypeScript | Trong Go |
|---|---|
| Class giữ dữ liệu và method | `struct` và method có receiver |
| Constructor / provider | Hàm `New...` và truyền dependency tường minh; không có DI container trong prototype |
| Nest module | Không tương đương trực tiếp Go module; ranh giới nghiệp vụ thường là package |
| `Promise` / `async` | Lời gọi bình thường trả kết quả trực tiếp; dùng goroutine khi cần concurrency |
| `throw` / `try-catch` | Thường trả `error` và xử lý ở caller |
| Interface TypeScript | Go interface mô tả method; type đáp ứng ngầm nếu có đủ method |

## Đọc một struct và method thật

Trong [`fingerprint.go`](../../prototype/identity/fingerprint.go):

```go
type Identity struct {
    Framework      string
    FilePath       string
    TitlePath      []string
    ParamSignature string
}
```

`[]string` là slice. `func (id Identity) Fingerprint(repoRoot string) string` là method của `Identity`, nhận repo root và trả fingerprint dạng chuỗi. `(id Identity)` là value receiver: nhận bản sao struct.

Ví dụ biểu thức dùng bên trong một hàm của package đã import identity:

```go
id := identity.Identity{
    Framework: "playwright",
    FilePath:  "tests/auth/login.spec.ts",
    TitlePath: []string{"Auth", "logs in"},
}
fp := id.Fingerprint("")
fmt.Println(fp)
```

`:=` khai báo và suy luận kiểu biến cục bộ. Đây là đoạn minh họa, không phải file chạy độc lập; xem [`example_test.go`](../../prototype/identity/example_test.go) để có ví dụ đầy đủ imports và output được kiểm tra.

## Value, pointer và dữ liệu dùng chung

`&Resolver{...}` tạo struct rồi lấy con trỏ. Receiver `*Resolver` nhận con trỏ, nhưng không có nghĩa mọi method đều sửa dữ liệu. `Resolve` hiện trả decision, không tự ghi alias vào database.

Sao chép struct chứa slice không sao chép toàn bộ phần tử slice:

```go
renamed := id
renamed.TitlePath = []string{"Auth", "signs in"}
```

Gán slice mới như trên tách title mới. Ngược lại, sửa `renamed.TitlePath[1]` ngay sau `renamed := id` có thể sửa cả dữ liệu mà `id.TitlePath` đang trỏ tới. Đọc `Normalize` để thấy cách tạo slice mới trước khi chuẩn hóa.

## Map và zero value

`AliasStore` là `map[string]string`: fingerprint → stable test ID. Cú pháp `id, ok := r.Aliases[fp]` phân biệt có key với không có key. Đọc nil map được; ghi nil map gây panic. Map thông thường không an toàn nếu có đọc/ghi đồng thời mà thiếu đồng bộ.

Biến chưa gán có zero value: số là `0`, bool là `false`, pointer/map/slice là `nil`. Zero value không mặc nhiên hợp lệ về nghiệp vụ: dùng `DefaultConfig()` rồi điều chỉnh. Dù resolver hiện có fallback khi threshold bằng 0, không nên dựa vào fallback để tạo config thiếu trường.

## Error và JSON

Trong [`playwright.go`](../../prototype/identity/playwright.go), loader kiểm tra lỗi ngay sau đọc file và parse JSON:

```go
raw, err := os.ReadFile(path)
if err != nil {
    return nil, fmt.Errorf("read report: %w", err)
}
```

`%w` giữ lỗi gốc trong chuỗi wrap để caller có thể phân loại. Không nuốt lỗi bằng `_`, không dùng panic cho report người dùng nhập sai. Struct tag như `` `json:"projectName"` `` ánh xạ tên trường JSON; decode thành công chưa đồng nghĩa dữ liệu hợp lệ về nghiệp vụ.

## Config, thời gian và concurrency

`WithRecencyGate` dùng value receiver và trả config mới. Cách truyền cấu hình này tránh test thay đổi global threshold của nhau. Tuy nhiên struct vẫn có trường export; đây là quy ước không mutate shared config, không phải tính bất biến được compiler bảo đảm.

`Resolve(in, now)` nhận thời gian từ caller, giúp test dùng mốc cố định. Khi xây I/O production, học thêm `context.Context` để truyền deadline/cancellation; không cần thêm context vào mọi hàm tính điểm thuần.

Goroutine cho phép công việc chạy đồng thời. Nó không tự tạo backpressure, transaction hay độ bền dữ liệu. [`concurrent.go`](../../prototype/identity/concurrent.go) dùng mutex bảo vệ get-or-create trong một process; nhiều backend instance vẫn cần ràng buộc uniqueness ở database.

Để học cú pháp có bài chạy tương tác, dùng [A Tour of Go](https://go.dev/tour/) song song với các file được dẫn ở trên.
