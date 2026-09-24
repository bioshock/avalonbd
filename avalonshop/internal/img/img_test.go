package img

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
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

// craftOversizedPNG builds a syntactically valid PNG (correct signature, IHDR
// with a real CRC32, and a token IDAT/IEND) that declares width x height in
// its header without containing real pixel data for it. This is the same
// "decompression bomb" shape as a crafted huge-IHDR/tiny-IDAT file: decoders
// allocate width*height*bytesPerPixel straight from IHDR, before ever
// inflating IDAT, so the file itself stays a few dozen bytes regardless of
// the declared dimensions — no large fixture needs to be committed.
func craftOversizedPNG(t *testing.T, width, height uint32) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})

	writeChunk := func(typ string, data []byte) {
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
		buf.Write(lenBuf[:])
		typAndData := append([]byte(typ), data...)
		buf.Write(typAndData)
		var crcBuf [4]byte
		binary.BigEndian.PutUint32(crcBuf[:], crc32.ChecksumIEEE(typAndData))
		buf.Write(crcBuf[:])
	}

	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], width)
	binary.BigEndian.PutUint32(ihdr[4:], height)
	ihdr[8] = 8 // bit depth
	ihdr[9] = 6 // color type: truecolor with alpha
	// compression, filter, interlace all 0
	writeChunk("IHDR", ihdr)
	writeChunk("IDAT", []byte{0x00}) // never inflated: rejected on dimensions first
	writeChunk("IEND", nil)
	return buf.Bytes()
}

// TestProcessRejectsOversizedImage is the C2 fix: a header declaring
// 20000x20000 is over both the 10,000px-per-side and 30,000,000px-area caps,
// at a file size of a few dozen bytes — far under the 10 MB per-file upload
// cap, which only ever measured compressed bytes on the wire and never
// looked at declared dimensions. Deleting the dimension check in decode
// makes this test hang/OOM rather than simply fail, since Process would then
// attempt a real 1.6 GB decode of garbage IDAT data.
func TestProcessRejectsOversizedImage(t *testing.T) {
	dir := t.TempDir()
	huge := craftOversizedPNG(t, 20000, 20000)
	if _, err := Process(huge, dir); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("a rejected image must not write any files: %v", entries)
	}
}

// TestProcessAcceptsNormalSizedImage pins the other side of C2: an ordinary
// upload, comfortably under both new caps, must still be processed normally.
func TestProcessAcceptsNormalSizedImage(t *testing.T) {
	dir := t.TempDir()
	r, err := Process(pngBytes(t, gradient(1200, 900)), dir)
	if err != nil {
		t.Fatalf("normal image should be accepted: %v", err)
	}
	if r.Width != 1200 || r.Height != 900 {
		t.Fatalf("result %+v", r)
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
