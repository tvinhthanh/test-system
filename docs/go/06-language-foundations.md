# 06 — Nền tảng ngôn ngữ để viết code độc lập

[Mục lục](README.md) · Tiếp: [Concurrency](07-concurrency-and-lifecycle.md)

Mục tiêu: tự viết một hàm nghiệp vụ, biết ownership dữ liệu và giải thích được hành vi của code. Đọc chương 02 trước. Học trước trên fingerprint.go, config.go và example_test.go của prototype. Snippet dưới đây chỉ giải thích khái niệm, chưa biên dịch; không tạo package bài tập. Phần ingest chưa có chỉ triển khai khi đến feature tương ứng.

## Biến, kiểu và control flow

`var n int` tạo số nguyên có giá trị 0. `n := 3` suy luận kiểu trong hàm; dùng `=` khi gán lại. `const` dành cho giá trị hằng; cấu hình lấy từ môi trường là dữ liệu runtime, không phải constant. Chuyển `int64` sang `int` phải xem phạm vi giá trị trước, nhất là byte size và duration.

Go chỉ có `for` cho vòng lặp. `range` trên slice trả index và bản sao phần tử: thay field của bản sao struct không thay phần tử gốc. Muốn sửa, dùng index hoặc thiết kế hàm trả slice mới. `switch` không tự rơi xuống case sau. Early return giúp tách validation khỏi đường thành công.

```go
// Minh họa validation của ingest tương lai; cần import "fmt".
func ValidateBatchSize(count, limit int) error {
    if limit <= 0 {
        return fmt.Errorf("batch limit must be positive")
    }
    if count <= 0 || count > limit {
        return fmt.Errorf("batch size %d outside 1..%d", count, limit)
    }
    return nil
}
```

Giới hạn đến từ config/protocol, không chép con số rải rác vào handler. Ví dụ chỉ kiểm tra số lượng; giới hạn bytes là điều kiện riêng.

## Collection và chuỗi

| Kiểu | Đặc điểm | Lỗi dễ gặp trong project |
|---|---|---|
| `[3]string` | Độ dài thuộc kiểu, gán sẽ copy array | Nhầm array với slice |
| `[]string` | Slice tham chiếu backing array, có len/cap | Copy `Identity` rồi sửa chung `TitlePath` |
| `map[string]string` | Tra key; thứ tự duyệt không được bảo đảm | Hash param theo thứ tự map khiến fingerprint không ổn định |
| `string` | Chuỗi bytes bất biến | `len` đo bytes, không đo ký tự người dùng nhìn thấy |

`range` trên string giải mã rune; rune cũng chưa tương đương một ký tự hiển thị vì dấu tổ hợp. Với title tiếng Việt, phải xác định thuật toán đo bytes hay rune. Không thay normalization của fingerprint âm thầm: thay đổi đó có thể đổi identity và cần version/migration.

`append` có thể dùng lại backing array hoặc cấp array mới. Khi API yêu cầu không làm thay đổi input, clone slice trước khi sửa phần tử; shallow clone chưa đủ với phần tử chứa map/slice/con trỏ. Đọc [`Normalize`](../../prototype/identity/fingerprint.go) như một ví dụ ownership rõ.

## Method set và interface

Interface mô tả năng lực caller cần. Ví dụ dưới đây là minh họa ranh giới đọc alias của identity tương lai, không phải API có sẵn:

```go
type AliasReader interface {
    Lookup(ctx context.Context, projectID, fingerprint string) (string, error)
}
```

Adapter có method đúng signature đáp ứng interface mà không cần `implements`. Nếu `Lookup` có receiver `*Store`, thường truyền `*Store`, không truyền giá trị `Store`. Có thể kiểm tra ở compile time bằng `var _ AliasReader = (*Store)(nil)` sau khi đã định nghĩa Store.

Embedding tái sử dụng field/method, không phải hệ thống kế thừa class. Ưu tiên composition có dependency được đặt tên rõ khi quyền và transaction cần dễ theo dõi. Generic repository CRUD cho mọi entity thường làm mất quy tắc riêng như `FinishRun` hoặc `ResolvePending`.

## Bẫy nil interface

```go
type LookupError struct{}
func (*LookupError) Error() string { return "lookup failed" }

func badResult() error {
    var err *LookupError
    return err
}
```

`badResult() != nil`: interface chứa kiểu động `*LookupError` dù giá trị pointer là nil. Thành công phải trả `nil` trực tiếp. Bẫy này dễ xuất hiện khi constructor trả interface hoặc adapter chuyển concrete error thành `error`.

## Error, defer và tài nguyên

Dùng `errors.Is` khi phân loại sentinel error; dùng `errors.As` khi cần dữ liệu của typed error. Wrap bằng `%w` ở ranh giới có thêm ngữ cảnh, nhưng response cho client phải dùng mã lỗi ổn định và không lộ lỗi SQL/token.

`defer` chạy khi hàm hiện tại trả về, theo thứ tự ngược đăng ký; đối số được đánh giá lúc đăng ký. Trong vòng lặp mở nhiều file, `defer Close` ở hàm bao ngoài giữ file đến hết hàm: tách xử lý mỗi file thành hàm nhỏ. Với writer, lỗi `Close` có thể quan trọng; đừng mặc định mọi close đều bỏ qua được.

Panic dành cho điều kiện bất thường của chương trình, không phải validation payload. `recover` không thay thế error handling, chỉ bắt panic ở cùng goroutine khi được dùng đúng trong deferred function. Ghi nhận lỗi và giữ khả năng điều tra, không trả thành công sau panic.

## Generics và chuẩn thư viện

Ví dụ generic nhỏ, có lý do rõ: giữ kiểu phần tử trong kết quả phân trang của query khi có yêu cầu thực tế; chưa có type này trong repo.

```go
type Page[T any] struct {
    Items      []T
    NextCursor string
}
```

Type parameter có ích khi thuật toán/cấu trúc thực sự dùng chung nhiều kiểu. Interface thích hợp cho hành vi như đọc alias. Không dùng reflection hoặc `any` để bỏ qua type checking chỉ vì không muốn định nghĩa DTO.

Trước khi tự viết helper collection, kiểm tra thư viện chuẩn của toolchain đang dùng. Đọc [tutorial generics](https://go.dev/doc/tutorial/generics). [Effective Go](https://go.dev/doc/effective_go) hữu ích về idiom, nhưng là tài liệu cũ và không bao phủ đầy đủ modules/generics. Quy tắc ngôn ngữ tra ở [Go specification](https://go.dev/ref/spec); website có thể mô tả phiên bản mới hơn `go 1.25.5` của prototype.

## Bài tập và tự kiểm tra

1. Đọc config.go và gate_test.go để hiểu giá trị cấu hình, các boundary và config copy. Khi triển khai ingest thật, chuyển hiểu biết đó thành validation test theo cap SPEC: count 0, 1, cap, cap+1; config cap không dương là lỗi. Không thêm ValidateBatchSize vào prototype chỉ để làm bài.
2. Giải thích vì sao duyệt map rồi hash trực tiếp không deterministic. **Đáp án:** phải chuẩn hóa thứ tự key và encoding, như `NormalizeParams`; sorting không tự giải quyết mọi vấn đề phân cách giá trị.
3. Viết test caller nhận nil khi lookup không lỗi. **Đáp án:** tránh trả typed nil qua interface, assertion phải kiểm tra giá trị error thực sự.
4. Clone Identity rồi sửa title mà không thay input. **Đáp án:** copy backing data của `TitlePath`, không chỉ copy struct.

Đạt chương này khi dự đoán được output trước khi chạy và giải thích được ai sở hữu dữ liệu sau mỗi phép gán.
