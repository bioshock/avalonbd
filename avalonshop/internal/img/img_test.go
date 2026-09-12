package img

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/gen2brain/webp"
)

func gradient(w, h int) *image.NRGBA {
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			m.Set(x, y, color.NRGBA{uint8(x * 255 / w), uint8(y * 255 / h), 90, 255})
		}
	}
	return m
}

func pngBytes(t *testing.T, m image.Image) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, m); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func decodeWebP(t *testing.T, path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m, err := webp.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestProcessLarge(t *testing.T) {
	dir := t.TempDir()
	r, err := Process(pngBytes(t, gradient(2000, 1500)), dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Width != 1600 || r.Height != 1200 || len(r.Stem) != 16 {
		t.Fatalf("result %+v", r)
	}
	if len(r.Widths) != 3 || r.Widths[0] != 400 || r.Widths[2] != 1600 {
		t.Fatalf("widths %v", r.Widths)
	}
	for _, w := range r.Widths {
		m := decodeWebP(t, filepath.Join(dir, Filename(r.Stem, w)))
		if m.Bounds().Dx() != w || m.Bounds().Dy() != w*3/4 {
			t.Fatalf("variant %d has bounds %v", w, m.Bounds())
		}
	}
}

func TestProcessSmallNeverUpscales(t *testing.T) {
	dir := t.TempDir()
	r, err := Process(pngBytes(t, gradient(600, 600)), dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Width != 600 || r.Height != 600 || len(r.Widths) != 2 || r.Widths[0] != 400 || r.Widths[1] != 600 {
		t.Fatalf("result %+v", r)
	}
	if got := WidthsFor(600); len(got) != 2 || got[1] != 600 {
		t.Fatalf("WidthsFor(600) = %v", got)
	}
	if got := WidthsFor(1600); len(got) != 3 {
		t.Fatalf("WidthsFor(1600) = %v", got)
	}
	Remove(dir, r.Stem, r.Width)
	if _, err := os.Stat(filepath.Join(dir, Filename(r.Stem, 400))); !os.IsNotExist(err) {
		t.Fatal("Remove left files behind")
	}
}

func TestProcessRejectsNonImage(t *testing.T) {
	if _, err := Process([]byte("%PDF-1.4 not an image"), t.TempDir()); err != ErrUnsupported {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
}

// buildJPEGWithOrientation wraps a real JPEG with an APP1 EXIF segment carrying tag 0x0112.
func buildJPEGWithOrientation(t *testing.T, m image.Image, o uint16) []byte {
	var raw bytes.Buffer
	jpeg.Encode(&raw, m, nil)
	j := raw.Bytes()
	tiff := []byte("MM\x00\x2A\x00\x00\x00\x08") // big-endian, IFD0 at offset 8
	ifd := make([]byte, 2+12+4)
	binary.BigEndian.PutUint16(ifd[0:], 1)      // one entry
	binary.BigEndian.PutUint16(ifd[2:], 0x0112) // Orientation
	binary.BigEndian.PutUint16(ifd[4:], 3)      // SHORT
	binary.BigEndian.PutUint32(ifd[6:], 1)      // count
	binary.BigEndian.PutUint16(ifd[10:], o)     // value
	payload := append([]byte("Exif\x00\x00"), append(tiff, ifd...)...)
	seg := []byte{0xFF, 0xE1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}
	seg = append(seg, payload...)
	out := append([]byte{}, j[:2]...) // SOI
	out = append(out, seg...)
	return append(out, j[2:]...)
}

func TestOrientationParseAndApply(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	// mark top-left red, top-right blue
	src.Set(0, 0, color.NRGBA{255, 0, 0, 255})
	src.Set(3, 0, color.NRGBA{0, 0, 255, 255})
	data := buildJPEGWithOrientation(t, src, 6)
	if got := Orientation(data); got != 6 {
		t.Fatalf("Orientation = %d", got)
	}
	if got := Orientation(pngBytes(t, src)); got != 1 {
		t.Fatalf("PNG should report 1, got %d", got)
	}
	rot := applyOrientation(src, 6) // 90° clockwise: 4x2 → 2x4, top-left goes to top-right
	if rot.Bounds().Dx() != 2 || rot.Bounds().Dy() != 4 {
		t.Fatalf("bounds %v", rot.Bounds())
	}
	if r, _, _, _ := rot.At(1, 0).RGBA(); r>>8 != 255 {
		t.Fatalf("top-left red should now be at (1,0), got %v", rot.At(1, 0))
	}
	if _, _, b, _ := rot.At(1, 3).RGBA(); b>>8 != 255 {
		t.Fatalf("top-right blue should now be at (1,3), got %v", rot.At(1, 3))
	}
	// end-to-end: a rotated-tag JPEG comes out portrait
	dir := t.TempDir()
	r, err := Process(buildJPEGWithOrientation(t, gradient(800, 400), 6), dir)
	if err != nil || r.Width != 400 || r.Height != 800 {
		t.Fatalf("expected 400x800 after orientation, got %+v err=%v", r, err)
	}
}
