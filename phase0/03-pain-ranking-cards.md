# Thẻ pain ranking — câu B7

> In ra hoặc mở trên màn hình. Đưa **cả 5 thẻ cùng lúc**, nhờ xếp từ nặng nhất tới nhẹ nhất.
> Không gợi ý, không giải thích thêm. Im lặng chờ.

---

### (i)
## Không biết test fail vì sao
Mất thời gian điều tra mới biết là bug thật, test hỏng, hay lỗi môi trường

---

### (ii)
## Flaky làm mất niềm tin vào CI
Test lúc pass lúc fail, team quen tay rerun cho qua

---

### (iii)
## Quản lý manual + automation song song
Hai hệ thống, hai nguồn sự thật, khó biết cái gì đã được phủ

---

### (iv)
## Viết test mới tốn công
Mất nhiều thời gian để thêm test cho tính năng mới

---

### (v)
## Chạy test quá chậm
Bộ test mất quá lâu, làm chậm vòng phản hồi

---

## Cách đọc kết quả

| Kết quả | Nghĩa |
|---|---|
| (i) hoặc (ii) trong top-2 | **H1 dương** — đúng wedge hiện tại |
| (iii) hạng 1 | Tín hiệu **lật wedge** sang manual↔automation (QĐ-2 phương án B) |
| (iv) hoặc (v) hạng 1 | Sản phẩm này **không** giải pain #1 của họ — ghi nhận trung thực |

**Ngưỡng Gate-0 (§4.5 điều kiện #4):** (i)+(ii) trong top-2 của **≥ 8/15** người.
**Nhánh lật wedge:** (iii) hạng 1 của ≥ 8/15 → viết lại §2 và §4 trước khi build.
