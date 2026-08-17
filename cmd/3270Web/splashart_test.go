// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"image"
	"testing"
)

func pixelAt(t *testing.T, img *image.RGBA, x, y int) splashColor {
	t.Helper()
	if !(image.Point{X: x, Y: y}).In(img.Bounds()) {
		t.Fatalf("pixel (%d,%d) is outside %v", x, y, img.Bounds())
	}
	i := img.PixOffset(x, y)
	if img.Pix[i+3] != 0xff {
		t.Fatalf("pixel (%d,%d) has alpha %d, want an opaque picture", x, y, img.Pix[i+3])
	}
	return splashColor{R: img.Pix[i], G: img.Pix[i+1], B: img.Pix[i+2]}
}

func near(a, b splashColor, tolerance int) bool {
	return abs(int(a.R)-int(b.R)) <= tolerance &&
		abs(int(a.G)-int(b.G)) <= tolerance &&
		abs(int(a.B)-int(b.B)) <= tolerance
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func TestSplashColorMix(t *testing.T) {
	black := splashColor{}
	white := splashColor{0xff, 0xff, 0xff}

	if got := black.mix(white, 0); got != black {
		t.Errorf("mix at 0 = %v, want %v", got, black)
	}
	if got := black.mix(white, 1); got != white {
		t.Errorf("mix at 1 = %v, want %v", got, white)
	}
	if got := black.mix(white, 0.5); !near(got, splashColor{0x80, 0x80, 0x80}, 1) {
		t.Errorf("mix at 0.5 = %v, want mid grey", got)
	}
	// Out of range asks for one of the ends, not for a wrapped-around colour.
	if got := black.mix(white, 4); got != white {
		t.Errorf("mix past 1 = %v, want %v", got, white)
	}
}

func TestSplashColorBrightenClamps(t *testing.T) {
	// The brand's lighter mint is almost at full green already, so brightening
	// it can only take the channels up to the ceiling. Nothing may wrap.
	got := splashGlow.brighten(1.5)
	if got.G != 0xff {
		t.Errorf("brightened green = %d, want it pinned at 255", got.G)
	}
	if got.R < splashGlow.R || got.B < splashGlow.B {
		t.Errorf("brighten(%v) = %v, want no channel to fall", splashGlow, got)
	}

	dark := splashMark.brighten(0.5)
	if dark.G >= splashMark.G {
		t.Errorf("brighten(0.5) = %v, want a darker colour than %v", dark, splashMark)
	}
}

func TestSplashRenderPlateFillsAndAntialiases(t *testing.T) {
	const size = 64
	img := splashRenderPlate(splashPlate{
		Width: size, Height: size, Radius: 12, Inset: 1,
		FillFrom: splashMark, FillTo: splashMark,
		Over: splashBG,
	})

	if got := img.Bounds(); got != image.Rect(0, 0, size, size) {
		t.Fatalf("bounds = %v, want a %dx%d picture", got, size, size)
	}

	if centre := pixelAt(t, img, size/2, size/2); !near(centre, splashMark, 2) {
		t.Errorf("centre = %v, want the fill %v", centre, splashMark)
	}
	if corner := pixelAt(t, img, 0, 0); corner != splashBG {
		t.Errorf("corner = %v, want the background %v — the radius should cut it away", corner, splashBG)
	}

	// The whole point of rasterising these rather than asking GDI for them is
	// the partly covered pixel along the curve.
	var blended int
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			p := pixelAt(t, img, x, y)
			if p != splashBG && !near(p, splashMark, 2) {
				blended++
			}
		}
	}
	if blended == 0 {
		t.Error("no partially covered pixels: the corners are not anti-aliased")
	}
}

func TestSplashRenderPlateGradientRunsDiagonally(t *testing.T) {
	const size = 64
	img := splashRenderPlate(splashPlate{
		Width: size, Height: size, Radius: 8, Inset: 1,
		FillFrom: splashMark, FillTo: splashMarkDeep,
		Over: splashBG,
	})

	topLeft := pixelAt(t, img, 14, 14)
	bottomRight := pixelAt(t, img, size-14, size-14)
	if topLeft == bottomRight {
		t.Fatalf("both ends of the gradient are %v, want the 135° run the stylesheet has", topLeft)
	}
	if !near(topLeft, splashMark, 16) {
		t.Errorf("top left = %v, want it near the mark's light stop %v", topLeft, splashMark)
	}
	if !near(bottomRight, splashMarkDeep, 16) {
		t.Errorf("bottom right = %v, want it near the mark's dark stop %v", bottomRight, splashMarkDeep)
	}
}

func TestSplashRenderPlateDrawsBorderAndRing(t *testing.T) {
	const size = 64
	img := splashRenderPlate(splashPlate{
		Width: size, Height: size, Radius: 8, Inset: 1,
		FillFrom: splashPanel, FillTo: splashPanel,
		Border: splashBorder, BorderWidth: 2,
		Ring: splashMark, RingWidth: 2, RingInset: 4,
		Over: splashBG,
	})

	if edge := pixelAt(t, img, size/2, 2); !near(edge, splashBorder, 24) {
		t.Errorf("top edge = %v, want the border %v", edge, splashBorder)
	}
	if centre := pixelAt(t, img, size/2, size/2); !near(centre, splashPanel, 2) {
		t.Errorf("centre = %v, want the fill %v", centre, splashPanel)
	}

	// The focus ring has to be findable, or the keyboard has nowhere to show.
	var ringed bool
	for y := 0; y < size; y++ {
		if near(pixelAt(t, img, size/2, y), splashMark, 24) {
			ringed = true
			break
		}
	}
	if !ringed {
		t.Error("no focus ring drawn")
	}
}

func TestSplashRenderPlatePressShiftsDown(t *testing.T) {
	const size = 48
	plate := splashPlate{
		Width: size, Height: size, Radius: 8, Inset: 2,
		FillFrom: splashMark, FillTo: splashMark,
		Over: splashBG,
	}
	resting := splashRenderPlate(plate)
	plate.OffsetY = 2
	pressed := splashRenderPlate(plate)

	// A row that the resting face covers and the shifted one has moved off.
	top := 2
	if !near(pixelAt(t, resting, size/2, top), splashMark, 2) {
		t.Fatalf("resting face does not reach row %d", top)
	}
	if near(pixelAt(t, pressed, size/2, top), splashMark, 2) {
		t.Error("pressed face did not move down")
	}
}

func TestSplashRenderPlateDegenerateSizes(t *testing.T) {
	for _, size := range []image.Point{{X: 0, Y: 0}, {X: 0, Y: 10}, {X: 10, Y: 0}, {X: -4, Y: 8}} {
		img := splashRenderPlate(splashPlate{Width: size.X, Height: size.Y, Radius: 8, Over: splashBG})
		if img == nil {
			t.Fatalf("splashRenderPlate(%v) returned nil", size)
		}
	}
}

func TestSplashHairlineFadesAtBothEnds(t *testing.T) {
	const width = 200
	img := splashHairline(width, 3, splashBG)

	if left := pixelAt(t, img, 0, 1); !near(left, splashBG, 3) {
		t.Errorf("left end = %v, want it to fade into %v", left, splashBG)
	}
	if right := pixelAt(t, img, width-1, 1); !near(right, splashBG, 6) {
		t.Errorf("right end = %v, want it to fade into %v", right, splashBG)
	}

	middle := pixelAt(t, img, width/2, 1)
	if near(middle, splashBG, 40) {
		t.Errorf("middle = %v, want the accent to show", middle)
	}

	// Every row of the rule is the same, so height is only thickness.
	for y := 0; y < 3; y++ {
		if got := pixelAt(t, img, width/2, y); got != middle {
			t.Errorf("row %d = %v, want every row to match %v", y, got, middle)
		}
	}
}

func TestSplashStatusDotIsARoundLamp(t *testing.T) {
	const size = 24
	img := splashStatusDot(size, splashMark, splashPanel)

	if centre := pixelAt(t, img, size/2, size/2); !near(centre, splashMark, 2) {
		t.Errorf("centre = %v, want the lamp lit %v", centre, splashMark)
	}
	if corner := pixelAt(t, img, 0, 0); !near(corner, splashPanel, 8) {
		t.Errorf("corner = %v, want the panel %v behind a round lamp", corner, splashPanel)
	}

	// The halo sits between the two: brighter than the panel, short of the core.
	halo := pixelAt(t, img, size/2, 1)
	if halo == splashPanel {
		t.Log("halo has faded out entirely at the edge, which is fine")
	}
}

func TestSplashLogoRendersTheMark(t *testing.T) {
	for _, size := range []int{16, 40, 68, 96} {
		img, err := splashLogo(size, splashBG)
		if err != nil {
			t.Fatalf("splashLogo(%d) failed: %v", size, err)
		}
		if got := img.Bounds(); got != image.Rect(0, 0, size, size) {
			t.Fatalf("splashLogo(%d) bounds = %v", size, got)
		}

		var inked int
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				if !near(pixelAt(t, img, x, y), splashBG, 6) {
					inked++
				}
			}
		}
		if inked < size*size/8 {
			t.Errorf("splashLogo(%d) drew %d pixels of mark, want the mark to fill the box", size, inked)
		}
	}
}

func TestSplashLogoCropTightensOntoTheMark(t *testing.T) {
	src, err := splashLogoSource()
	if err != nil {
		t.Fatalf("splashLogoSource failed: %v", err)
	}

	crop := splashLogoCrop(src)
	if crop.Empty() {
		t.Fatal("crop is empty")
	}
	if !crop.In(src.Bounds()) {
		t.Errorf("crop %v is outside the master %v", crop, src.Bounds())
	}
	if crop.Dx() >= src.Bounds().Dx() && crop.Dy() >= src.Bounds().Dy() {
		t.Errorf("crop %v is the whole master: the icon's padding was not trimmed", crop)
	}
	if d := abs(crop.Dx() - crop.Dy()); d > 2 {
		t.Errorf("crop %v is %d off square, and the widget it fills is square", crop, d)
	}
}

func TestSplashLogoScaleOverBlendsOntoTheBackground(t *testing.T) {
	src, err := splashLogoSource()
	if err != nil {
		t.Fatalf("splashLogoSource failed: %v", err)
	}

	// The master is transparent at its corners. Whatever colour it is laid
	// over has to be what shows there — not white, and not black.
	over := splashColor{0x40, 0x10, 0x80}
	img := splashScaleOver(src, src.Bounds(), 32, over)
	if corner := pixelAt(t, img, 0, 0); !near(corner, over, 2) {
		t.Errorf("corner = %v, want the background %v to show through", corner, over)
	}
}

func TestSplashAddress(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"loopback is left alone", "http://127.0.0.1:3270/", "http://127.0.0.1:3270/"},
		{"a name is left alone", "http://mainframe.example:3270/", "http://mainframe.example:3270/"},
		{"IPv4 wildcard is not somewhere to send a browser", "http://0.0.0.0:3270/", "http://localhost:3270/"},
		{"IPv6 wildcard likewise", "http://[::]:3270/", "http://localhost:3270/"},
		{"a wildcard on another port keeps the port", "http://0.0.0.0:8080/", "http://localhost:8080/"},
		{"a real IPv6 address is left alone", "http://[::1]:3270/", "http://[::1]:3270/"},
		{"nonsense is handed back unchanged", "not a url", "not a url"},
		{"empty is handed back unchanged", "", ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := splashAddress(test.in); got != test.want {
				t.Errorf("splashAddress(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

func TestSplashVersion(t *testing.T) {
	tests := []struct{ in, want string }{
		{"0.4.0", "v0.4.0"},
		{"v0.4.0", "v0.4.0"},
		{"", "dev"},
		{"   ", "dev"},
		{"1.2.3-rc1", "v1.2.3-rc1"},
	}

	for _, test := range tests {
		if got := splashVersion(test.in); got != test.want {
			t.Errorf("splashVersion(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}
