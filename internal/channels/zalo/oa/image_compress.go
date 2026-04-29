package oa

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"log/slog"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp" // register WebP decoder
)

// Zalo OA /v2.0/oa/upload/image rejects payloads over 1MB (error -210).
// Strategy: scale longest side down, loop JPEG quality 85→35 at each size.

var (
	jpegQualityLadder = []int{85, 75, 65, 55, 45, 35}
	maxSideLadder     = []int{1600, 1200, 900, 600}
)

// maxDecodePixels caps W*H to bound the RGBA buffer image.Decode allocates,
// preventing a small payload with huge dimensions from pinning GB of memory.
const maxDecodePixels = 25_000_000

// compressForZaloImage shrinks oversized images under maxBytes. Transparent
// inputs route to PNG re-encode (JPEG would flatten alpha to black).
func compressForZaloImage(data []byte, originalMIME string, maxBytes int) ([]byte, string, error) {
	if len(data) <= maxBytes {
		return data, originalMIME, nil
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("zalo_oa: decode image header: %w", err)
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxDecodePixels {
		return nil, "", fmt.Errorf("zalo_oa: image dimensions %dx%d exceed %d pixel cap",
			cfg.Width, cfg.Height, maxDecodePixels)
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("zalo_oa: decode image for compression: %w", err)
	}
	bounds := img.Bounds()
	origW, origH := bounds.Dx(), bounds.Dy()

	if hasTransparency(img) {
		out, mt, err := compressTransparent(img, originalMIME, maxBytes)
		if err == nil {
			slog.Info("zalo_oa.image.compressed",
				"orig_bytes", len(data), "orig_mime", originalMIME,
				"new_bytes", len(out), "out_mime", mt, "transparent", true)
			return out, mt, nil
		}
		return nil, "", fmt.Errorf("zalo_oa: transparent image cannot fit under %d bytes (%dx%d original %d bytes): %w",
			maxBytes, origW, origH, len(data), err)
	}

	for _, side := range maxSideLadder {
		scaled := img
		if origW > side || origH > side {
			scaled = imaging.Fit(img, side, side, imaging.Lanczos)
		}
		for _, q := range jpegQualityLadder {
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: q}); err != nil {
				return nil, "", fmt.Errorf("zalo_oa: jpeg encode (side=%d q=%d): %w", side, q, err)
			}
			if buf.Len() <= maxBytes {
				slog.Info("zalo_oa.image.compressed",
					"orig_bytes", len(data), "orig_mime", originalMIME,
					"new_bytes", buf.Len(), "side", side, "quality", q)
				return buf.Bytes(), "image/jpeg", nil
			}
		}
	}
	return nil, "", fmt.Errorf("zalo_oa: image cannot fit under %d bytes (%dx%d original %d bytes)",
		maxBytes, origW, origH, len(data))
}

// hasTransparency reports whether any pixel is non-opaque. Samples four
// corners + a stride; corners catch the far-edge case strides can miss.
func hasTransparency(img image.Image) bool {
	switch img.ColorModel() {
	case color.RGBAModel, color.NRGBAModel, color.RGBA64Model, color.NRGBA64Model, color.AlphaModel, color.Alpha16Model:
	default:
		return false
	}
	b := img.Bounds()
	corners := [4][2]int{
		{b.Min.X, b.Min.Y},
		{b.Max.X - 1, b.Min.Y},
		{b.Min.X, b.Max.Y - 1},
		{b.Max.X - 1, b.Max.Y - 1},
	}
	for _, p := range corners {
		if _, _, _, a := img.At(p[0], p[1]).RGBA(); a < 0xffff {
			return true
		}
	}
	step := 1
	if w := b.Dx(); w > 64 {
		step = w / 64
	}
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			if _, _, _, a := img.At(x, y).RGBA(); a < 0xffff {
				return true
			}
		}
	}
	return false
}

// compressTransparent shrinks the longest side until the PNG fits under
// maxBytes (PNG has no quality knob; only dimensions).
func compressTransparent(img image.Image, _ string, maxBytes int) ([]byte, string, error) {
	bounds := img.Bounds()
	origW, origH := bounds.Dx(), bounds.Dy()
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	for _, side := range maxSideLadder {
		scaled := img
		if origW > side || origH > side {
			scaled = imaging.Fit(img, side, side, imaging.Lanczos)
		}
		var buf bytes.Buffer
		if err := enc.Encode(&buf, scaled); err != nil {
			return nil, "", fmt.Errorf("png encode (side=%d): %w", side, err)
		}
		if buf.Len() <= maxBytes {
			return buf.Bytes(), "image/png", nil
		}
	}
	return nil, "", fmt.Errorf("png too large at smallest tried side")
}
