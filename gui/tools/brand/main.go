// Generates PNG previews and a multi-resolution Windows icon from the SVG source.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

func render(source []byte, size int) ([]byte, error) {
	return renderSize(source, size, size)
}

func renderSize(source []byte, width, height int) ([]byte, error) {
	icon, err := oksvg.ReadIconStream(bytes.NewReader(source))
	if err != nil {
		return nil, err
	}
	icon.SetTarget(0, 0, float64(width), float64(height))
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	icon.Draw(rasterx.NewDasher(width, height, rasterx.NewScannerGV(width, height, img, img.Bounds())), 1)
	var out bytes.Buffer
	err = png.Encode(&out, img)
	return out.Bytes(), err
}

func generate() error {
	source, err := os.ReadFile(filepath.Join("assets", "drift-icon.svg"))
	if err != nil {
		return err
	}
	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	images := make([][]byte, len(sizes))
	for i, size := range sizes {
		images[i], err = render(source, size)
		if err != nil {
			return err
		}
	}
	var out bytes.Buffer
	write := func(value any) { _ = binary.Write(&out, binary.LittleEndian, value) }
	write(uint16(0))
	write(uint16(1))
	write(uint16(len(sizes)))
	offset := 6 + 16*len(sizes)
	for i, size := range sizes {
		write(uint8(size % 256))
		write(uint8(size % 256))
		write(uint8(0))
		write(uint8(0))
		write(uint16(1))
		write(uint16(32))
		write(uint32(len(images[i])))
		write(uint32(offset))
		offset += len(images[i])
	}
	for _, data := range images {
		out.Write(data)
	}
	if err = os.WriteFile(filepath.Join("assets", "drift.ico"), out.Bytes(), 0644); err != nil {
		return err
	}
	preview, err := render(source, 512)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join("assets", "drift-icon.png"), preview, 0644); err != nil {
		return err
	}
	logoSource, err := os.ReadFile(filepath.Join("assets", "drift-logo.svg"))
	if err != nil {
		return err
	}
	logo, err := renderSize(logoSource, 1200, 512)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join("assets", "drift-logo.png"), logo, 0644)
}

func main() {
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
