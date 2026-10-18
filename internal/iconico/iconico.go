// Package iconico writes Windows .ico files with PNG-compressed images at the
// sizes Windows asks for (title bar, file list, taskbar, Alt+Tab). The desktop
// build feeds the result to go-winres, which puts it in the executable as
// resource `#1`.
//
// PNG entries are the Vista and later form; the sizes below 256 are still
// encoded as PNG here because LoadIcon and the shell's icon loader accept them
// (checked against this app's own resource).
package iconico

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
)

// MaxSize is the largest image an .ico can name: the width and height fields
// are one byte each, where zero means 256.
const MaxSize = 256

// Build writes an .ico holding every image, in the order given. Each image must
// be a square of at most 256x256 pixels; the callers (and Windows) expect the
// smallest first.
func Build(w io.Writer, imgs []image.Image) error {
	if len(imgs) == 0 {
		return errors.New("iconico: no images")
	}
	type entry struct {
		side int
		png  []byte
	}
	entries := make([]entry, 0, len(imgs))
	for _, img := range imgs {
		b := img.Bounds()
		if b.Dx() != b.Dy() || b.Dx() == 0 || b.Dx() > MaxSize {
			return fmt.Errorf("iconico: %dx%d is not a square icon of 1..%d pixels", b.Dx(), b.Dy(), MaxSize)
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return fmt.Errorf("iconico: encode %dx%d: %w", b.Dx(), b.Dy(), err)
		}
		entries = append(entries, entry{side: b.Dx(), png: buf.Bytes()})
	}
	// The directory is a 6 byte header plus one 16 byte entry per image; the
	// images themselves follow it.
	offset := 6 + 16*len(entries)

	var out bytes.Buffer
	header := []uint16{0, 1, uint16(len(entries))} // reserved, type=icon, count
	for _, v := range header {
		if err := binary.Write(&out, binary.LittleEndian, v); err != nil {
			return err
		}
	}
	for _, e := range entries {
		var w, h int
		if e.side >= MaxSize {
			w, h = 0, 0 // zero names 256
		} else {
			w, h = e.side, e.side
		}
		dir := []uint8{
			uint8(w), uint8(h),
			0, // colours in the palette: none (PNG)
			0, // reserved
		}
		if err := binary.Write(&out, binary.LittleEndian, dir); err != nil {
			return err
		}
		planes := []uint16{1, 32} // one plane, 32 bits per pixel
		for _, v := range planes {
			if err := binary.Write(&out, binary.LittleEndian, v); err != nil {
				return err
			}
		}
		if err := binary.Write(&out, binary.LittleEndian, uint32(len(e.png))); err != nil {
			return err
		}
		if err := binary.Write(&out, binary.LittleEndian, uint32(offset)); err != nil {
			return err
		}
		offset += len(e.png)
	}
	for _, e := range entries {
		if _, err := out.Write(e.png); err != nil {
			return err
		}
	}
	if _, err := w.Write(out.Bytes()); err != nil {
		return err
	}
	return nil
}

// Fit scales src to a side x side square with a box filter: every destination
// pixel is the coverage-weighted average of the source pixels it covers, on
// premultiplied values so transparent edges do not darken. Icons are always
// scaled down (the source is the 512 px artwork), where an area average keeps
// detail better than dropping pixels.
func Fit(src image.Image, side int) (*image.RGBA, error) {
	if side <= 0 || side > MaxSize {
		return nil, fmt.Errorf("iconico: side %d is outside 1..%d", side, MaxSize)
	}
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw == 0 || sh == 0 {
		return nil, errors.New("iconico: empty source image")
	}
	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	xscale, yscale := float64(sw)/float64(side), float64(sh)/float64(side)
	for y := range side {
		y0, y1 := float64(y)*yscale, float64(y+1)*yscale
		for x := range side {
			x0, x1 := float64(x)*xscale, float64(x+1)*xscale
			var r, g, bl, a, weight float64
			for sy := int(math.Floor(y0)); sy < int(math.Ceil(y1)); sy++ {
				if sy < 0 || sy >= sh {
					continue
				}
				wy := math.Min(y1, float64(sy+1)) - math.Max(y0, float64(sy))
				if wy <= 0 {
					continue
				}
				for sx := int(math.Floor(x0)); sx < int(math.Ceil(x1)); sx++ {
					if sx < 0 || sx >= sw {
						continue
					}
					wx := math.Min(x1, float64(sx+1)) - math.Max(x0, float64(sx))
					if wx <= 0 {
						continue
					}
					w := wx * wy
					pr, pg, pb, pa := src.At(b.Min.X+sx, b.Min.Y+sy).RGBA()
					r += float64(pr) * w
					g += float64(pg) * w
					bl += float64(pb) * w
					a += float64(pa) * w
					weight += w
				}
			}
			if weight == 0 {
				continue
			}
			dst.Set(x, y, color.RGBA64{
				R: uint16(r / weight),
				G: uint16(g / weight),
				B: uint16(bl / weight),
				A: uint16(a / weight),
			})
		}
	}
	return dst, nil
}

// Sizes parses a comma-separated list of icon sizes, smallest first, rejecting
// anything an .ico cannot hold.
func Sizes(list string) ([]int, error) {
	var out []int
	for _, field := range bytes.Split([]byte(list), []byte(",")) {
		text := string(bytes.TrimSpace(field))
		if text == "" {
			continue
		}
		var side int
		if _, err := fmt.Sscanf(text, "%d", &side); err != nil {
			return nil, fmt.Errorf("iconico: %q is not a size", text)
		}
		if side <= 0 || side > MaxSize {
			return nil, fmt.Errorf("iconico: size %d is outside 1..%d", side, MaxSize)
		}
		out = append(out, side)
	}
	if len(out) == 0 {
		return nil, errors.New("iconico: no sizes given")
	}
	return out, nil
}
