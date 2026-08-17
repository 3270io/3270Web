// SPDX-License-Identifier: AGPL-3.0-or-later

package sampleapps

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/racingmars/go3270"
)

type app3Page int

const (
	app3PageMenu app3Page = iota
	app3PageInputs
	app3PageColors
	app3PageHighlights
	app3PageAttrOps
	app3PagePFKeys
	app3PageResults
	app3PageAltScreen
)

type app3State struct {
	page    app3Page
	values  map[string]string
	lastAID string
	status  string
	devinfo go3270.DevInfo
}

func handleApp3(conn net.Conn) {
	defer conn.Close()

	devinfo, err := go3270.NegotiateTelnet(conn)
	if err != nil {
		return
	}

	state := &app3State{
		page:    app3PageMenu,
		values:  map[string]string{},
		lastAID: "[none]",
		status:  "PF8 Next | PF7/PF4 Prev | PF3 Exit",
		devinfo: devinfo,
	}

	for {
		screen, row, col := state.buildScreen()
		var resp go3270.Response
		if state.page == app3PageAltScreen && state.devinfo != nil {
			resp, err = go3270.ShowScreenOpts(screen, state.values, conn, go3270.ScreenOpts{
				AltScreen: state.devinfo,
				Codepage:  state.devinfo.Codepage(),
				CursorRow: row,
				CursorCol: col,
			})
		} else {
			resp, err = go3270.ShowScreen(screen, state.values, row, col, conn)
		}
		if err != nil {
			return
		}
		state.lastAID = go3270.AIDtoString(resp.AID)
		for k, v := range resp.Values {
			state.values[k] = v
		}
		if state.handleAID(resp) {
			return
		}
	}
}

func (s *app3State) handleAID(resp go3270.Response) bool {
	aid := resp.AID
	if s.page == app3PagePFKeys {
		switch aid {
		case go3270.AIDPF3:
			return true
		case go3270.AIDEnter:
			s.status = fmt.Sprintf("PF Test captured %s. Use PF12 to return to menu.", s.lastAID)
			return false
		case go3270.AIDPF12:
			s.page = app3PageMenu
			s.status = fmt.Sprintf("Captured %s on PF test screen; returned to menu.", s.lastAID)
			return false
		default:
			s.status = fmt.Sprintf("PF Test captured AID: %s", s.lastAID)
			return false
		}
	}

	switch aid {
	case go3270.AIDPF3:
		return true
	case go3270.AIDPF1, go3270.AIDPF12:
		s.page = app3PageMenu
		s.status = fmt.Sprintf("Navigated to menu via %s.", s.lastAID)
	case go3270.AIDPF2:
		s.page = app3PageInputs
		s.status = "Input field matrix screen."
	case go3270.AIDPF5:
		s.page = app3PageColors
		s.status = "Color matrix screen (protected + input fields)."
	case go3270.AIDPF6:
		s.page = app3PageHighlights
		s.status = "Highlight matrix screen."
	case go3270.AIDPF9:
		s.page = app3PagePFKeys
		s.status = "PF/PA key capture screen. Press any PF/PA/Clear/Enter."
	case go3270.AIDPF10:
		s.page = app3PageAttrOps
		s.status = "AttributeOnly and PositionOnly demo screen."
	case go3270.AIDPF11:
		s.page = app3PageResults
		s.status = "Results / field echo screen."
	case go3270.AIDPF13:
		s.page = app3PageAltScreen
		s.status = "Alternate-screen demo (ScreenOpts.AltScreen)."
	case go3270.AIDPF4, go3270.AIDPF7:
		s.page = app3PrevPage(s.page)
		s.status = fmt.Sprintf("Moved to previous page via %s.", s.lastAID)
	case go3270.AIDPF8:
		s.page = app3NextPage(s.page)
		s.status = "Moved to next page via PF8."
	case go3270.AIDClear:
		s.clearEditableValues()
		s.status = "Cleared sample input values."
	case go3270.AIDEnter:
		s.handleEnter()
	default:
		s.status = fmt.Sprintf("Captured AID: %s", s.lastAID)
	}
	return false
}

func (s *app3State) handleEnter() {
	switch s.page {
	case app3PageMenu:
		choice := strings.TrimSpace(s.values["menuChoice"])
		if choice == "" {
			s.status = "Enter a screen number (1-7) then press Enter."
			return
		}
		n, err := strconv.Atoi(choice)
		if err != nil || n < 1 || n > 7 {
			s.status = fmt.Sprintf("Invalid menu choice %q. Use 1-7.", choice)
			return
		}
		switch n {
		case 1:
			s.page = app3PageInputs
		case 2:
			s.page = app3PageColors
		case 3:
			s.page = app3PageHighlights
		case 4:
			s.page = app3PageAttrOps
		case 5:
			s.page = app3PagePFKeys
		case 6:
			s.page = app3PageResults
		case 7:
			s.page = app3PageAltScreen
		}
		s.status = fmt.Sprintf("Opened screen %d via Enter.", n)
	case app3PageInputs:
		s.status = "Input fields captured. PF11 shows echoed values."
	case app3PageResults:
		s.status = "Results refreshed."
	default:
		s.status = fmt.Sprintf("Captured AID: %s", s.lastAID)
	}
}

func (s *app3State) clearEditableValues() {
	for _, key := range []string{
		"menuChoice", "alphaIn", "numIn", "passIn", "intenseIn", "blueIn", "redIn",
		"keepSpacesIn", "autoInput", "underscoreIn", "blinkIn", "reverseIn", "normalHiIn",
	} {
		s.values[key] = ""
	}
}

func app3PrevPage(p app3Page) app3Page {
	order := []app3Page{
		app3PageMenu, app3PageInputs, app3PageColors, app3PageHighlights, app3PageAttrOps, app3PagePFKeys, app3PageResults, app3PageAltScreen,
	}
	for i, v := range order {
		if v == p {
			if i == 0 {
				return order[len(order)-1]
			}
			return order[i-1]
		}
	}
	return app3PageMenu
}

func app3NextPage(p app3Page) app3Page {
	order := []app3Page{
		app3PageMenu, app3PageInputs, app3PageColors, app3PageHighlights, app3PageAttrOps, app3PagePFKeys, app3PageResults, app3PageAltScreen,
	}
	for i, v := range order {
		if v == p {
			return order[(i+1)%len(order)]
		}
	}
	return app3PageMenu
}

func (s *app3State) buildScreen() (go3270.Screen, int, int) {
	switch s.page {
	case app3PageInputs:
		return s.buildInputsScreen(), 4, 17
	case app3PageColors:
		return s.buildColorMatrixScreen(), 4, 33
	case app3PageHighlights:
		return s.buildHighlightMatrixScreen(), 4, 38
	case app3PageAttrOps:
		return s.buildAttrOpsScreen(), 0, 0
	case app3PagePFKeys:
		return s.buildPFKeyScreen(), 0, 0
	case app3PageResults:
		return s.buildResultsScreen(), 0, 0
	case app3PageAltScreen:
		return s.buildAltScreenDemo(), 0, 0
	default:
		return s.buildMenuScreen(), 15, 16
	}
}

func (s *app3State) buildMenuScreen() go3270.Screen {
	screen := go3270.Screen{
		{Row: 0, Col: 16, Intense: true, Content: "3270 BMS Attribute Test Matrix (Sample App 3)"},
		{Row: 2, Col: 0, Content: "This sample covers all field attributes exposed by go3270 used by 3270Web rendering and chaos tests."},
		{Row: 4, Col: 0, Content: "1) Input Field Matrix (write/protected/numeric/hidden/intense/KeepSpaces/autoskip)"},
		{Row: 5, Col: 0, Content: "2) Color Matrix (all 3270 colors on read-only and input fields)"},
		{Row: 6, Col: 0, Content: "3) Highlight Matrix (default/blink/reverse/underscore on read-only and input fields)"},
		{Row: 7, Col: 0, Content: "4) AttributeOnly + PositionOnly demo (inline SA attributes and SBA-only positioning)"},
		{Row: 8, Col: 0, Content: "5) PF/PA/Clear/Enter capture screen (PF1-PF24 visible and testable)"},
		{Row: 9, Col: 0, Content: "6) Results screen (echo values, lengths, KeepSpaces behavior, last AID)"},
		{Row: 10, Col: 0, Content: "7) Alternate Screen demo (ScreenOpts.AltScreen / terminal alternate dimensions)"},
		{Row: 12, Col: 0, Color: go3270.Turquoise, Content: "PF2 Inputs | PF5 Colors | PF6 Highlights | PF9 Key Test | PF10 Attr | PF11 Results | PF13 Alt"},
		{Row: 13, Col: 0, Color: go3270.Yellow, Content: "PF1/PF12 Menu | PF7/PF4 Prev | PF8 Next | PF3 Exit | Clear resets editable sample values"},
		{Row: 15, Col: 0, Content: "Menu Choice (1-7):"},
		{Row: 15, Col: 16, Name: "menuChoice", Write: true, Highlighting: go3270.Underscore},
		{Row: 15, Col: 19, Autoskip: true},
	}
	return app3FinalizeScreen(screen, "Menu", s.lastAID, s.status)
}

func (s *app3State) buildInputsScreen() go3270.Screen {
	screen := go3270.Screen{
		{Row: 0, Col: 24, Intense: true, Content: "Input Field Matrix"},
		{Row: 2, Col: 0, Content: "Label"},
		{Row: 2, Col: 16, Content: "Field"},
		{Row: 2, Col: 42, Content: "Notes"},

		{Row: 4, Col: 0, Content: "Alpha input"},
		{Row: 4, Col: 16, Name: "alphaIn", Write: true, Highlighting: go3270.Underscore},
		{Row: 4, Col: 34, Autoskip: true},
		{Row: 4, Col: 42, Content: "Writable, underscore"},

		{Row: 5, Col: 0, Content: "Numeric-only"},
		{Row: 5, Col: 16, Name: "numIn", Write: true, NumericOnly: true, Highlighting: go3270.Underscore},
		{Row: 5, Col: 28, Autoskip: true},
		{Row: 5, Col: 42, Content: "NumericOnly=true"},

		{Row: 6, Col: 0, Content: "Hidden/password"},
		{Row: 6, Col: 16, Name: "passIn", Write: true, Hidden: true},
		{Row: 6, Col: 30, Autoskip: true},
		{Row: 6, Col: 42, Content: "Hidden=true"},

		{Row: 7, Col: 0, Content: "Intense input"},
		{Row: 7, Col: 16, Name: "intenseIn", Write: true, Intense: true, Highlighting: go3270.Underscore},
		{Row: 7, Col: 34, Autoskip: true},
		{Row: 7, Col: 42, Content: "Intense + writable"},

		{Row: 8, Col: 0, Content: "Blue input"},
		{Row: 8, Col: 16, Name: "blueIn", Write: true, Color: go3270.Blue, Highlighting: go3270.Underscore},
		{Row: 8, Col: 34, Autoskip: true},
		{Row: 8, Col: 42, Content: "Color on input field"},

		{Row: 9, Col: 0, Content: "Red input"},
		{Row: 9, Col: 16, Name: "redIn", Write: true, Color: go3270.Red, Highlighting: go3270.Underscore},
		{Row: 9, Col: 34, Autoskip: true},
		{Row: 9, Col: 42, Content: "Color on input field"},

		{Row: 10, Col: 0, Content: "KeepSpaces"},
		{Row: 10, Col: 16, Name: "keepSpacesIn", Write: true, Highlighting: go3270.Underscore, KeepSpaces: true},
		{Row: 10, Col: 34, Autoskip: true},
		{Row: 10, Col: 42, Content: "KeepSpaces=true (see PF11)"},

		{Row: 12, Col: 0, Content: "Protected normal"},
		{Row: 12, Col: 16, Name: "roNormal", Content: "READONLY-NORMAL"},
		{Row: 12, Col: 42, Content: "Protected/read-only"},

		{Row: 13, Col: 0, Content: "Protected intense"},
		{Row: 13, Col: 16, Intense: true, Name: "roIntense", Content: "READONLY-INTENSE"},
		{Row: 13, Col: 42, Content: "Protected + Intense"},

		{Row: 14, Col: 0, Content: "Protected hidden"},
		{Row: 14, Col: 16, Hidden: true, Name: "roHidden", Content: "SECRET-TEXT"},
		{Row: 14, Col: 42, Content: "Protected + Hidden"},

		{Row: 15, Col: 0, Content: "Autoskip protected"},
		{Row: 15, Col: 16, Content: "AUTOSKIP-BLOCK", Autoskip: true},
		{Row: 15, Col: 30, Content: "<- protected field with Autoskip=true"},

		{Row: 17, Col: 0, Content: "Autoskip jump test"},
		{Row: 17, Col: 16, Name: "autoInput", Write: true, Highlighting: go3270.Underscore},
		{Row: 17, Col: 24, Autoskip: true},
		{Row: 17, Col: 26, Content: "TAB/field navigation should skip protected autoskip fields"},
	}
	return app3FinalizeScreen(screen, "Inputs", s.lastAID, s.status)
}

func (s *app3State) buildColorMatrixScreen() go3270.Screen {
	type colorRow struct {
		label string
		color go3270.Color
		name  string
	}
	rows := []colorRow{
		{"Default", go3270.DefaultColor, "default"},
		{"Blue", go3270.Blue, "blue"},
		{"Red", go3270.Red, "red"},
		{"Pink", go3270.Pink, "pink"},
		{"Green", go3270.Green, "green"},
		{"Turquoise", go3270.Turquoise, "turq"},
		{"Yellow", go3270.Yellow, "yellow"},
		{"White", go3270.White, "white"},
	}

	screen := go3270.Screen{
		{Row: 0, Col: 24, Intense: true, Content: "Color Matrix"},
		{Row: 2, Col: 0, Content: "Color"},
		{Row: 2, Col: 15, Content: "Protected (RO)"},
		{Row: 2, Col: 40, Content: "Input (RW)"},
		{Row: 2, Col: 62, Content: "Notes"},
	}
	for i, r := range rows {
		y := 4 + i
		screen = append(screen,
			go3270.Field{Row: y, Col: 0, Content: r.label},
			go3270.Field{Row: y, Col: 15, Content: "READONLY", Color: r.color},
			go3270.Field{Row: y, Col: 40, Name: "c_" + r.name, Write: true, Color: r.color, Highlighting: go3270.Underscore},
			go3270.Field{Row: y, Col: 52, Autoskip: true},
		)
	}
	screen = append(screen,
		go3270.Field{Row: 14, Col: 0, Color: go3270.White, Highlighting: go3270.ReverseVideo, Content: "White + Reverse Video (visibility check across themes)"},
		go3270.Field{Row: 16, Col: 0, Content: "Use this page to validate renderer color mapping for read-only and writable fields."},
	)
	return app3FinalizeScreen(screen, "Colors", s.lastAID, s.status)
}

func (s *app3State) buildHighlightMatrixScreen() go3270.Screen {
	screen := go3270.Screen{
		{Row: 0, Col: 21, Intense: true, Content: "Highlight Matrix"},
		{Row: 2, Col: 0, Content: "Highlight"},
		{Row: 2, Col: 16, Content: "Protected (RO)"},
		{Row: 2, Col: 42, Content: "Input (RW)"},

		{Row: 4, Col: 0, Content: "Default"},
		{Row: 4, Col: 16, Content: "DEFAULT"},
		{Row: 4, Col: 42, Name: "normalHiIn", Write: true},
		{Row: 4, Col: 56, Autoskip: true},

		{Row: 5, Col: 0, Content: "Underscore"},
		{Row: 5, Col: 16, Content: "UNDERSCORE", Highlighting: go3270.Underscore},
		{Row: 5, Col: 42, Name: "underscoreIn", Write: true, Highlighting: go3270.Underscore},
		{Row: 5, Col: 56, Autoskip: true},

		{Row: 6, Col: 0, Content: "ReverseVideo"},
		{Row: 6, Col: 16, Content: "REVERSE", Highlighting: go3270.ReverseVideo, Color: go3270.Turquoise},
		{Row: 6, Col: 42, Name: "reverseIn", Write: true, Highlighting: go3270.ReverseVideo, Color: go3270.Yellow},
		{Row: 6, Col: 56, Autoskip: true},

		{Row: 7, Col: 0, Content: "Blink"},
		{Row: 7, Col: 16, Content: "BLINK", Highlighting: go3270.Blink, Color: go3270.Pink},
		{Row: 7, Col: 42, Name: "blinkIn", Write: true, Highlighting: go3270.Blink, Color: go3270.Green},
		{Row: 7, Col: 56, Autoskip: true},

		{Row: 9, Col: 0, Content: "Combo checks"},
		{Row: 10, Col: 4, Content: "Intense + underscore", Intense: true, Highlighting: go3270.Underscore},
		{Row: 11, Col: 4, Content: "Color + reverse", Color: go3270.Blue, Highlighting: go3270.ReverseVideo},
		{Row: 12, Col: 4, Content: "Color + blink", Color: go3270.Red, Highlighting: go3270.Blink},
		{Row: 13, Col: 4, Content: "Input underline visibility test ->"},
		{Row: 13, Col: 39, Name: "underlineCheck", Write: true, Highlighting: go3270.Underscore, Color: go3270.White},
		{Row: 13, Col: 55, Autoskip: true},
	}
	return app3FinalizeScreen(screen, "Highlights", s.lastAID, s.status)
}

func (s *app3State) buildAttrOpsScreen() go3270.Screen {
	screen := go3270.Screen{
		{Row: 0, Col: 12, Intense: true, Content: "AttributeOnly + PositionOnly / SBA Demo"},
		{Row: 2, Col: 0, Content: "AttributeOnly applies color/highlight without starting a new field (SA command):"},
		{Row: 4, Col: 2, Content: "Normal "},
		{Row: 4, Col: 9, AttributeOnly: true, Color: go3270.Yellow, Highlighting: go3270.Underscore},
		{Row: 4, Col: 9, PositionOnly: true, Content: "ATTR-ONLY UNDERLINE YELLOW "},
		{Row: 4, Col: 35, AttributeOnly: true, Color: go3270.DefaultColor, Highlighting: go3270.DefaultHighlight},
		{Row: 4, Col: 35, PositionOnly: true, Content: "Back to default"},

		{Row: 6, Col: 0, Content: "PositionOnly moves to exact coordinates without creating a field attribute byte:"},
		{Row: 8, Col: 2, Content: "0123456789.123456789.123456789.123456789.123456789.123456789.123456789"},
		{Row: 9, Col: 2, Content: "......................................................................"},
		{Row: 9, Col: 12, PositionOnly: true, Content: "^ marker at col 12 via PositionOnly"},
		{Row: 10, Col: 20, PositionOnly: true, Content: "^ marker at col 20 via PositionOnly"},
		{Row: 11, Col: 32, PositionOnly: true, Content: "^ marker at col 32 via PositionOnly"},

		{Row: 13, Col: 0, Content: "Mixed protected/read-only formatting on one line:"},
		{Row: 14, Col: 2, Content: "Default"},
		{Row: 14, Col: 10, Content: "Blue", Color: go3270.Blue},
		{Row: 14, Col: 16, Content: "Rev", Color: go3270.Turquoise, Highlighting: go3270.ReverseVideo},
		{Row: 14, Col: 21, Content: "Blink", Color: go3270.Pink, Highlighting: go3270.Blink},
		{Row: 14, Col: 28, Content: "Under", Color: go3270.Green, Highlighting: go3270.Underscore},

		{Row: 16, Col: 0, Content: "Writable field to compare inline SA styling with input-field styling:"},
		{Row: 17, Col: 2, Name: "attrOpsInput", Write: true, Color: go3270.Turquoise, Highlighting: go3270.Underscore},
		{Row: 17, Col: 28, Autoskip: true},
	}
	return app3FinalizeScreen(screen, "AttrOps", s.lastAID, s.status)
}

func (s *app3State) buildPFKeyScreen() go3270.Screen {
	screen := go3270.Screen{
		{Row: 0, Col: 18, Intense: true, Content: "PF / PA / AID Capture Test Screen"},
		{Row: 2, Col: 0, Content: "Press PF1-PF24, PA1-PA3, Enter, or Clear. Last captured AID is shown below."},
		{Row: 3, Col: 0, Color: go3270.Yellow, Content: "On this screen PF keys are captured/stay here (except PF3 exits, PF12 returns to menu)."},
		{Row: 5, Col: 0, Content: "Last AID:"},
		{Row: 5, Col: 10, Name: "lastAidEcho", Intense: true, Color: go3270.Turquoise, Content: s.lastAID},
		{Row: 5, Col: 35, Content: "Status:"},
		{Row: 5, Col: 43, Name: "pfStatusEcho", Content: s.status},
	}

	for i := 1; i <= 24; i++ {
		row := 7 + ((i - 1) / 4)
		col := ((i - 1) % 4) * 19
		screen = append(screen, go3270.Field{
			Row: row, Col: col, Content: fmt.Sprintf("PF%-2d  Test key capture", i),
		})
	}
	screen = append(screen,
		go3270.Field{Row: 14, Col: 0, Content: "PA1/PA2/PA3 also captured; Clear clears sample input values; Enter refreshes this page."},
		go3270.Field{Row: 16, Col: 0, Content: "Return to Menu (PF12):"},
		go3270.Field{Row: 16, Col: 21, Name: "pfMenuHint", Write: true, Highlighting: go3270.Underscore},
		go3270.Field{Row: 16, Col: 32, Autoskip: true},
	)
	return app3FinalizeScreen(screen, "PFKeys", s.lastAID, s.status)
}

func (s *app3State) buildResultsScreen() go3270.Screen {
	keep := s.values["keepSpacesIn"]
	pass := strings.TrimSpace(s.values["passIn"])
	lines := []string{
		fmt.Sprintf("alphaIn        = %q", s.values["alphaIn"]),
		fmt.Sprintf("numIn          = %q", s.values["numIn"]),
		fmt.Sprintf("passIn length  = %d", len(pass)),
		fmt.Sprintf("intenseIn      = %q", s.values["intenseIn"]),
		fmt.Sprintf("blueIn         = %q", s.values["blueIn"]),
		fmt.Sprintf("redIn          = %q", s.values["redIn"]),
		fmt.Sprintf("keepSpacesIn   = %q (len=%d)", keep, len(keep)),
		fmt.Sprintf("autoInput      = %q", s.values["autoInput"]),
		fmt.Sprintf("normalHiIn     = %q", s.values["normalHiIn"]),
		fmt.Sprintf("underscoreIn   = %q", s.values["underscoreIn"]),
		fmt.Sprintf("reverseIn      = %q", s.values["reverseIn"]),
		fmt.Sprintf("blinkIn        = %q", s.values["blinkIn"]),
		fmt.Sprintf("last AID       = %s", s.lastAID),
	}
	screen := go3270.Screen{
		{Row: 0, Col: 24, Intense: true, Content: "Results / Echo Screen"},
		{Row: 2, Col: 0, Content: "Values collected from the sample screens (PF11 opens this page from anywhere):"},
	}
	for i, line := range lines {
		if i >= 17 {
			break
		}
		screen = append(screen, go3270.Field{Row: 4 + i, Col: 0, Content: line})
	}
	screen = append(screen,
		go3270.Field{Row: 19, Col: 0, Color: go3270.Turquoise, Highlighting: go3270.ReverseVideo, Content: "KeepSpaces field preserves leading/trailing spaces only on the field where KeepSpaces=true."},
		go3270.Field{Row: 20, Col: 0, Content: "Use Clear on any screen to reset editable values."},
	)
	return app3FinalizeScreen(screen, "Results", s.lastAID, s.status)
}

func (s *app3State) buildAltScreenDemo() go3270.Screen {
	rows, cols := 24, 80
	termType := "unknown"
	if s.devinfo != nil {
		rows, cols = s.devinfo.AltDimensions()
		termType = s.devinfo.TerminalType()
	}
	if rows < 24 {
		rows = 24
	}
	if cols < 80 {
		cols = 80
	}
	hasLargerAlt := rows > 24 || cols > 80

	screen := make(go3270.Screen, 0, rows+20)
	bannerText := "ALT SCREEN ACTIVE: alternate size is larger than 24x80"
	bannerColor := go3270.Green
	if !hasLargerAlt {
		bannerText = "ALT SCREEN ACTIVE: client alternate size is 24x80 (same as default)"
		bannerColor = go3270.Yellow
	}
	screen = append(screen,
		go3270.Field{Row: 0, Col: 0, Intense: true, Content: trimTo("Alternate Screen Demo (ScreenOpts.AltScreen)", cols)},
		go3270.Field{Row: 1, Col: 0, Content: trimTo(fmt.Sprintf("TerminalType=%s | AltDimensions=%dx%d", termType, rows, cols), cols)},
		go3270.Field{Row: 2, Col: 0, Color: bannerColor, Highlighting: go3270.ReverseVideo, Content: trimTo(bannerText, cols)},
		go3270.Field{Row: 3, Col: 0, Color: go3270.Turquoise, Content: trimTo("This page is rendered using alternate screen mode when supported by the client.", cols)},
	)

	var ruler strings.Builder
	for i := 0; i < cols; i++ {
		ruler.WriteByte(byte('0' + (i % 10)))
	}
	screen = append(screen, go3270.Field{Row: 5, Col: 0, Content: ruler.String()})

	maxProbeRows := min(14, rows-11)
	for i := 0; i < maxProbeRows; i++ {
		y := 6 + i
		screen = append(screen, go3270.Field{Row: y, Col: 0, Content: fmt.Sprintf("R%02d", y)})
		x := min(cols-1, 6+(i*4))
		screen = append(screen, go3270.Field{Row: y, Col: x, PositionOnly: true, Content: "*"})
	}

	inputRow := max(8, rows-6)
	label := "Alt screen input:"
	inputCol := 0
	inputStart := inputCol + len(label) + 1
	inputEnd := min(cols-2, inputStart+24)
	if inputStart < cols-1 {
		screen = append(screen,
			go3270.Field{Row: inputRow, Col: inputCol, Content: trimTo(label, cols)},
			go3270.Field{Row: inputRow, Col: inputStart, Name: "altScreenInput", Write: true, Highlighting: go3270.Underscore, Color: go3270.Green},
			go3270.Field{Row: inputRow, Col: inputEnd, Autoskip: true},
		)
	}

	note := "Client reports alternate screen larger than 24x80."
	if rows == 24 && cols == 80 {
		note = "Client alternate screen is 24x80 (same as default). Demo still uses ScreenOpts.AltScreen."
	}
	screen = append(screen, go3270.Field{
		Row: max(7, inputRow+1), Col: 0, Content: trimTo(note, cols),
	})

	brText := fmt.Sprintf("Bottom-right marker (%d,%d)", rows-1, cols-1)
	brRow := rows - 3
	if brRow < 6 {
		brRow = 6
	}
	brCol := max(0, cols-len(brText)-1)
	screen = append(screen,
		go3270.Field{Row: brRow, Col: brCol, Color: go3270.Yellow, Highlighting: go3270.Underscore, Content: trimTo(brText, cols-brCol)},
		go3270.Field{Row: rows - 2, Col: max(0, cols-1), PositionOnly: true, Content: "+"},
		go3270.Field{Row: rows - 1, Col: 0, Content: trimTo("PF1/PF12 Menu PF4/PF7 Prev PF8 Next PF13 Alt PF3 Exit", cols)},
	)

	return screen
}

func app3FinalizeScreen(screen go3270.Screen, pageTitle, lastAID, status string) go3270.Screen {
	footer := []go3270.Field{
		{Row: 21, Col: 0, Color: go3270.Turquoise, Content: fmt.Sprintf("Page: %s", pageTitle)},
		{Row: 21, Col: 18, Content: fmt.Sprintf("Last AID: %s", lastAID)},
		{Row: 22, Col: 0, Content: "PF1/PF12 Menu PF2 Inputs PF5 Colors PF6 High PF9 Keys PF10 Attr PF11 Results PF13 Alt"},
		{Row: 23, Col: 0, Content: "PF4/PF7 Prev  PF8 Next  Enter Apply/Select  Clear Reset Inputs  PF3 Exit"},
	}
	if strings.TrimSpace(status) != "" {
		footer = append(footer, go3270.Field{
			Row: 20, Col: 0, Color: go3270.Yellow, Content: trimTo(status, 79),
		})
	}
	return append(screen, footer...)
}

func trimTo(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
