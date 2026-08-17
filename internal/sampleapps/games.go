// SPDX-License-Identifier: AGPL-3.0-or-later

package sampleapps

import (
	"fmt"
	"strings"

	"github.com/racingmars/go3270"
)

// The chrome and the geometry the four game samples share.
//
// A 3270 terminal is a block mode device: the host writes a screen, the
// keyboard locks, and nothing comes back until the operator presses an AID
// key. There is no key-at-a-time input to read and no way to redraw on a
// timer, so none of these can be the real time game of the same name. Each is
// instead what the device can actually do — a guess or a move per
// transmission for the word game and for noughts and crosses, one tick of the
// world per key press for the snake and the bat and ball, with a typed move
// string for when you want several ticks at once.
//
// They are here because they are small, self-contained screens that lean on
// the parts of the datastream the other samples touch lightly: colour and
// extended highlighting per character cell, field validation, PF key
// handling, and a screen that changes completely on every transmission. They
// also give somebody evaluating the terminal something to do with it that is
// over in a minute.
//
// All four draw on the default 24x80 buffer whatever the terminal negotiated,
// so a board is the same everywhere and the games have no opinion about
// models. The pet store is the sample that demonstrates model handling.

const (
	gameRows = 24
	gameCols = 80

	// The rows every game shares: a banner and a rule, then the body, then a
	// rule, the message line, a line for typed input, and the key legend.
	gameRuleRow    = 1
	gameBodyTop    = 2
	gameBodyBottom = 19
	gameSepRow     = 20
	gameMsgRow     = 21
	gameEntryRow   = 22
	gameKeyRow     = 23

	// gameRuleWidth stops the horizontal rules a few columns short of the
	// edge, so a rule never ends in the cell where something else would have
	// to put its attribute byte.
	gameRuleWidth = gameCols - 4
)

type gameChrome struct {
	id    string
	title string

	// right is the score, drawn along the right hand end of the banner. It
	// gives way to the title rather than overwriting it.
	right string

	keys   string
	msg    string
	msgErr bool
}

// gameFrame wraps a body in the banner, message line and key legend.
func gameFrame(c gameChrome, body go3270.Screen) go3270.Screen {
	screen := make(go3270.Screen, 0, len(body)+8)

	title := gameTrunc(c.title, 40)
	titleCol := (gameCols-len(title))/2 - 1
	if leftmost := len(c.id) + 2; titleCol < leftmost {
		titleCol = leftmost
	}
	screen = append(screen,
		go3270.Field{Row: 0, Col: 0, Color: go3270.Blue, Content: c.id},
		go3270.Field{Row: 0, Col: titleCol, Intense: true, Color: go3270.White, Content: title},
	)
	if room := gameCols - 2 - (titleCol + len(title)); c.right != "" && room >= 8 {
		right := gameTrunc(c.right, room)
		screen = append(screen, go3270.Field{Row: 0, Col: gameCols - 1 - len(right),
			Color: go3270.Turquoise, Content: right})
	}
	screen = append(screen, go3270.Field{Row: gameRuleRow, Col: 0, Color: go3270.Blue,
		Content: strings.Repeat("-", gameRuleWidth)})

	screen = append(screen, body...)

	msgColor := go3270.Green
	if c.msgErr {
		msgColor = go3270.Red
	}
	screen = append(screen,
		go3270.Field{Row: gameSepRow, Col: 0, Color: go3270.Blue,
			Content: strings.Repeat("-", gameRuleWidth)},
		go3270.Field{Row: gameMsgRow, Col: 0, Intense: true, Color: msgColor,
			Content: gameTrunc(c.msg, gameCols-2)},
		go3270.Field{Row: gameKeyRow, Col: 0, Color: go3270.Turquoise,
			Content: gameTrunc(c.keys, gameCols-2)},
	)
	return screen
}

// gameEntry is the typed input line above the key legend, and returns the
// column the cursor belongs in.
func gameEntry(label string, width int, name, value, hint string) (go3270.Screen, int) {
	attrCol := len(label) + 1
	screen := go3270.Screen{
		{Row: gameEntryRow, Col: 0, Color: go3270.White, Content: label},
		{Row: gameEntryRow, Col: attrCol, Name: name, Write: true,
			Highlighting: go3270.Underscore, Content: gameTrunc(value, width)},
		{Row: gameEntryRow, Col: attrCol + width + 1, Autoskip: true},
	}
	if hintCol := attrCol + width + 3; hint != "" && hintCol < gameCols-4 {
		screen = append(screen, go3270.Field{Row: gameEntryRow, Col: hintCol,
			Color: go3270.Blue, Content: gameTrunc(hint, gameCols-2-hintCol)})
	}
	return screen, attrCol + 1
}

// gameHelpBody lays out a help screen as one line per row of the body.
func gameHelpBody(lines []string) go3270.Screen {
	screen := make(go3270.Screen, 0, len(lines))
	for i, line := range lines {
		row := gameBodyTop + i
		if row > gameBodyBottom {
			break
		}
		if line == "" {
			continue
		}
		color := go3270.Green
		if strings.HasSuffix(line, ":") {
			color = go3270.White
		}
		screen = append(screen, go3270.Field{Row: row, Col: 0, Color: color,
			Content: gameTrunc(line, gameCols-2)})
	}
	return screen
}

func gameTrunc(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if len(text) <= width {
		return text
	}
	return text[:width]
}

func gamePlural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// gameCodepage is the code page the client negotiated, or nil for the
// package default when nothing was negotiated at all.
func gameCodepage(dev go3270.DevInfo) go3270.Codepage {
	if dev == nil {
		return nil
	}
	return dev.Codepage()
}

// The playfield the snake and the bat and ball share.
//
// A character cell is about twice as tall as it is wide, so one cell of the
// world is drawn two columns wide to come out roughly square. The left column
// of each pair is deliberately left empty: it is where the field attribute
// byte goes when that cell needs a colour of its own, which is what lets any
// cell be drawn independently of its neighbours without the two fighting over
// a column.
const (
	courtWidth  = 36
	courtHeight = 16

	courtTopRow  = gameBodyTop
	courtLeftCol = 0
)

func courtRow(y int) int     { return courtTopRow + 1 + y }
func courtBottomRow() int    { return courtRow(courtHeight) }
func courtAttrCol(x int) int { return courtLeftCol + 2 + 2*x }

func courtBorder(color go3270.Color) go3270.Screen {
	bar := strings.Repeat("=", courtAttrCol(courtWidth)+1)
	screen := make(go3270.Screen, 0, 2+courtHeight*2)
	screen = append(screen,
		go3270.Field{Row: courtTopRow, Col: courtLeftCol, Color: color, Content: bar},
		go3270.Field{Row: courtBottomRow(), Col: courtLeftCol, Color: color, Content: bar},
	)
	for y := 0; y < courtHeight; y++ {
		screen = append(screen,
			go3270.Field{Row: courtRow(y), Col: courtLeftCol, Color: color, Content: "|"},
			go3270.Field{Row: courtRow(y), Col: courtAttrCol(courtWidth), Color: color, Content: "|"},
		)
	}
	return screen
}

// courtPainter draws cells into the playfield, and refuses to draw two in the
// same place.
//
// Two things wanting the same cell is not a bug to be prevented upstream —
// the instant a snake runs into itself the head and a body segment are on the
// same cell, and that is the whole point of the move. But two fields on one
// cell is a datastream a terminal draws wrong, so whatever was painted first
// keeps the cell. Paint in the order the player should see: the thing that
// just happened, then the scenery.
type courtPainter struct {
	fields go3270.Screen
	taken  map[int]bool
}

func newCourtPainter() *courtPainter {
	return &courtPainter{taken: map[int]bool{}}
}

func (p *courtPainter) paint(x, y int, glyph string, color go3270.Color) {
	if x < 0 || x >= courtWidth || y < 0 || y >= courtHeight {
		return
	}
	key := y*courtWidth + x
	if p.taken[key] {
		return
	}
	p.taken[key] = true
	p.fields = append(p.fields, go3270.Field{Row: courtRow(y), Col: courtAttrCol(x),
		Color: color, Intense: true, Content: glyph})
}

func (p *courtPainter) screen() go3270.Screen { return p.fields }

// parseGameMoves turns a typed move string into the ticks it stands for.
//
// This is what makes a turn per transmission playable. A terminal that sends
// one transmission per key press would otherwise cost a round trip per step,
// so "RRDD" is four ticks, and so is "2R2D".
func parseGameMoves(text, allowed string, max int) ([]byte, error) {
	moves := make([]byte, 0, max)
	count := 0
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if ch == ' ' {
			continue
		}
		if ch >= '0' && ch <= '9' {
			count = count*10 + int(ch-'0')
			if count > max {
				count = max
			}
			continue
		}
		dir := gameDirection(ch)
		if dir == 0 || !strings.ContainsRune(allowed, rune(dir)) {
			return nil, fmt.Errorf("%q is not a move here - use %s", string(ch), gameMoveHint(allowed))
		}
		if count == 0 {
			count = 1
		}
		for n := 0; n < count && len(moves) < max; n++ {
			moves = append(moves, dir)
		}
		count = 0
	}
	return moves, nil
}

func gameDirection(ch byte) byte {
	switch ch {
	case 'u', 'U':
		return 'U'
	case 'd', 'D':
		return 'D'
	case 'l', 'L':
		return 'L'
	case 'r', 'R':
		return 'R'
	case '.':
		return '.'
	}
	return 0
}

func gameMoveHint(allowed string) string {
	return strings.Join(strings.Split(allowed, ""), " ")
}
