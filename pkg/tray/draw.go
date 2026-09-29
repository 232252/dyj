package tray

import (
	"bytes"
	"image"
	"image/png"
)

// tiny drawing shims keep tray.go free of image imports.

type rgbaImage struct{ *image.RGBA }

func imageNewRGBA(w, h int) rgbaImage {
	return rgbaImage{image.NewRGBA(image.Rect(0, 0, w, h))}
}

func (m rgbaImage) set(x, y int, r, g, b, a uint8) {
	off := y*m.Stride + x*4
	m.Pix[off] = r
	m.Pix[off+1] = g
	m.Pix[off+2] = b
	m.Pix[off+3] = a
}

func pngEncode(buf *bytes.Buffer, img image.Image) error {
	return png.Encode(buf, img)
}
