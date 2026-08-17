// SPDX-License-Identifier: AGPL-3.0-or-later

package sampleapps

import (
	"fmt"
	"math/rand"
	"net"
	"strings"
	"time"

	"github.com/racingmars/go3270"
)

// The snake: eat, grow, and do not run into anything.
//
// The real game runs on a timer, and a 3270 terminal has no timer to run on:
// the host cannot redraw until the operator transmits. So the world here
// advances one tick per transmission. A PF key turns the snake and takes a
// step, and the entry line takes a whole run of moves — "RRDD", or "3R2D" —
// so a turn can be several ticks without several round trips.
//
// That makes it a chess clock version of the game rather than an arcade one,
// and it is the honest translation. It also makes it a good thing to point
// chaos exploration or a workflow recording at: every transmission redraws
// most of the screen, and the board is built from up to a couple of hundred
// individually coloured single character fields.

type snakePage int

const (
	snakePageGame snakePage = iota
	snakePageHelp
)

const (
	snakeStartLength = 4
	snakeGrowth      = 3
	snakeFoodScore   = 10
	snakeMaxMoves    = 20
)

type snakePoint struct{ x, y int }

type snakeSession struct {
	rng *rand.Rand

	page snakePage

	// body is the snake, head first.
	body []snakePoint
	dir  snakePoint
	grow int
	food snakePoint

	alive bool
	full  bool
	wrap  bool

	score, best, ticks int

	msg    string
	msgErr bool
}

func handleSnake(conn net.Conn) {
	defer conn.Close()

	dev, err := go3270.NegotiateTelnet(conn)
	if err != nil {
		return
	}

	s := newSnakeSession(time.Now().UnixNano())
	for {
		screen, row, col := s.render()
		resp, err := go3270.ShowScreenOpts(screen, nil, conn, go3270.ScreenOpts{
			CursorRow: row, CursorCol: col, Codepage: gameCodepage(dev),
		})
		if err != nil {
			return
		}
		if s.dispatch(resp) {
			return
		}
	}
}

func newSnakeSession(seed int64) *snakeSession {
	s := &snakeSession{rng: rand.New(rand.NewSource(seed))}
	s.newGame()
	return s
}

func (s *snakeSession) newGame() {
	head := snakePoint{x: courtWidth / 4, y: courtHeight / 2}
	s.body = make([]snakePoint, 0, snakeStartLength)
	for i := 0; i < snakeStartLength; i++ {
		s.body = append(s.body, snakePoint{x: head.x - i, y: head.y})
	}
	s.dir = snakePoint{x: 1}
	s.grow = 0
	s.alive = true
	s.full = false
	s.score = 0
	s.ticks = 0
	s.placeFood()
	s.note("Type a run of moves, or turn with a PF key. U D L R.", false)
}

func (s *snakeSession) dispatch(resp go3270.Response) bool {
	switch resp.AID {
	case go3270.AIDPF3:
		if s.page == snakePageHelp {
			s.page = snakePageGame
			return false
		}
		return true
	case go3270.AIDPF1:
		s.page = snakePageHelp
		return false
	case go3270.AIDPF5:
		s.page = snakePageGame
		s.newGame()
		return false
	case go3270.AIDPF6:
		s.page = snakePageGame
		s.wrap = !s.wrap
		s.note(fmt.Sprintf("The walls are now %s.", s.wallRule()), false)
		return false
	case go3270.AIDPF7:
		s.step('U')
		return false
	case go3270.AIDPF8:
		s.step('D')
		return false
	case go3270.AIDPF10:
		s.step('L')
		return false
	case go3270.AIDPF11:
		s.step('R')
		return false
	case go3270.AIDEnter:
		if s.page == snakePageHelp {
			s.page = snakePageGame
			return false
		}
		s.run(resp.Values["move"])
		return false
	}
	s.page = snakePageGame
	s.note(fmt.Sprintf("%s does nothing here. PF1 lists the keys that do.",
		go3270.AIDtoString(resp.AID)), true)
	return false
}

// run plays out a typed move string. An empty one is a single tick in the
// direction the snake is already going, which is what makes Enter on its own
// the "carry on" key.
func (s *snakeSession) run(text string) {
	if !s.playable() {
		return
	}
	moves, err := parseGameMoves(strings.TrimSpace(text), "UDLR.", snakeMaxMoves)
	if err != nil {
		s.note(err.Error(), true)
		return
	}
	if len(moves) == 0 {
		moves = []byte{'.'}
	}
	eaten := s.score
	for _, move := range moves {
		if !s.playable() {
			return
		}
		s.step(move)
	}
	if s.alive && !s.full {
		s.note(fmt.Sprintf("%d %s. %d eaten. Length %d.", len(moves),
			gamePlural(len(moves), "tick", "ticks"),
			(s.score-eaten)/snakeFoodScore, len(s.body)), false)
	}
}

func (s *snakeSession) step(move byte) {
	if !s.playable() {
		return
	}
	s.page = snakePageGame
	s.turn(move)
	s.advance()
}

// playable reports whether the game will take a move, and says why not if it
// will not.
func (s *snakeSession) playable() bool {
	switch {
	case !s.alive:
		s.note("The game is over. PF5 starts another one.", true)
		return false
	case s.full:
		s.note("There is nowhere left to go. PF5 starts another one.", false)
		return false
	}
	return true
}

// turn changes direction, except into the snake's own neck: a snake that
// reverses onto itself would die on the spot, which is a rule of the game
// rather than an accident of this one.
func (s *snakeSession) turn(move byte) {
	var dir snakePoint
	switch move {
	case 'U':
		dir = snakePoint{y: -1}
	case 'D':
		dir = snakePoint{y: 1}
	case 'L':
		dir = snakePoint{x: -1}
	case 'R':
		dir = snakePoint{x: 1}
	default:
		return
	}
	if len(s.body) > 1 && dir.x == -s.dir.x && dir.y == -s.dir.y {
		return
	}
	s.dir = dir
}

func (s *snakeSession) advance() {
	head := s.body[0]
	next := snakePoint{x: head.x + s.dir.x, y: head.y + s.dir.y}
	if s.wrap {
		next.x = (next.x + courtWidth) % courtWidth
		next.y = (next.y + courtHeight) % courtHeight
	} else if next.x < 0 || next.x >= courtWidth || next.y < 0 || next.y >= courtHeight {
		s.die("Into the wall.")
		return
	}

	// The tail cell is free by the time the head reaches it, unless the
	// snake is still growing into it.
	occupied := len(s.body)
	if s.grow == 0 {
		occupied--
	}
	for i := 0; i < occupied; i++ {
		if s.body[i] == next {
			s.die("Into itself.")
			return
		}
	}

	s.body = append(s.body, snakePoint{})
	copy(s.body[1:], s.body[:len(s.body)-1])
	s.body[0] = next
	if s.grow > 0 {
		s.grow--
	} else {
		s.body = s.body[:len(s.body)-1]
	}
	s.ticks++

	if next == s.food {
		s.score += snakeFoodScore
		s.grow += snakeGrowth
		s.placeFood()
	}
}

func (s *snakeSession) die(how string) {
	s.alive = false
	if s.score > s.best {
		s.best = s.score
	}
	s.note(fmt.Sprintf("%s Score %d after %d ticks. PF5 for another game.",
		how, s.score, s.ticks), true)
}

// placeFood drops the next piece somewhere the snake is not. A board with no
// free cell left is the only way to win this game outright.
func (s *snakeSession) placeFood() {
	free := make([]snakePoint, 0, courtWidth*courtHeight)
	taken := make(map[snakePoint]bool, len(s.body))
	for _, cell := range s.body {
		taken[cell] = true
	}
	for y := 0; y < courtHeight; y++ {
		for x := 0; x < courtWidth; x++ {
			cell := snakePoint{x: x, y: y}
			if !taken[cell] {
				free = append(free, cell)
			}
		}
	}
	if len(free) == 0 {
		s.full = true
		if s.score > s.best {
			s.best = s.score
		}
		s.note(fmt.Sprintf("The board is full at %d. Nothing else to eat.", s.score), false)
		return
	}
	s.food = free[s.rng.Intn(len(free))]
}

func (s *snakeSession) wallRule() string {
	if s.wrap {
		return "OPEN"
	}
	return "SOLID"
}

func (s *snakeSession) note(msg string, isErr bool) {
	s.msg, s.msgErr = msg, isErr
}

func (s *snakeSession) render() (go3270.Screen, int, int) {
	if s.page == snakePageHelp {
		return gameFrame(gameChrome{
			id:    "SNK020",
			title: "SNAKE - HOW TO PLAY",
			keys:  "PF3=Back Enter=Back",
			msg:   "PF3 returns to the game.",
		}, gameHelpBody(snakeHelpLines)), gameKeyRow, 0
	}

	painter := newCourtPainter()
	painter.paint(s.body[0].x, s.body[0].y, "@", go3270.Green)
	for _, cell := range s.body[1:] {
		painter.paint(cell.x, cell.y, "o", go3270.Green)
	}
	if !s.full {
		painter.paint(s.food.x, s.food.y, "*", go3270.Red)
	}

	body := append(courtBorder(go3270.Blue), painter.screen()...)
	entry, cursorCol := gameEntry("MOVE ===>", 12, "move", "", "U D L R, e.g. RRDD or 3R2D")
	body = append(body, entry...)

	screen := gameFrame(gameChrome{
		id:    "SNK010",
		title: "SNAKE",
		right: fmt.Sprintf("SCORE %d  BEST %d  LENGTH %d", s.score, s.best, len(s.body)),
		keys: fmt.Sprintf("Enter=Play PF1=Help PF3=Exit PF5=New PF6=Walls %s "+
			"PF7/8=Up/Dn PF10/11=L/R", s.wallRule()),
		msg:    s.msg,
		msgErr: s.msgErr,
	}, body)
	return screen, gameEntryRow, cursorCol
}

var snakeHelpLines = []string{
	"THE GAME:",
	"  Eat the food, grow by three cells each time, and do not run into the",
	"  walls or into yourself. There is no clock: the world advances one tick",
	"  for every move you send, and waits for you in between.",
	"",
	"MOVING:",
	"  PF7 PF8 PF10 PF11 turn up, down, left and right and take one tick. The",
	"  entry line takes a run of moves and plays them in order:",
	"",
	"    RRDD     right, right, down, down",
	"    3R2D     the same thing, counted",
	"    .        carry on in the direction you are already going",
	"",
	"  Enter on an empty line is a single tick. A snake will not turn back on",
	"  itself, so a move into your own neck is ignored rather than fatal.",
	"",
	"KEYS:",
	"  PF3 exit   PF5 new game   PF6 solid or open walls   PF1 this screen",
}
