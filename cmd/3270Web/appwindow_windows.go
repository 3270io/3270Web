//go:build windows

package main

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sync"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

// The window somebody gets when they double-click 3270Web.exe.
//
// It is the first thing the product shows on Windows, and for a while it was
// the only part of it that did not look like the rest: system-grey buttons on
// a near-black panel, next to a browser UI that had been given a mark, a
// palette and a card layout. So it is dressed the same way now — the card, the
// rule along the top edge and the button treatment the sign-in page has, the
// mark the title bar and the taskbar show, and the brand's own greens rather
// than the terminal's phosphor, which is the wrong green to sit beside the
// mark. splashart.go says where each colour comes from.
//
// Windows has no theming hook for any of that: a push button is drawn by the
// system, and GDI has no anti-aliasing, so a rounded corner drawn with
// RoundRect comes out stepped. Everything with a curve in it is therefore
// rasterised in splashart.go, which is ordinary Go image code, and this file
// blits the result. What it costs is that the buttons are custom widgets and
// have to earn back what a real button gives away for free: they are tab
// stops, they say what they are to a screen reader, they take Space and
// Return, and they show where the keyboard is.

// wsTabStop is WS_TABSTOP, which the dialog manager looks for when Tab moves
// the focus. Custom widgets do not carry it by default, and without it this
// window could only be worked with a mouse.
const wsTabStop = 0x00010000

// Sizes are in 1/96" units — what walk calls device independent pixels — so
// the window keeps its proportions on a scaled display.
const (
	splashWindowWidth  = 680
	splashWindowHeight = 360
	splashMinWidth     = 600
	splashMinHeight    = 340

	splashRuleHeight = 3

	splashMarginX      = 28
	splashMarginTop    = 26
	splashMarginBottom = 24

	splashLogoBox   = 68
	splashHeaderGap = 18
	splashAfterHead = 20

	splashCardPadX = 17
	splashCardPadY = 15
	splashCardGap  = 8

	splashDotBox = 16

	splashPrimaryWidth   = 176
	splashSecondaryWidth = 116
	splashButtonHeight   = 42
	splashButtonGap      = 12

	splashButtonRadius = 8
)

// splashPressOffset is how far a pressed button moves, the same one pixel
// `.auth-submit:active` translates by.
const splashPressOffset = 1

// The window's copy. The hint is here rather than inline because it is the
// one thing the window has to say that is not obvious from looking at it:
// this is the server, not a launcher for one, and closing it takes the
// sessions with it.
const (
	splashTagline = "Browser-based IBM 3270 terminal"
	splashHint    = "Leave this window open — closing it stops the server."

	// GDI cannot letter-space text, and the stylesheet's small uppercase
	// labels are spaced (0.13em). Spelling the gaps is the whole trick.
	splashStatusLabel = "R U N N I N G"
	splashStatusName  = "Running"
)

func runAppWindow(rawURL string, onExit func()) {
	address := splashAddress(rawURL)

	chrome, err := newSplashChrome()
	if err != nil {
		showFatalError(fmt.Sprintf("Failed to create app window. %v", err))
		return
	}
	defer chrome.dispose()

	var once sync.Once
	var mw *walk.MainWindow

	onClose := func() {
		once.Do(func() {
			if onExit != nil {
				onExit()
			}
		})
	}

	var appIcon *walk.Icon
	if icon, err := loadAppIconFromResource(); err == nil {
		appIcon = icon
	} else if icon, err := loadAppIconFromFile(); err == nil {
		appIcon = icon
	}

	rule := &splashPicture{
		fallback: chrome.bgFill,
		render: func(width, height int) *image.RGBA {
			return splashHairline(width, height, splashBG)
		},
	}
	logo := &splashPicture{
		fallback: chrome.bgFill,
		render: func(width, height int) *image.RGBA {
			side := width
			if height < side {
				side = height
			}
			img, err := splashLogo(side, splashBG)
			if err != nil {
				return nil
			}
			return img
		},
	}
	dot := &splashPicture{
		fallback: chrome.panelFill,
		render: func(width, height int) *image.RGBA {
			side := width
			if height < side {
				side = height
			}
			return splashStatusDot(side, splashMark, splashPanel)
		},
	}

	openButton := &splashButton{
		chrome:  chrome,
		text:    "Open 3270Web",
		primary: true,
		action:  func() { openBrowser(address) },
	}
	exitButton := &splashButton{
		chrome: chrome,
		text:   "Exit",
		action: func() {
			onClose()
			if mw != nil {
				mw.Close()
			}
		},
	}

	ui := &splashUI{
		pictures: []*splashPicture{rule, logo, dot},
		buttons:  []*splashButton{openButton, exitButton},
	}
	defer ui.dispose()

	mainWindow := MainWindow{
		AssignTo:   &mw,
		Title:      "3270Web",
		MinSize:    Size{Width: splashMinWidth, Height: splashMinHeight},
		Size:       Size{Width: splashWindowWidth, Height: splashWindowHeight},
		Background: chrome.background,
		Layout:     VBox{MarginsZero: true, Spacing: 0},
		OnMouseMove: func(int, int, walk.MouseButton) {
			ui.clearHover(nil)
		},
		Children: []Widget{
			// The rule along the top edge, drawn the way every card in the
			// browser UI is topped — see splashHairline.
			CustomWidget{
				AssignTo:            &rule.widget,
				MinSize:             Size{Height: splashRuleHeight},
				MaxSize:             Size{Height: splashRuleHeight},
				Background:          chrome.background,
				PaintMode:           PaintNoErase,
				PaintPixels:         rule.paint,
				InvalidatesOnResize: true,
			},
			Composite{
				Background: chrome.background,
				Layout: VBox{
					Margins: Margins{Left: splashMarginX, Top: splashMarginTop, Right: splashMarginX, Bottom: splashMarginBottom},
					Spacing: 0,
				},
				OnMouseMove: func(int, int, walk.MouseButton) {
					ui.clearHover(nil)
				},
				Children: []Widget{
					// Mark, wordmark, and one line saying what this is.
					Composite{
						Background: chrome.background,
						MaxSize:    Size{Height: splashLogoBox},
						Layout:     HBox{MarginsZero: true, Spacing: splashHeaderGap},
						Children: []Widget{
							CustomWidget{
								AssignTo:            &logo.widget,
								MinSize:             Size{Width: splashLogoBox, Height: splashLogoBox},
								MaxSize:             Size{Width: splashLogoBox, Height: splashLogoBox},
								Background:          chrome.background,
								PaintMode:           PaintNoErase,
								PaintPixels:         logo.paint,
								InvalidatesOnResize: true,
								Accessibility: Accessibility{
									Role: AccRoleGraphic,
									Name: "3270Web",
								},
							},
							Composite{
								Background: chrome.background,
								Layout:     VBox{MarginsZero: true, Spacing: 2},
								Children: []Widget{
									VSpacer{},
									// The wordmark is split the way the brand
									// lockup splits it: the number in text, the
									// word in the mark's own green.
									Composite{
										Background: chrome.background,
										Layout:     HBox{MarginsZero: true, Spacing: 0},
										Children: []Widget{
											Label{
												Text:      "3270",
												TextColor: chrome.ink,
												Font:      Font{Family: splashSans, PointSize: 18, Bold: true},
											},
											Label{
												Text:      "Web",
												TextColor: chrome.brand,
												Font:      Font{Family: splashSans, PointSize: 18, Bold: true},
											},
											HSpacer{},
										},
									},
									Label{
										Text:      splashTagline,
										TextColor: chrome.muted,
										Font:      Font{Family: splashSans, PointSize: 9},
									},
									VSpacer{},
								},
							},
							HSpacer{},
						},
					},
					VSpacer{Size: splashAfterHead},
					splashCard(chrome, dot, address),
					VSpacer{},
					// Actions, with the version stamp trailing them.
					Composite{
						Background: chrome.background,
						MaxSize:    Size{Height: splashButtonHeight},
						Layout:     HBox{MarginsZero: true, Spacing: splashButtonGap},
						OnMouseMove: func(int, int, walk.MouseButton) {
							ui.clearHover(nil)
						},
						Children: []Widget{
							openButton.widget(chrome, splashPrimaryWidth, ui),
							exitButton.widget(chrome, splashSecondaryWidth, ui),
							HSpacer{},
							Label{
								Text:      splashVersion(appVersion),
								TextColor: chrome.dim,
								Font:      Font{Family: splashMono, PointSize: 9},
								Alignment: AlignHFarVCenter,
							},
						},
					},
				},
			},
		},
	}
	if appIcon != nil {
		mainWindow.Icon = appIcon
	}
	if err := mainWindow.Create(); err != nil {
		showFatalError(fmt.Sprintf("Failed to create app window. %v", err))
		return
	}
	if mw == nil {
		showFatalError("Failed to create app window.")
		return
	}
	if appIcon != nil {
		_ = mw.SetIcon(appIcon)
	}

	// A pointer that leaves through the title bar or off the edge of the
	// screen never crosses a container on the way out, so the hover it left
	// behind is cleared when the window stops being the active one.
	mw.Deactivating().Attach(func() { ui.clearHover(nil) })

	for _, button := range ui.buttons {
		button.watchFocus()
	}

	mw.Show()

	// The primary action is where the keyboard starts, so Space or Return
	// opens the terminal without anybody having to reach for Tab first.
	if openButton.custom != nil {
		_ = openButton.custom.SetFocus()
	}

	mw.Run()
}

// splashCard is the panel that reports what the server is doing: a lamp, the
// address, and what closing the window would mean. The border is a composite
// one pixel larger than the panel inside it, which is how a container gets a
// hairline border when the toolkit has no such property.
func splashCard(chrome *splashChrome, dot *splashPicture, address string) Widget {
	return Composite{
		Background: chrome.border,
		Layout:     VBox{Margins: Margins{Left: 1, Top: 1, Right: 1, Bottom: 1}, Spacing: 0},
		Children: []Widget{
			Composite{
				Background: chrome.panel,
				Layout: VBox{
					Margins: Margins{Left: splashCardPadX, Top: splashCardPadY, Right: splashCardPadX, Bottom: splashCardPadY},
					Spacing: splashCardGap,
				},
				Children: []Widget{
					Composite{
						Background: chrome.panel,
						MaxSize:    Size{Height: splashDotBox},
						Layout:     HBox{MarginsZero: true, Spacing: 7},
						Children: []Widget{
							CustomWidget{
								AssignTo:            &dot.widget,
								MinSize:             Size{Width: splashDotBox, Height: splashDotBox},
								MaxSize:             Size{Width: splashDotBox, Height: splashDotBox},
								Background:          chrome.panel,
								PaintMode:           PaintNoErase,
								PaintPixels:         dot.paint,
								InvalidatesOnResize: true,
								Accessibility: Accessibility{
									Role: AccRoleGraphic,
									Name: splashStatusName,
								},
							},
							Label{
								Text:      splashStatusLabel,
								TextColor: chrome.brand,
								Font:      Font{Family: splashSans, PointSize: 8, Bold: true},
								Alignment: AlignHNearVCenter,
								Accessibility: Accessibility{
									Name: splashStatusName,
								},
							},
							HSpacer{},
						},
					},
					Label{
						Text:      address,
						TextColor: chrome.ink,
						Font:      Font{Family: splashMono, PointSize: 12},
					},
					Label{
						Text:      splashHint,
						TextColor: chrome.muted,
						Font:      Font{Family: splashSans, PointSize: 9},
					},
				},
			},
		},
	}
}

// splashUI is the part of the window its handlers need to reach: the widgets
// that hold a pointer state between them, and the bitmaps to hand back when
// it closes.
type splashUI struct {
	pictures []*splashPicture
	buttons  []*splashButton
}

// clearHover drops the pointer state everywhere except on except. Windows
// only tells a window the pointer arrived, never that it left, so leaving is
// inferred from it turning up somewhere else.
func (ui *splashUI) clearHover(except *splashButton) {
	for _, button := range ui.buttons {
		if button == except {
			continue
		}
		button.setPointer(false, false)
	}
}

func (ui *splashUI) dispose() {
	for _, picture := range ui.pictures {
		picture.dispose()
	}
	for _, button := range ui.buttons {
		button.dispose()
	}
}

// Segoe UI is the shell font on every Windows this runs on, and it is what
// "Inter, Segoe UI, system-ui" resolves to in the browser. Consolas stands in
// for the stylesheet's --mono and carries the same job here: prose in the
// shell font, addresses in mono.
const (
	splashSans = "Segoe UI"
	splashMono = "Consolas"
)

// splashChrome owns the brushes, fonts and colours the window is drawn with.
// GDI objects are handles, so they are made once and given back together.
type splashChrome struct {
	background SolidColorBrush
	panel      SolidColorBrush
	border     SolidColorBrush

	ink   walk.Color
	muted walk.Color
	dim   walk.Color
	brand walk.Color

	button *walk.Font

	// Flat brushes for the one case a rendered picture could not be turned
	// into a bitmap: better a plain shape than a hole in the window.
	bgFill        *walk.SolidColorBrush
	panelFill     *walk.SolidColorBrush
	primaryFill   *walk.SolidColorBrush
	secondaryFill *walk.SolidColorBrush
}

func newSplashChrome() (*splashChrome, error) {
	buttonFont, err := walk.NewFont(splashSans, 10, walk.FontBold)
	if err != nil {
		return nil, err
	}

	bgFill, err := walk.NewSolidColorBrush(walkColor(splashBG))
	if err != nil {
		return nil, err
	}
	panelFill, err := walk.NewSolidColorBrush(walkColor(splashPanel))
	if err != nil {
		return nil, err
	}
	primaryFill, err := walk.NewSolidColorBrush(walkColor(splashMark))
	if err != nil {
		return nil, err
	}
	secondaryFill, err := walk.NewSolidColorBrush(walkColor(splashPanel2))
	if err != nil {
		return nil, err
	}

	return &splashChrome{
		background:    SolidColorBrush{Color: walkColor(splashBG)},
		panel:         SolidColorBrush{Color: walkColor(splashPanel)},
		border:        SolidColorBrush{Color: walkColor(splashBorder)},
		ink:           walkColor(splashInk),
		muted:         walkColor(splashMuted),
		dim:           walkColor(splashDim),
		brand:         walkColor(splashMark),
		button:        buttonFont,
		bgFill:        bgFill,
		panelFill:     panelFill,
		primaryFill:   primaryFill,
		secondaryFill: secondaryFill,
	}, nil
}

func (c *splashChrome) dispose() {
	if c == nil {
		return
	}
	if c.button != nil {
		c.button.Dispose()
	}
	for _, brush := range []*walk.SolidColorBrush{c.bgFill, c.panelFill, c.primaryFill, c.secondaryFill} {
		if brush != nil {
			brush.Dispose()
		}
	}
}

func walkColor(c splashColor) walk.Color {
	return walk.RGB(c.R, c.G, c.B)
}

// splashPicture is a widget showing one rendered picture, kept as a bitmap
// until its size or the display's scaling changes.
type splashPicture struct {
	widget   *walk.CustomWidget
	render   func(width, height int) *image.RGBA
	fallback walk.Brush

	cached *walk.Bitmap
	size   walk.Size
}

func (p *splashPicture) paint(canvas *walk.Canvas, _ walk.Rectangle) error {
	bounds := p.widget.ClientBoundsPixels()
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return nil
	}

	if p.cached == nil || p.size.Width != bounds.Width || p.size.Height != bounds.Height {
		p.dispose()
		if img := p.render(bounds.Width, bounds.Height); img != nil {
			if bitmap, err := walk.NewBitmapFromImageForDPI(img, p.widget.DPI()); err == nil {
				p.cached = bitmap
				p.size = walk.Size{Width: bounds.Width, Height: bounds.Height}
			}
		}
	}

	// Nothing erases behind this widget, so it always paints all of itself.
	if p.cached == nil {
		return canvas.FillRectanglePixels(p.fallback, bounds)
	}
	return canvas.DrawImagePixels(p.cached, walk.Point{})
}

func (p *splashPicture) dispose() {
	if p.cached != nil {
		p.cached.Dispose()
		p.cached = nil
		p.size = walk.Size{}
	}
}

// splashFace names one drawn state of a button. Size is part of the name
// because a display's scaling can change while the window is open, and the
// face for the old scaling is the wrong picture rather than a stale one.
type splashFace struct {
	width, height int
	hovered       bool
	pressed       bool
	focused       bool
}

type splashButton struct {
	chrome  *splashChrome
	text    string
	primary bool
	action  func()

	custom *walk.CustomWidget

	// armed is a left button held down on this widget; hovered is the
	// pointer being over it. A button is drawn pressed only while both hold,
	// which is what makes dragging off a held button let it back up without
	// letting go of the press — walk takes the mouse capture on the way
	// down, so the moves and the release arrive here either way.
	armed   bool
	hovered bool

	faces map[splashFace]*walk.Bitmap
}

// widget builds the declarative widget for this button and wires its pointer,
// keyboard and focus handling to it.
func (b *splashButton) widget(chrome *splashChrome, width int, ui *splashUI) Widget {
	return CustomWidget{
		AssignTo:            &b.custom,
		MinSize:             Size{Width: width, Height: splashButtonHeight},
		MaxSize:             Size{Width: width, Height: splashButtonHeight},
		Background:          chrome.background,
		PaintMode:           PaintNoErase,
		PaintPixels:         b.paint,
		InvalidatesOnResize: true,
		Style:               wsTabStop,
		Accessibility: Accessibility{
			Role:          AccRolePushbutton,
			Name:          b.text,
			DefaultAction: "Press",
		},
		OnMouseMove: func(x, y int, _ walk.MouseButton) {
			ui.clearHover(b)
			b.setPointer(b.contains(x, y), b.armed)
		},
		OnMouseDown: func(x, y int, button walk.MouseButton) {
			if button&walk.LeftButton == 0 {
				return
			}
			if b.custom != nil {
				_ = b.custom.SetFocus()
			}
			b.setPointer(b.contains(x, y), true)
		},
		OnMouseUp: func(x, y int, button walk.MouseButton) {
			if button&walk.LeftButton == 0 {
				return
			}
			armed, inside := b.armed, b.contains(x, y)
			b.setPointer(inside, false)
			// A press let go somewhere else is a cancelled press, which is
			// what every other button on the system does.
			if armed && inside {
				b.activate()
			}
		},
		OnKeyDown: func(key walk.Key) {
			if key == walk.KeySpace || key == walk.KeyReturn {
				b.activate()
			}
		},
	}
}

// watchFocus repaints the button when it gains or loses the keyboard, which
// is the only way the focus ring can appear and go away again.
func (b *splashButton) watchFocus() {
	if b.custom == nil {
		return
	}
	b.custom.FocusedChanged().Attach(func() {
		b.custom.Invalidate()
	})
}

func (b *splashButton) contains(x, y int) bool {
	if b.custom == nil {
		return false
	}
	bounds := b.custom.ClientBoundsPixels()
	return x >= 0 && y >= 0 && x < bounds.Width && y < bounds.Height
}

func (b *splashButton) setPointer(hovered, armed bool) {
	if b.hovered == hovered && b.armed == armed {
		return
	}
	b.hovered, b.armed = hovered, armed
	if b.custom != nil {
		b.custom.Invalidate()
	}
}

func (b *splashButton) activate() {
	if b.action != nil {
		b.action()
	}
}

func (b *splashButton) paint(canvas *walk.Canvas, _ walk.Rectangle) error {
	if b.custom == nil {
		return nil
	}
	bounds := b.custom.ClientBoundsPixels()
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return nil
	}

	face := splashFace{
		width:   bounds.Width,
		height:  bounds.Height,
		hovered: b.hovered,
		pressed: b.armed && b.hovered,
		focused: b.custom.Focused(),
	}

	bitmap, drawn := b.faces[face]
	if !drawn {
		if made, err := walk.NewBitmapFromImageForDPI(splashRenderPlate(b.plate(face)), b.custom.DPI()); err == nil {
			bitmap = made
		}
		if b.faces == nil {
			b.faces = make(map[splashFace]*walk.Bitmap)
		}
		// A failure is remembered too, so a window that cannot make bitmaps
		// falls back once rather than on every paint.
		b.faces[face] = bitmap
	}

	if bitmap != nil {
		if err := canvas.DrawImagePixels(bitmap, walk.Point{}); err != nil {
			return err
		}
	} else if err := canvas.FillRectanglePixels(b.fallbackBrush(), bounds); err != nil {
		return err
	}

	label := bounds
	if face.pressed {
		label.Y += splashScale(splashPressOffset, b.custom.DPI())
	}
	return canvas.DrawTextPixels(b.text, b.chrome.button, b.labelColor(), label,
		walk.TextCenter|walk.TextVCenter|walk.TextSingleLine)
}

// plate describes how this button is drawn in the given state. The primary
// button is the sign-in page's submit button: an accent gradient with the
// background colour as its text. The secondary is the panel with a border,
// which is what a quiet control looks like everywhere else in the product.
func (b *splashButton) plate(face splashFace) splashPlate {
	dpi := 96
	if b.custom != nil {
		dpi = b.custom.DPI()
	}

	plate := splashPlate{
		Width:  face.width,
		Height: face.height,
		Radius: float64(splashScale(splashButtonRadius, dpi)),
		Inset:  float64(splashScale(1, dpi)),
		Over:   splashBG,
	}

	if b.primary {
		// The mark's own face gradient, top-left to bottom-right, which is
		// the same run and the same two greens the icon is drawn with.
		plate.FillFrom, plate.FillTo = splashMark, splashMarkDeep
	} else {
		plate.FillFrom, plate.FillTo = splashPanel, splashPanel2
		plate.Border = splashBorder
		plate.BorderWidth = float64(splashScale(1, dpi))
	}

	switch {
	case face.pressed:
		// `filter: brightness()` on the way down, and the same one pixel
		// `.auth-submit:active` moves by.
		plate.FillFrom = plate.FillFrom.brighten(0.9)
		plate.FillTo = plate.FillTo.brighten(0.9)
		plate.OffsetY = float64(splashScale(splashPressOffset, dpi))
	case face.hovered:
		plate.FillFrom = plate.FillFrom.brighten(1.12)
		plate.FillTo = plate.FillTo.brighten(1.12)
		if !b.primary {
			plate.Border = splashMark
		}
	}

	if face.focused {
		// Where the keyboard is has to carry against both fills, and the
		// brand's lighter mint is the one colour that reads on the emerald
		// and on the panel alike.
		plate.Ring = splashGlow
		plate.RingWidth = float64(splashScale(2, dpi))
		plate.RingInset = float64(splashScale(3, dpi))
	}

	return plate
}

func (b *splashButton) labelColor() walk.Color {
	if b.primary {
		return walkColor(splashOnMark)
	}
	return b.chrome.ink
}

func (b *splashButton) fallbackBrush() walk.Brush {
	if b.primary {
		return b.chrome.primaryFill
	}
	return b.chrome.secondaryFill
}

func (b *splashButton) dispose() {
	for _, bitmap := range b.faces {
		if bitmap != nil {
			bitmap.Dispose()
		}
	}
	b.faces = nil
}

// splashScale converts a 1/96" measurement to device pixels, never rounding a
// visible line away to nothing.
func splashScale(value, dpi int) int {
	scaled := value * dpi / 96
	if value > 0 && scaled < 1 {
		return 1
	}
	return scaled
}

func loadAppIconFromFile() (*walk.Icon, error) {
	exePath, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exePath)
		candidates := []string{
			filepath.Join(exeDir, "3270Web_logo.png"),
			filepath.Join(exeDir, "static", "3270Web_logo.png"),
			filepath.Join(exeDir, "web", "static", "3270Web_logo.png"),
			filepath.Join(exeDir, "..", "web", "static", "3270Web_logo.png"),
		}
		for _, candidate := range candidates {
			if _, statErr := os.Stat(candidate); statErr == nil {
				return walk.NewIconFromFile(candidate)
			}
		}
	}

	return walk.NewIconFromFile(filepath.FromSlash("web/static/3270Web_logo.png"))
}

func loadAppIconFromResource() (*walk.Icon, error) {
	if icon, err := walk.NewIconFromResource("APPICON"); err == nil {
		return icon, nil
	}
	return walk.NewIconFromResourceId(1)
}
