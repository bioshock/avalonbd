// Package img turns an uploaded image into oriented, metadata-free WebP variants.
package img

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gen2brain/webp"
	xdraw "golang.org/x/image/draw"
)

var ErrUnsupported = errors.New("Please upload JPEG, PNG, WebP or GIF")

var Widths = []int{400, 900, 1600}

const quality = 82

type Result struct {
	Stem   string
	Width  int
	Height int
	Widths []int
}

func Filename(stem string, w int) string { return fmt.Sprintf("%s-%d.webp", stem, w) }

// WidthsFor lists the variants that exist for an image whose largest variant is width.
func WidthsFor(width int) []int {
	var out []int
	for _, w := range Widths {
		if w < width {
			out = append(out, w)
		}
	}
	return append(out, width)
}

func Remove(dir, stem string, width int) {
	for _, w := range WidthsFor(width) {
		os.Remove(filepath.Join(dir, Filename(stem, w)))
	}
}

func decode(data []byte) (image.Image, error) {
	r := bytes.NewReader(data)
	switch http.DetectContentType(data) {
	case "image/jpeg":
		m, err := jpeg.Decode(r)
		if err != nil {
			return nil, err
		}
		return applyOrientation(m, Orientation(data)), nil
	case "image/png":
		return png.Decode(r)
	case "image/gif":
		return gif.Decode(r) // first frame
	case "image/webp":
		return webp.Decode(r, webp.Options{AutoRotate: true})
	}
	return nil, ErrUnsupported
}

// Process decodes data, writes every applicable WebP variant into dir, and
// reports the largest variant's dimensions. On any failure it removes what it wrote.
func Process(data []byte, dir string) (Result, error) {
	src, err := decode(data)
	if err != nil {
		if errors.Is(err, ErrUnsupported) {
			return Result{}, ErrUnsupported
		}
		return Result{}, fmt.Errorf("decode: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Result{}, err
	}
	var stemBytes [8]byte
	rand.Read(stemBytes[:])
	res := Result{Stem: hex.EncodeToString(stemBytes[:])}
	ow := src.Bounds().Dx()
	targets := WidthsFor(min(ow, Widths[len(Widths)-1]))
	for _, w := range targets {
		m := resize(src, w)
		f, err := os.Create(filepath.Join(dir, Filename(res.Stem, w)))
		if err == nil {
			err = webp.Encode(f, m, webp.Options{Quality: quality})
			err = errors.Join(err, f.Close())
		}
		if err != nil {
			Remove(dir, res.Stem, targets[len(targets)-1])
			return Result{}, err
		}
		res.Widths = append(res.Widths, w)
		res.Width, res.Height = m.Bounds().Dx(), m.Bounds().Dy()
	}
	return res, nil
}

func resize(src image.Image, w int) *image.NRGBA {
	b := src.Bounds()
	if b.Dx() == w {
		dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
		return dst
	}
	h := int(math.Round(float64(b.Dy()) * float64(w) / float64(b.Dx())))
	if h < 1 {
		h = 1
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Src, nil)
	return dst
}

// applyOrientation maps EXIF orientation values 2–8 onto pixel moves.
func applyOrientation(src image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	in := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(in, in.Bounds(), src, b.Min, draw.Src)
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	out := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			si, di := in.PixOffset(x, y), out.PixOffset(dx, dy)
			copy(out.Pix[di:di+4], in.Pix[si:si+4])
		}
	}
	return out
}
