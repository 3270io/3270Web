// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"math"
	"net"
	"net/url"
	"strings"

	webassets "github.com/jnnngs/3270Web"
)

// The artwork behind the desktop window that opens when somebody
// double-clicks the executable.
//
// Everything here is plain Go image work with no Windows in it, for two
// reasons. The first is that GDI, which the window itself draws with, has no
// anti-aliasing: a rounded button or a status dot drawn with RoundRect or
// Ellipse comes out with stepped edges, which is exactly the detail that
// makes a window look unfinished next to the browser UI. Rasterising the
// shapes here, from a signed distance field, gives the same smooth edges the
// browser gets, and the window only has to blit the result.
//
// The second is that it can be tested and looked at on any machine. The
// window needs Windows; these pictures do not.

// splashColor is one 8-bit sRGB colour.
//
// The palette is the brand's, not the terminal's. Those are two different
// greens and the difference is the point: a 3270 screen is drawn in phosphor
// green because that is what a 3270 screen looks like, while everything
// around the screen — the mark, the site, the documentation — is the brand's
// emerald. This window is chrome rather than a screen, and it is looking at
// the mark, so phosphor next to the mark reads as two greens that missed each
// other.
//
// Every value below is a published token: the two emeralds are the stops of
// the mark's own face gradient in brand/3270web-icon.svg, and the rest are
// the dark palette from docs/stylesheets/3270-theme.css, which is the same
// brand on the same near-black.
type splashColor struct{ R, G, B uint8 }

var (
	splashBG     = splashColor{0x03, 0x11, 0x0d} // 3270-theme --bg
	splashPanel  = splashColor{0x06, 0x20, 0x1a} // --surface-solid
	splashPanel2 = splashColor{0x0c, 0x32, 0x26} // --surface-solid lifted towards --accent
	splashBorder = splashColor{0x11, 0x44, 0x32} // --line, flattened onto the panel

	splashInk   = splashColor{0xe6, 0xff, 0xf5} // --text
	splashMuted = splashColor{0x9f, 0xe6, 0xc8} // --text-2
	splashDim   = splashColor{0x5f, 0x9e, 0x86} // --text-3

	// The mark's own two greens, and the ink dark enough to sit on them.
	splashMark     = splashColor{0x00, 0xa7, 0x6f} // icon face gradient, light stop
	splashMarkDeep = splashColor{0x00, 0x87, 0x5a} // icon face gradient, dark stop
	splashOnMark   = splashColor{0x02, 0x13, 0x0a} // --accent-ink
	splashGlow     = splashColor{0x7c, 0xf9, 0xd0} // --accent-2
)

// mix returns c blended towards other, t running 0 (all c) to 1 (all other).
func (c splashColor) mix(other splashColor, t float64) splashColor {
	if t <= 0 {
		return c
	}
	if t >= 1 {
		return other
	}
	return splashColor{
		R: lerp8(c.R, other.R, t),
		G: lerp8(c.G, other.G, t),
		B: lerp8(c.B, other.B, t),
	}
}

// brighten scales every channel by f, the way `filter: brightness(f)` does in
// the stylesheet the buttons are modelled on. Channels clamp at 255.
func (c splashColor) brighten(f float64) splashColor {
	return splashColor{R: clamp8(float64(c.R) * f), G: clamp8(float64(c.G) * f), B: clamp8(float64(c.B) * f)}
}

func lerp8(a, b uint8, t float64) uint8 {
	return clamp8(float64(a) + (float64(b)-float64(a))*t)
}

func clamp8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

func clamp01(v float64) float64 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 1
	}
	return v
}

// splashPlate is one rounded rectangle: the face of a button, drawn onto the
// flat colour behind it. Sizes are in device pixels, so a plate is rendered
// for the DPI it will be shown at rather than scaled up to it.
type splashPlate struct {
	Width, Height int
	Radius        float64
	Inset         float64 // keeps the shape off the edge, leaving room for the shadow and the press

	// Fill runs along the 135° axis — top-left to bottom-right — which is
	// both the run the stylesheet's buttons use and the run the mark's own
	// face gradient is drawn along.
	FillFrom, FillTo splashColor

	Border      splashColor
	BorderWidth float64

	// Ring is the keyboard focus indicator, drawn inside the face so it
	// cannot be clipped by the widget's own bounds.
	Ring      splashColor
	RingWidth float64
	RingInset float64

	// OffsetY shifts the face down, which is how the pressed state reads.
	// `.auth-submit:active` does the same with translateY(1px).
	OffsetY float64

	Over splashColor
}

// splashRenderPlate rasterises one plate. The result is opaque: the plate is
// composited onto Over here rather than relying on the window to blend it,
// which keeps the blit a straight copy.
func splashRenderPlate(p splashPlate) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, p.Width, p.Height))
	if p.Width <= 0 || p.Height <= 0 {
		return img
	}

	halfW := float64(p.Width)/2 - p.Inset
	halfH := float64(p.Height)/2 - p.Inset
	centreX := float64(p.Width) / 2
	centreY := float64(p.Height)/2 + p.OffsetY
	radius := math.Min(p.Radius, math.Min(halfW, halfH))
	span := float64(p.Width + p.Height)

	for y := 0; y < p.Height; y++ {
		py := float64(y) + 0.5
		for x := 0; x < p.Width; x++ {
			px := float64(x) + 0.5
			d := roundedBoxSDF(px-centreX, py-centreY, halfW, halfH, radius)

			cover := clamp01(0.5 - d)
			if cover == 0 {
				setRGB(img, x, y, p.Over)
				continue
			}

			t := 0.0
			if span > 0 {
				t = clamp01((px + py) / span)
			}
			face := p.FillFrom.mix(p.FillTo, t)

			if p.BorderWidth > 0 {
				inner := clamp01(0.5 - (d + p.BorderWidth))
				face = p.Border.mix(face, inner)
			}

			if p.RingWidth > 0 {
				outer := clamp01(0.5 - (d + p.RingInset))
				inner := clamp01(0.5 - (d + p.RingInset + p.RingWidth))
				face = face.mix(p.Ring, outer-inner)
			}

			setRGB(img, x, y, p.Over.mix(face, cover))
		}
	}

	return img
}

// roundedBoxSDF returns the distance from a point to the edge of a rounded
// rectangle centred on the origin — negative inside, positive outside, and
// close enough to a true distance near the edge that clamping it gives a
// pixel's coverage.
func roundedBoxSDF(px, py, halfW, halfH, radius float64) float64 {
	qx := math.Abs(px) - halfW + radius
	qy := math.Abs(py) - halfH + radius
	outside := math.Hypot(math.Max(qx, 0), math.Max(qy, 0))
	inside := math.Min(math.Max(qx, qy), 0)
	return outside + inside - radius
}

func setRGB(img *image.RGBA, x, y int, c splashColor) {
	i := img.PixOffset(x, y)
	img.Pix[i] = c.R
	img.Pix[i+1] = c.G
	img.Pix[i+2] = c.B
	img.Pix[i+3] = 0xff
}

// splashHairline draws the rule along the top of the window: the shape of
// gradient `.auth-card::before` puts along the top of every card in the
// browser UI — nothing at the edges, colour through the middle — swept
// through the brand's two greens.
func splashHairline(width, height int, over splashColor) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	if width <= 0 || height <= 0 {
		return img
	}

	const (
		accentStart = 0.22 // stop positions from the stylesheet
		accentEnd   = 0.62
		opacity     = 0.85
	)

	for x := 0; x < width; x++ {
		t := (float64(x) + 0.5) / float64(width)

		var (
			colour = splashMark
			alpha  float64
		)
		switch {
		case t < accentStart:
			alpha = t / accentStart
		case t < accentEnd:
			colour = splashMark.mix(splashGlow, (t-accentStart)/(accentEnd-accentStart))
			alpha = 1
		default:
			colour = splashGlow
			alpha = (1 - t) / (1 - accentEnd)
		}

		lit := over.mix(colour, clamp01(alpha)*opacity)
		for y := 0; y < height; y++ {
			setRGB(img, x, y, lit)
		}
	}

	return img
}

// splashStatusDot draws the lamp beside "Running": a filled circle with a
// soft halo, so the one moving part of the window reads as alive rather than
// as a full stop.
func splashStatusDot(size int, colour, over splashColor) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	if size <= 0 {
		return img
	}

	centre := float64(size) / 2
	radius := float64(size) * 0.28
	halo := float64(size)*0.5 - radius

	for y := 0; y < size; y++ {
		py := float64(y) + 0.5 - centre
		for x := 0; x < size; x++ {
			px := float64(x) + 0.5 - centre
			d := math.Hypot(px, py) - radius

			pixel := over
			if halo > 0 && d > 0 {
				pixel = over.mix(colour, clamp01(1-d/halo)*0.35)
			}
			pixel = pixel.mix(colour, clamp01(0.5-d))
			setRGB(img, x, y, pixel)
		}
	}

	return img
}

// splashLogo renders the application mark — the same picture the title bar
// and the taskbar show — at size device pixels square, flattened onto the
// colour it sits on.
//
// The source is the 512px master, so it is always downsampled: the averaging
// below is what keeps the mark's edges clean at 40-odd pixels, where letting
// the platform stretch it would not.
func splashLogo(size int, over splashColor) (*image.RGBA, error) {
	src, err := splashLogoSource()
	if err != nil {
		return nil, err
	}
	return splashScaleOver(src, splashLogoCrop(src), size, over), nil
}

func splashLogoSource() (image.Image, error) {
	data, err := webassets.FS.ReadFile("web/static/3270Web_logo.png")
	if err != nil {
		return nil, fmt.Errorf("splash logo: %w", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("splash logo: %w", err)
	}
	return img, nil
}

// splashLogoCrop finds the square worth drawing. The master has a wide
// transparent margin — sized for an application icon, which is padded by
// convention — and beside a wordmark that margin reads as the mark being too
// small rather than as breathing room.
func splashLogoCrop(src image.Image) image.Rectangle {
	bounds := src.Bounds()

	// The threshold keys on the mark rather than on the glow around it: a
	// crop drawn round the faintest lit pixel is barely a crop at all.
	const inkAlpha = 0x2000

	inked := image.Rectangle{Min: bounds.Max, Max: bounds.Min}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if _, _, _, a := src.At(x, y).RGBA(); a > inkAlpha {
				inked.Min.X = min(inked.Min.X, x)
				inked.Min.Y = min(inked.Min.Y, y)
				inked.Max.X = max(inked.Max.X, x+1)
				inked.Max.Y = max(inked.Max.Y, y+1)
			}
		}
	}
	if inked.Empty() {
		return bounds
	}

	// A square around the ink with a little air, kept square when it runs
	// into the edge of the master: the widget it fills is square, and
	// clipping one side of the crop would squash the mark to fit.
	side := float64(max(inked.Dx(), inked.Dy())) * 1.08
	side = math.Min(side, float64(min(bounds.Dx(), bounds.Dy())))

	cx := clampFloat(float64(inked.Min.X+inked.Max.X)/2, float64(bounds.Min.X)+side/2, float64(bounds.Max.X)-side/2)
	cy := clampFloat(float64(inked.Min.Y+inked.Max.Y)/2, float64(bounds.Min.Y)+side/2, float64(bounds.Max.Y)-side/2)

	left := int(math.Round(cx - side/2))
	top := int(math.Round(cy - side/2))
	length := int(math.Round(side))
	return image.Rect(left, top, left+length, top+length).Intersect(bounds)
}

func clampFloat(v, lo, hi float64) float64 {
	if lo > hi {
		return (lo + hi) / 2
	}
	return math.Max(lo, math.Min(hi, v))
}

// splashScaleOver box-filters src's crop down to a size-square image, laying
// each source pixel over the background colour first so the mark's soft edge
// blends into the window rather than onto white.
func splashScaleOver(src image.Image, crop image.Rectangle, size int, over splashColor) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	if size <= 0 || crop.Empty() {
		return dst
	}

	scaleX := float64(crop.Dx()) / float64(size)
	scaleY := float64(crop.Dy()) / float64(size)

	for y := 0; y < size; y++ {
		y0 := float64(crop.Min.Y) + float64(y)*scaleY
		y1 := y0 + scaleY
		for x := 0; x < size; x++ {
			x0 := float64(crop.Min.X) + float64(x)*scaleX
			x1 := x0 + scaleX

			var sumR, sumG, sumB, sumW float64
			for sy := int(math.Floor(y0)); sy < int(math.Ceil(y1)); sy++ {
				wy := overlap(float64(sy), float64(sy+1), y0, y1)
				if wy <= 0 {
					continue
				}
				for sx := int(math.Floor(x0)); sx < int(math.Ceil(x1)); sx++ {
					wx := overlap(float64(sx), float64(sx+1), x0, x1)
					if wx <= 0 {
						continue
					}
					// RGBA() hands back premultiplied 16-bit channels, so
					// laying the pixel over the background is an add of
					// what the background still shows through.
					r, g, b, a := src.At(sx, sy).RGBA()
					clear := 1 - float64(a)/0xffff
					w := wx * wy
					sumR += (float64(r)/0x101 + float64(over.R)*clear) * w
					sumG += (float64(g)/0x101 + float64(over.G)*clear) * w
					sumB += (float64(b)/0x101 + float64(over.B)*clear) * w
					sumW += w
				}
			}

			pixel := over
			if sumW > 0 {
				pixel = splashColor{R: clamp8(sumR / sumW), G: clamp8(sumG / sumW), B: clamp8(sumB / sumW)}
			}
			setRGB(dst, x, y, pixel)
		}
	}

	return dst
}

func overlap(aLo, aHi, bLo, bHi float64) float64 {
	return math.Max(0, math.Min(aHi, bHi)-math.Max(aLo, bLo))
}

// splashAddress is the address the window shows and the button opens.
//
// The server reports the address it actually bound, which for a container or
// a LAN-visible deployment is a wildcard — 0.0.0.0, or :: for IPv6. Neither
// is somewhere a browser can be sent, and neither is what an operator would
// type, so a wildcard is shown as localhost. Any real address is left alone.
func splashAddress(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return raw
	}

	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		host, port = parsed.Host, ""
	}

	if !splashIsWildcard(host) {
		return raw
	}

	parsed.Host = "localhost"
	if port != "" {
		parsed.Host = net.JoinHostPort("localhost", port)
	}
	return parsed.String()
}

func splashIsWildcard(host string) bool {
	host = strings.Trim(host, "[]")
	if host == "" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}

// splashVersion is the version stamp in the corner of the window.
func splashVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "dev"
	}
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}
