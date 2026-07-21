// Prepares the Mustang app icon from artwork: finds the artwork bounds
// inside the black background, crops it, scales it to Apple's icon grid
// (824px content on a 1024px canvas) and applies the standard macOS
// rounded-rect mask. Output: icon/icon-1024.png.
//
// Usage: go run ./icon <source.png>
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"os"

	xdraw "golang.org/x/image/draw"
)

const (
	canvas  = 1024
	content = 824 // Apple icon grid: content square on the 1024 canvas
	radius  = 185 // Apple rounded-rect radius for an 824 square
)

func luma(c color.Color) int {
	r, g, b, _ := c.RGBA()
	return int(r>>8+g>>8+b>>8) / 3
}

// artworkBounds scans from the center row/column outward for the first
// non-black pixels to find where the artwork sits on the background.
func artworkBounds(img image.Image) image.Rectangle {
	b := img.Bounds()
	cx, cy := (b.Min.X+b.Max.X)/2, (b.Min.Y+b.Max.Y)/2
	const dark = 50
	left, right, top, bottom := b.Min.X, b.Max.X-1, b.Min.Y, b.Max.Y-1
	for x := b.Min.X; x < b.Max.X; x++ {
		if luma(img.At(x, cy)) > dark {
			left = x
			break
		}
	}
	for x := b.Max.X - 1; x >= b.Min.X; x-- {
		if luma(img.At(x, cy)) > dark {
			right = x
			break
		}
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		if luma(img.At(cx, y)) > dark {
			top = y
			break
		}
	}
	for y := b.Max.Y - 1; y >= b.Min.Y; y-- {
		if luma(img.At(cx, y)) > dark {
			bottom = y
			break
		}
	}
	return image.Rect(left, top, right+1, bottom+1)
}

func insideRoundRect(x, y, x0, y0, x1, y1, r int) bool {
	if x < x0 || x >= x1 || y < y0 || y >= y1 {
		return false
	}
	cx, cy := x, y
	if x < x0+r {
		cx = x0 + r
	} else if x >= x1-r {
		cx = x1 - r - 1
	}
	if y < y0+r {
		cy = y0 + r
	} else if y >= y1-r {
		cy = y1 - r - 1
	}
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: go run ./icon <source.png>")
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	src, err := png.Decode(f)
	f.Close()
	if err != nil {
		log.Fatal(err)
	}

	crop := artworkBounds(src)
	// Center-column scans can hit dark artwork regions; the artwork is a
	// centered square, so trust the horizontal extent and center the crop.
	side := crop.Dx()
	b := src.Bounds()
	cx, cy := (b.Min.X+b.Max.X)/2, (b.Min.Y+b.Max.Y)/2
	crop = image.Rect(cx-side/2, cy-side/2, cx+side/2, cy+side/2)
	log.Printf("artwork bounds (squared): %v (of %v)", crop, src.Bounds())

	scaled := image.NewNRGBA(image.Rect(0, 0, content, content))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), src, crop, xdraw.Src, nil)

	out := image.NewNRGBA(image.Rect(0, 0, canvas, canvas))
	off := (canvas - content) / 2
	for y := 0; y < content; y++ {
		for x := 0; x < content; x++ {
			if insideRoundRect(x, y, 0, 0, content, content, radius) {
				out.SetNRGBA(off+x, off+y, scaled.NRGBAAt(x, y))
			}
		}
	}

	o, err := os.Create("icon/icon-1024.png")
	if err != nil {
		log.Fatal(err)
	}
	defer o.Close()
	if err := png.Encode(o, out); err != nil {
		log.Fatal(err)
	}
	log.Print("wrote icon/icon-1024.png")
}
