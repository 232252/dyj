// Command genicons regenerates every raster icon derivative from the
// 512px Chromium render of assets/brand/dyj-logo.svg (see
// docs/research/11 §4.3). Produce the master first:
//
//	chrome --headless=new --disable-gpu --user-data-dir=<tmp> \
//	  --hide-scrollbars --default-background-color=00000000 \
//	  --window-size=512,512 --screenshot=dist/icons/dyj-512.png \
//	  file:///<repo>/dist/icons/render-big.html
//
// (render-big.html shows the SVG at a fixed 512px; a 100vw layout gets
// zoomed by Windows display scaling at large window sizes.) Then:
//
//	go run ./scripts/genicons
//
// writes the hicolor PNGs for Linux and the multi-size favicon.ico
// embedded in the admin UI. All paths are literals on purpose.
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"log"
	"os"

	"golang.org/x/image/draw"
)

func main() {
	raw, err := os.ReadFile("dist/icons/dyj-512.png")
	if err != nil {
		log.Fatal(err)
	}
	src, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		log.Fatal(err)
	}
	if b := src.Bounds(); b.Dx() != 512 || b.Dy() != 512 {
		log.Fatalf("master is %dx%d, want 512x512", b.Dx(), b.Dy())
	}

	pngs := map[int][]byte{}
	for _, e := range []struct {
		size int
		path string
		dir  string
	}{
		{16, "packaging/linux/icons/hicolor/16/apps/dyj.png", "packaging/linux/icons/hicolor/16/apps"},
		{32, "packaging/linux/icons/hicolor/32/apps/dyj.png", "packaging/linux/icons/hicolor/32/apps"},
		{48, "packaging/linux/icons/hicolor/48/apps/dyj.png", "packaging/linux/icons/hicolor/48/apps"},
		{64, "packaging/linux/icons/hicolor/64/apps/dyj.png", "packaging/linux/icons/hicolor/64/apps"},
		{128, "packaging/linux/icons/hicolor/128/apps/dyj.png", "packaging/linux/icons/hicolor/128/apps"},
		{256, "packaging/linux/icons/hicolor/256/apps/dyj.png", "packaging/linux/icons/hicolor/256/apps"},
	} {
		dst := image.NewNRGBA(image.Rect(0, 0, e.size, e.size))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
		var buf bytes.Buffer
		if err := png.Encode(&buf, dst); err != nil {
			log.Fatal(err)
		}
		pngs[e.size] = buf.Bytes()

		if err := os.MkdirAll(e.dir, 0o755); err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(e.path, buf.Bytes(), 0o644); err != nil {
			log.Fatal(err)
		}
		log.Printf("%s: %d bytes", e.path, buf.Len())
	}

	log.Printf("favicon.ico: %d bytes", len(favicon(pngs[16], pngs[32], pngs[48])))
	if err := os.WriteFile("pkg/admin/web/favicon.ico", favicon(pngs[16], pngs[32], pngs[48]), 0o644); err != nil {
		log.Fatal(err)
	}

	// Full-size ICO (16..256) for the Windows exe resources (go-winres reads it).
	full := ico([]int{16, 32, 48, 64, 128, 256}, pngs)
	if err := os.WriteFile("assets/brand/dyj.ico", full, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("assets/brand/dyj.ico: %d bytes", len(full))
}

// favicon packs three PNGs into an ICO container (PNG-compressed
// entries are valid for every size on Vista+).
func favicon(png16, png32, png48 []byte) []byte {
	return ico([]int{16, 32, 48}, map[int][]byte{16: png16, 32: png32, 48: png48})
}

// ico builds an ICO container from the given sizes, reading payloads
// from pngs (rendered above from the master).
func ico(sizes []int, pngs map[int][]byte) []byte {
	var out []byte
	header := make([]byte, 6)
	header[2], header[3] = 1, 0 // type: icon
	binary.LittleEndian.PutUint16(header[4:], uint16(len(sizes)))
	out = append(out, header...)

	offset := uint32(6 + 16*len(sizes))
	for _, n := range sizes {
		entry := make([]byte, 16)
		entry[0], entry[1] = uint8(n), uint8(n)
		binary.LittleEndian.PutUint16(entry[4:], 1)  // planes
		binary.LittleEndian.PutUint16(entry[6:], 32) // bpp
		binary.LittleEndian.PutUint32(entry[8:], uint32(len(pngs[n])))
		binary.LittleEndian.PutUint32(entry[12:], offset)
		out = append(out, entry...)
		offset += uint32(len(pngs[n]))
	}
	for _, n := range sizes {
		out = append(out, pngs[n]...)
	}
	return out
}
