// Генератор иконок трея для internal/tray/assets.
// Логика модуля: рисует пиктограмму «домик» с бейджем статуса в четырёх
// вариантах (docs/spec.md, раздел 7.1) и сохраняет каждый как .ico
// (Windows: 16 и 32 px, 32-битный BGRA с альфой, без PNG-сжатия — максимально
// совместимый формат для LoadImage) и .png (macOS/прочие ОС, 32 px).
// Запуск из корня репозитория: go run ./scripts/genicons
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

// Палитра статусов.
var (
	colHouse     = color.NRGBA{240, 240, 240, 255} // «обычная» — светлый домик
	colHouseDim  = color.NRGBA{140, 140, 140, 255} // приглушённый — нет связи
	colConnected = color.NRGBA{76, 175, 80, 255}   // зелёный бейдж
	colPaused    = color.NRGBA{255, 170, 0, 255}   // янтарный бейдж паузы
	colError     = color.NRGBA{220, 60, 50, 255}   // красный бейдж ошибки
	colGlyph     = color.NRGBA{255, 255, 255, 255} // белые символы внутри бейджа
)

// variant описывает один вариант иконки.
type variant struct {
	name  string
	house color.NRGBA
	badge *color.NRGBA // nil — без бейджа
	glyph string       // "", "pause", "bang"
}

func main() {
	outDir := filepath.Join("internal", "tray", "assets")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fail(err)
	}

	variants := []variant{
		{name: "connected", house: colHouse, badge: &colConnected},
		{name: "disconnected", house: colHouseDim},
		{name: "paused", house: colHouse, badge: &colPaused, glyph: "pause"},
		{name: "error", house: colHouse, badge: &colError, glyph: "bang"},
	}

	for _, v := range variants {
		img16 := render(v, 16)
		img32 := render(v, 32)

		ico, err := encodeICO(img16, img32)
		if err != nil {
			fail(err)
		}
		if err := os.WriteFile(filepath.Join(outDir, v.name+".ico"), ico, 0o644); err != nil {
			fail(err)
		}

		var buf bytes.Buffer
		if err := png.Encode(&buf, img32); err != nil {
			fail(err)
		}
		if err := os.WriteFile(filepath.Join(outDir, v.name+".png"), buf.Bytes(), 0o644); err != nil {
			fail(err)
		}
		fmt.Printf("иконка %s: .ico (16+32) и .png (32)\n", v.name)
	}
}

// fail печатает ошибку и завершает генератор.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "ошибка генерации иконок:", err)
	os.Exit(1)
}

// render рисует вариант иконки на холсте size×size.
// Все координаты заданы в сетке 32×32 и масштабируются множителем f.
func render(v variant, size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	f := float64(size) / 32.0

	set := func(x, y int, c color.NRGBA) {
		if x >= 0 && x < size && y >= 0 && y < size {
			img.SetNRGBA(x, y, c)
		}
	}

	// Домик: крыша — треугольник с вершиной (16,3) и основанием y=14,
	// корпус — прямоугольник 7..25 × 14..28.
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			// Центр пикселя в координатах сетки 32×32.
			x := (float64(px) + 0.5) / f
			y := (float64(py) + 0.5) / f

			inBody := x >= 7 && x < 25 && y >= 14 && y < 28
			halfRoof := (y - 3) * 13.0 / 11.0
			inRoof := y >= 3 && y < 14 && x >= 16-halfRoof && x <= 16+halfRoof
			if inBody || inRoof {
				set(px, py, v.house)
			}
		}
	}

	if v.badge == nil {
		return img
	}

	// Бейдж статуса: круг в правом нижнем углу поверх домика.
	const bcx, bcy, br = 23.0, 23.0, 8.5
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			x := (float64(px)+0.5)/f - bcx
			y := (float64(py)+0.5)/f - bcy
			if x*x+y*y <= br*br {
				set(px, py, *v.badge)
			}
		}
	}

	// Символ внутри бейджа.
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			x := (float64(px) + 0.5) / f
			y := (float64(py) + 0.5) / f
			var on bool
			switch v.glyph {
			case "pause":
				// Две вертикальные полосы.
				on = y >= 19 && y < 28 &&
					((x >= 19.5 && x < 21.5) || (x >= 24.5 && x < 26.5))
			case "bang":
				// Восклицательный знак: палка и точка.
				on = (x >= 21.8 && x < 24.2 && y >= 18 && y < 24.5) ||
					(x >= 21.8 && x < 24.2 && y >= 26 && y < 28.5)
			}
			if on {
				set(px, py, colGlyph)
			}
		}
	}
	return img
}

// encodeICO упаковывает изображения в .ico: классический 32-битный BGRA
// с альфа-каналом и AND-маской, без PNG-сжатия — такой формат Windows
// LoadImage читает на любой версии.
func encodeICO(images ...*image.NRGBA) ([]byte, error) {
	var buf bytes.Buffer
	w := func(v any) { binary.Write(&buf, binary.LittleEndian, v) }

	// ICONDIR.
	w(uint16(0)) // reserved
	w(uint16(1)) // тип: иконка
	w(uint16(len(images)))

	// Данные каждой картинки готовим заранее, чтобы знать размеры и смещения.
	var blobs [][]byte
	for _, img := range images {
		blobs = append(blobs, encodeDIB(img))
	}

	offset := 6 + 16*len(images) // после ICONDIR и всех ICONDIRENTRY
	for i, img := range images {
		size := img.Bounds().Dx()
		b := byte(size)
		if size >= 256 {
			b = 0
		}
		w(b)          // ширина
		w(b)          // высота
		w(byte(0))    // палитра не используется
		w(byte(0))    // reserved
		w(uint16(1))  // planes
		w(uint16(32)) // бит на пиксель
		w(uint32(len(blobs[i])))
		w(uint32(offset))
		offset += len(blobs[i])
	}
	for _, blob := range blobs {
		buf.Write(blob)
	}
	return buf.Bytes(), nil
}

// encodeDIB кодирует картинку как DIB для .ico: BITMAPINFOHEADER
// (высота удвоена: XOR-битмап + AND-маска), строки снизу вверх, BGRA.
func encodeDIB(img *image.NRGBA) []byte {
	size := img.Bounds().Dx()
	var buf bytes.Buffer
	w := func(v any) { binary.Write(&buf, binary.LittleEndian, v) }

	// BITMAPINFOHEADER.
	w(uint32(40))       // размер заголовка
	w(int32(size))      // ширина
	w(int32(size * 2))  // высота: XOR + AND
	w(uint16(1))        // planes
	w(uint16(32))       // бит на пиксель
	w(uint32(0))        // без сжатия
	w(uint32(0))        // размер данных (можно 0 для несжатых)
	w(int32(0))         // ppm X
	w(int32(0))         // ppm Y
	w(uint32(0))        // цветов в палитре
	w(uint32(0))        // важных цветов

	// XOR-битмап: BGRA, строки снизу вверх.
	for y := size - 1; y >= 0; y-- {
		for x := 0; x < size; x++ {
			c := img.NRGBAAt(x, y)
			buf.WriteByte(c.B)
			buf.WriteByte(c.G)
			buf.WriteByte(c.R)
			buf.WriteByte(c.A)
		}
	}

	// AND-маска: 1 бит на пиксель, строки выровнены до 32 бит.
	// При 32-битном XOR с альфой маска нулевая (прозрачность задаёт альфа).
	rowBytes := ((size + 31) / 32) * 4
	zeroRow := make([]byte, rowBytes)
	for y := 0; y < size; y++ {
		buf.Write(zeroRow)
	}
	return buf.Bytes()
}
