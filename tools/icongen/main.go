// Command icongen writes the .ico the Windows build embeds from one square
// source image (the same artwork the web preview and the macOS bundle use):
//
//	go run ./tools/icongen -in web/public/android-chrome-512x512.png \
//	  -out web/public/app-icon.ico
//
// shell/winres.sh runs it before go-winres, so the executable's icon has a
// crisp entry for every size Windows asks for instead of one 256 px image it
// would have to scale down itself.
package main

import (
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"os"

	"github.com/lsj5031/joplin-lite/internal/iconico"
)

func main() {
	in := flag.String("in", "", "square source image (PNG or JPEG)")
	out := flag.String("out", "", "destination .ico")
	sizes := flag.String("sizes", "16,32,48,256", "comma-separated square sizes, smallest first")
	flag.Parse()
	if *in == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}

	src, err := load(*in)
	if err != nil {
		log.Fatalf("icongen: %v", err)
	}
	side, err := iconico.Sizes(*sizes)
	if err != nil {
		log.Fatalf("icongen: %v", err)
	}
	imgs := make([]image.Image, 0, len(side))
	for _, s := range side {
		img, err := iconico.Fit(src, s)
		if err != nil {
			log.Fatalf("icongen: %v", err)
		}
		imgs = append(imgs, img)
	}

	f, err := os.Create(*out)
	if err != nil {
		log.Fatalf("icongen: %v", err)
	}
	if err := iconico.Build(f, imgs); err != nil {
		f.Close()
		log.Fatalf("icongen: %v", err)
	}
	if err := f.Close(); err != nil {
		log.Fatalf("icongen: %v", err)
	}
	fmt.Printf("wrote %s (%v) from %s\n", *out, side, *in)
}

func load(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, format, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	b := img.Bounds()
	fmt.Printf("read %s (%s, %dx%d)\n", path, format, b.Dx(), b.Dy())
	return img, nil
}
