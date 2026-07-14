package filepreview

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"github.com/muesli/termenv"
)

// ---------------------------------------------------------------------------
// ANSI character dithering with Floyd-Steinberg error diffusion
//
// Instead of mapping exact pixel colors to half-block characters (which
// produces very blocky output at terminal resolution), this renderer uses
// Unicode shade characters (█ ▓ ▒ ░ ⠀) with error-diffused luminance to
// create smooth continuous-tone images.
//
// Each cell displays one source pixel. The luminance is approximated by
// selecting a character whose "ink coverage" best matches the desired
// brightness, then coloring it with the pixel's original color. Floyd-
// Steinberg error diffusion distributes the quantization error to
// neighboring cells, eliminating banding and producing a natural look.
// ---------------------------------------------------------------------------

// ditherLevel maps a luminance fraction (0.0–1.0) to a Unicode shade
// character and its approximate pixel coverage.
type ditherLevel struct {
	char     rune
	coverage float64 // fraction of the cell filled by ink (0.0–1.0)
}

var ditherLevels = []ditherLevel{
	{'█', 1.000},
	{'▓', 0.750},
	{'▒', 0.500},
	{'░', 0.250},
	{' ', 0.000},
}

// floydErr holds accumulated luminance error for one cell (0 = below,
// 1 = right in the current row, 2 = left in the next row, etc. depending
// on position).
type floydBuf struct {
	data []float64
	w    int
}

func newFloydBuf(w int) *floydBuf {
	// Two rows of float64 per column + 2 guard columns on each side
	return &floydBuf{data: make([]float64, (w+4)*2), w: w}
}

func (fb *floydBuf) get(cur, col int) float64 {
	return fb.data[cur*(fb.w+4)+col+2]
}

func (fb *floydBuf) add(cur, col int, v float64) {
	idx := cur*(fb.w+4) + col + 2
	fb.data[idx] += v
}

// clearNext zeroes the next row before a new scanline.
func (fb *floydBuf) clearNext(cur int) {
	next := 1 - cur
	off := next * (fb.w + 4)
	for i := range fb.w + 4 {
		fb.data[off+i] = 0
	}
}

// luminance8 computes perceived luminance of an 8-bit RGB color (0-255).
func luminance8(r, g, b uint8) int {
	return (int(r)*299 + int(g)*587 + int(b)*114) / 1000
}

// luminance8f is the float64 version (0.0–1.0).
func luminance8f(r, g, b uint8) float64 {
	return float64(luminance8(r, g, b)) / 255.0
}

// ConvertImageToANSIDithered converts an image to ANSI using Floyd-Steinberg
// error-diffused character dithering.
//
// The source image should already be resized to the target terminal grid
// dimensions (maxWidth × maxHeight). Each output cell shows one source pixel
// using a shade character (█→▓→▒→░→space) + the pixel's original color.
func ConvertImageToANSIDithered(img image.Image, bgLum float64) string {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	var out strings.Builder
	fb := newFloydBuf(w)

	for y := range h {
		cur := y & 1 // 0 or 1 — which row of the error buffer we're writing
		fb.clearNext(cur)

		for x := range w {
			r, g, b, _ := img.At(x, y).RGBA()
			r8, g8, b8 := uint8(r>>8), uint8(g>>8), uint8(b>>8)
			pixelLum := luminance8f(r8, g8, b8)

			// Accumulated error from already-processed neighbors
			err := fb.get(cur, x)
			adjusted := pixelLum + err
			if adjusted < 0 {
				adjusted = 0
			} else if adjusted > 1 {
				adjusted = 1
			}

			// Find the shade character whose coverage best approximates
			// the adjusted luminance.
			var best ditherLevel
			bestDiff := 2.0
			for _, l := range ditherLevels {
				achieved := l.coverage*adjusted + (1-l.coverage)*bgLum
				diff := achieved - adjusted
				if diff < 0 {
					diff = -diff
				}
				if diff < bestDiff {
					bestDiff = diff
					best = l
				}
			}

			// Achieved luminance for error calculation
			achieved := best.coverage*adjusted + (1-best.coverage)*bgLum
			qErr := adjusted - achieved

			// Floyd-Steinberg distribution
			const (
				right = 7.0 / 16.0
				bl    = 3.0 / 16.0
				bm    = 5.0 / 16.0
				br    = 1.0 / 16.0
			)
			if x+1 < w {
				fb.add(cur, x+1, qErr*right)
			}
			if y+1 < h {
				fb.add(1-cur, x-1, qErr*bl)
				fb.add(1-cur, x, qErr*bm)
				if x+1 < w {
					fb.add(1-cur, x+1, qErr*br)
				}
			}

			// Output: colored shade character
			col := termenv.RGBColor(fmt.Sprintf("#%02x%02x%02x", r8, g8, b8))
			cell := termenv.String(string(best.char)).Foreground(col)
			out.WriteString(cell.String())
		}
		if y+1 < h {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// ---------------------------------------------------------------------------
// Half-block fallback (used when dithering produces unexpected results)
// ---------------------------------------------------------------------------

// minContrast is the minimum luminance difference for the ▄ half-block char.
const minContrast = 40

func clamp8(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func colorToHex8(c color.RGBA) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

func avgRGBA(a, b color.RGBA) color.RGBA {
	return color.RGBA{
		R: uint8((int(a.R) + int(b.R)) / 2),
		G: uint8((int(a.G) + int(b.G)) / 2),
		B: uint8((int(a.B) + int(b.B)) / 2),
		A: uint8((int(a.A) + int(b.A)) / 2),
	}
}

func ensureContrast8(fg color.RGBA, bgLum int) color.RGBA {
	fgLum := luminance8(fg.R, fg.G, fg.B)
	diff := fgLum - bgLum
	if diff < 0 {
		diff = -diff
	}
	if diff >= minContrast {
		return fg
	}
	deficit := minContrast - diff
	if deficit < 30 {
		deficit = 30
	}
	distanceToBlack := fgLum
	distanceToWhite := 255 - fgLum
	bgIsLight := bgLum > 128
	if bgIsLight && distanceToBlack >= deficit {
		return color.RGBA{
			R: clamp8(int(fg.R) - deficit*int(fg.R)/255),
			G: clamp8(int(fg.G) - deficit*int(fg.G)/255),
			B: clamp8(int(fg.B) - deficit*int(fg.B)/255),
			A: fg.A,
		}
	}
	if !bgIsLight && distanceToWhite >= deficit {
		return color.RGBA{
			R: clamp8(int(fg.R) + deficit*(255-int(fg.R))/255),
			G: clamp8(int(fg.G) + deficit*(255-int(fg.G))/255),
			B: clamp8(int(fg.B) + deficit*(255-int(fg.B))/255),
			A: fg.A,
		}
	}
	if distanceToWhite >= deficit {
		return color.RGBA{
			R: clamp8(int(fg.R) + deficit*(255-int(fg.R))/255),
			G: clamp8(int(fg.G) + deficit*(255-int(fg.G))/255),
			B: clamp8(int(fg.B) + deficit*(255-int(fg.B))/255),
			A: fg.A,
		}
	}
	if fgLum > 128 {
		return color.RGBA{R: 0, G: 0, B: 0, A: fg.A}
	}
	return color.RGBA{R: 255, G: 255, B: 255, A: fg.A}
}

// ConvertImageToANSIHalfBlock renders an image using ▄ half-block characters.
// Kept as a fallback — prefers ConvertImageToANSIDithered.
func ConvertImageToANSIHalfBlock(img image.Image, defaultBGColor color.Color) string {
	srcW := img.Bounds().Dx()
	height := img.Bounds().Dy()

	width := srcW / 2
	if srcW%2 != 0 {
		width++
	}

	var out strings.Builder
	cache := newColorCache()
	defaultBGHex := colorToHex(defaultBGColor)
	defaultRGBA := color.RGBAModel.Convert(defaultBGColor).(color.RGBA)

	for y := 0; y < height; y += 2 {
		for col := range width {
			srcX := col * 2
			p1 := color.RGBAModel.Convert(img.At(srcX, y)).(color.RGBA)
			p2 := color.RGBAModel.Convert(img.At(min(srcX+1, srcW-1), y)).(color.RGBA)
			upperRGBA := avgRGBA(p1, p2)
			upperColor := cache.getTermenvColor(upperRGBA, defaultBGHex)
			upperLum := luminance8(upperRGBA.R, upperRGBA.G, upperRGBA.B)

			lowerRGBA := defaultRGBA
			if y+1 < height {
				p1 := color.RGBAModel.Convert(img.At(srcX, y+1)).(color.RGBA)
				p2 := color.RGBAModel.Convert(img.At(min(srcX+1, srcW-1), y+1)).(color.RGBA)
				lowerRGBA = avgRGBA(p1, p2)
			}
			enhanced := ensureContrast8(lowerRGBA, upperLum)
			lowerColor := termenv.RGBColor(colorToHex8(enhanced))

			cell := termenv.String("▄").Foreground(lowerColor).Background(upperColor)
			out.WriteString(cell.String())
		}
		if y+2 < height {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// ANSIRenderer converts an image to ANSI escape codes for terminal display.
// It uses Floyd-Steinberg dithered character rendering when the preview panel
// has at least 20 columns of width; below that it falls back to the half-
// block renderer.
func (p *ImagePreviewer) ANSIRenderer(img image.Image, defaultBGColor string,
	maxWidth int, maxHeight int) (string, error) {
	bgColor, err := hexToColor(defaultBGColor)
	if err != nil {
		return "", fmt.Errorf("invalid background color: %w", err)
	}
	bgLum := luminance8f(bgColor.R, bgColor.G, bgColor.B)

	// Resize to the exact terminal cell grid (dithered uses 1:1).
	fittedImg := resizeForANSIDithered(img, maxWidth, maxHeight)
	return ConvertImageToANSIDithered(fittedImg, bgLum), nil
}

// ---------------------------------------------------------------------------
// Color cache (shared by half-block renderer)
// ---------------------------------------------------------------------------

type colorCache struct {
	rgbaToTermenv map[color.RGBA]termenv.RGBColor
}

func newColorCache() *colorCache {
	return &colorCache{
		rgbaToTermenv: make(map[color.RGBA]termenv.RGBColor),
	}
}

func (c *colorCache) getTermenvColor(col color.Color, fallbackColor string) termenv.RGBColor {
	rgba, ok := color.RGBAModel.Convert(col).(color.RGBA)
	if !ok || rgba.A == 0 {
		return termenv.RGBColor(fallbackColor)
	}
	if termenvColor, exists := c.rgbaToTermenv[rgba]; exists {
		return termenvColor
	}
	termenvColor := termenv.RGBColor(fmt.Sprintf("#%02x%02x%02x", rgba.R, rgba.G, rgba.B))
	c.rgbaToTermenv[rgba] = termenvColor
	return termenvColor
}
