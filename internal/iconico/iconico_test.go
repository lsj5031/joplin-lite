package iconico

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// solid returns a side x side image of one colour.
func solid(side int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	for y := range side {
		for x := range side {
			img.Set(x, y, c)
		}
	}
	return img
}

// TestFitAverages covers the box filter: a 2x2 block of four colours becomes
// one pixel of their average, and alpha averages with them (premultiplied, so
// a fully transparent pixel does not pull the colour towards black).
func TestFitAverages(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			c := color.RGBA{R: 255, A: 255} // red
			if (x/2+y/2)%2 == 1 {
				c = color.RGBA{B: 255, G: 255, A: 255} // yellow-ish: red+green would be white, keep B
			}
			src.Set(x, y, c)
		}
	}
	got, err := Fit(src, 2)
	if err != nil {
		t.Fatalf("Fit: %v", err)
	}
	if b := got.Bounds(); b.Dx() != 2 || b.Dy() != 2 {
		t.Fatalf("bounds = %v, want 2x2", b)
	}
	if r, _, _, a := got.At(0, 0).RGBA(); r == 0 || a == 0 {
		t.Fatalf("top-left = %v, want a blend of the source block", got.At(0, 0))
	}

	// A half-transparent pixel halves the average alpha; the colour stays.
	mixed := image.NewRGBA(image.Rect(0, 0, 2, 1))
	mixed.Set(0, 0, color.RGBA{R: 200, A: 255})
	mixed.Set(1, 0, color.RGBA{R: 200, A: 0})
	half, err := Fit(mixed, 1)
	if err != nil {
		t.Fatalf("Fit: %v", err)
	}
	r, _, _, a := half.At(0, 0).RGBA()
	if a < 0x7000 || a > 0x9000 {
		t.Fatalf("alpha = %d, want about half of 65535", a)
	}
	if r < 0x6000 {
		t.Fatalf("red = %d, want the colour not darkened by transparency", r)
	}
}

// TestBuildWritesDecodableEntries parses the file back: the directory must list
// each size with 256 written as zero, and every payload must be a PNG of that
// size that decodes.
func TestBuildWritesDecodableEntries(t *testing.T) {
	sizes := []int{16, 32, 48, 256}
	imgs := make([]image.Image, 0, len(sizes))
	for _, s := range sizes {
		img, err := Fit(solid(512, color.RGBA{R: 10, G: 200, B: 90, A: 255}), s)
		if err != nil {
			t.Fatalf("Fit(%d): %v", s, err)
		}
		imgs = append(imgs, img)
	}
	var buf bytes.Buffer
	if err := Build(&buf, imgs); err != nil {
		t.Fatalf("Build: %v", err)
	}
	data := buf.Bytes()

	reserved := binary.LittleEndian.Uint16(data[0:2])
	kind := binary.LittleEndian.Uint16(data[2:4])
	count := binary.LittleEndian.Uint16(data[4:6])
	if reserved != 0 || kind != 1 || count != 4 {
		t.Fatalf("header = %d/%d/%d, want 0/1/4", reserved, kind, count)
	}
	for i, want := range sizes {
		e := data[6+16*i : 6+16*(i+1)]
		named := int(e[0])
		if want == MaxSize {
			if named != 0 {
				t.Fatalf("entry %d names %d, want 0 for %d", i, named, MaxSize)
			}
		} else if named != want {
			t.Fatalf("entry %d names %d, want %d", i, named, want)
		}
		if e[1] != e[0] {
			t.Fatalf("entry %d is not square: %d x %d", i, e[0], e[1])
		}
		if planes := binary.LittleEndian.Uint16(e[4:6]); planes != 1 {
			t.Fatalf("entry %d planes = %d", i, planes)
		}
		if bits := binary.LittleEndian.Uint16(e[6:8]); bits != 32 {
			t.Fatalf("entry %d bit count = %d", i, bits)
		}
		size := binary.LittleEndian.Uint32(e[8:12])
		offset := binary.LittleEndian.Uint32(e[12:16])
		if int(offset)+int(size) > len(data) {
			t.Fatalf("entry %d runs past the file: offset %d size %d of %d", i, offset, size, len(data))
		}
		img, err := png.Decode(bytes.NewReader(data[offset : offset+size]))
		if err != nil {
			t.Fatalf("entry %d payload: %v", i, err)
		}
		if b := img.Bounds(); b.Dx() != want || b.Dy() != want {
			t.Fatalf("entry %d decodes as %v, want %dx%d", i, b, want, want)
		}
	}
}

// TestBuildRejectsBadInput keeps the failures explicit: an .ico cannot name a
// rectangle or anything over 256 px, and an empty icon is useless.
func TestBuildRejectsBadInput(t *testing.T) {
	if err := Build(&bytes.Buffer{}, nil); err == nil {
		t.Fatal("Build with no images succeeded")
	}
	if err := Build(&bytes.Buffer{}, []image.Image{solid(300, color.RGBA{A: 255})}); err == nil {
		t.Fatal("Build accepted a 300x300 image")
	}
	rect := image.NewRGBA(image.Rect(0, 0, 32, 16))
	if err := Build(&bytes.Buffer{}, []image.Image{rect}); err == nil {
		t.Fatal("Build accepted a 32x16 image")
	}
	if _, err := Fit(solid(32, color.RGBA{A: 255}), 512); err == nil {
		t.Fatal("Fit accepted a 512 px target")
	}
	if _, err := Sizes("16,,abc"); err == nil {
		t.Fatal("Sizes accepted a non-numeric size")
	}
	if got, err := Sizes(" 16, 32 ,256 "); err != nil || len(got) != 3 || got[2] != 256 {
		t.Fatalf("Sizes = %v (%v)", got, err)
	}
}
