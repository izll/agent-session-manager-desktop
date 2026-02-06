package main

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
)

// addDevBadge draws a full-width yellow "DEV" banner across the bottom of the icon.
func addDevBadge(pngData []byte) []byte {
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return pngData
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	result := image.NewRGBA(bounds)
	draw.Draw(result, bounds, img, bounds.Min, draw.Src)

	// Scale factor based on icon size
	scale := w / 16
	if scale < 1 {
		scale = 1
	}

	// "DEV" bitmap font (3x5 per character)
	letters := map[byte][]string{
		'D': {"##.", "#.#", "#.#", "#.#", "##."},
		'E': {"###", "#..", "##.", "#..", "###"},
		'V': {"#.#", "#.#", "#.#", ".#.", ".#."},
	}

	text := "DEV"
	charW, charH := 3, 5
	spacing := 1
	textW := len(text)*charW + (len(text)-1)*spacing
	textH := charH

	padY := 2
	bannerH := (textH + padY*2) * scale

	// Full-width yellow banner at the bottom
	bannerY := h - bannerH
	yellow := color.RGBA{R: 255, G: 200, B: 0, A: 255}
	for y := bannerY; y < h; y++ {
		for x := 0; x < w; x++ {
			result.Set(x, y, yellow)
		}
	}

	// Center text horizontally
	totalTextW := textW * scale
	textStartX := (w - totalTextW) / 2
	textStartY := bannerY + padY*scale

	// Draw text in black
	black := color.RGBA{R: 0, G: 0, B: 0, A: 255}
	curX := textStartX

	for _, ch := range []byte(text) {
		rows := letters[ch]
		for row, rowStr := range rows {
			for col, c := range rowStr {
				if c == '#' {
					px := curX + col*scale
					py := textStartY + row*scale
					for dy := 0; dy < scale; dy++ {
						for dx := 0; dx < scale; dx++ {
							result.Set(px+dx, py+dy, black)
						}
					}
				}
			}
		}
		curX += (charW + spacing) * scale
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, result); err != nil {
		return pngData
	}
	return buf.Bytes()
}
