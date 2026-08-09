package sampleapps

import (
	"fmt"
	"math/rand"
	"net"
	"strings"
	"time"

	"github.com/racingmars/go3270"
)

// Bat and ball, played a tick at a time.
//
// Like the snake, the original runs on a clock the terminal does not have, so
// the ball moves one cell per transmission: PF7 and PF8 move the bat and take
// a tick with them, and the entry line takes a run of moves — "UUD", or "3U"
// — for when the ball is a long way off and you want to get there in one
// round trip. Enter on its own holds the bat still for a tick.
//
// The machine's bat is the interesting part to watch: at the easy levels it
// only tracks the ball when the ball is coming towards it, and hesitates for
// a tick at a time, which is the whole of the difficulty setting.

type pongPage int

const (
	pongPageGame pongPage = iota
	pongPageHelp
)

const (
	pongBatHeight = 4
	pongTarget    = 5
	pongMaxMoves  = 20
)

type pongPoint struct{ x, y int }

type pongLevel struct {
	name string
	// hesitate is the percentage of ticks the machine's bat sits out.
	hesitate int
	// blind machines only move while the ball is coming towards them.
	blind bool
}

var pongLevels = []pongLevel{
	{name: "EASY", hesitate: 55, blind: true},
	{name: "FAIR", hesitate: 25, blind: true},
	{name: "HARD", hesitate: 0, blind: false},
}

type pongSession struct {
	rng *rand.Rand

	page pongPage

	ball   pongPoint
	vx, vy int

	// you and machine are the top row of each bat.
	you, machine int

	yourScore, machineScore int
	rally, bestRally        int

	level int
	over  bool

	msg    string
	msgErr bool
}

func handlePong(conn net.Conn) {
	defer conn.Close()

	dev, err := go3270.NegotiateTelnet(conn)
	if err != nil {
		return
	}

	s := newPongSession(time.Now().UnixNano())
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

func newPongSession(seed int64) *pongSession {
	s := &pongSession{rng: rand.New(rand.NewSource(seed)), level: 1}
	s.newMatch()
	return s
}

func (s *pongSession) newMatch() {
	s.yourScore, s.machineScore = 0, 0
	s.rally = 0
	s.over = false
	s.you = (courtHeight - pongBatHeight) / 2
	s.machine = s.you
	s.serve(-1)
	s.note(fmt.Sprintf("First to %d. PF7 and PF8 move your bat and take a tick with them.",
		pongTarget), false)
}

// serve puts the ball back in the middle, travelling in the given direction:
// -1 towards your bat, +1 towards the machine's.
func (s *pongSession) serve(direction int) {
	s.ball = pongPoint{x: courtWidth / 2, y: courtHeight / 2}
	s.vx = direction
	s.vy = 1
	if s.rng.Intn(2) == 0 {
		s.vy = -1
	}
	s.rally = 0
}

func (s *pongSession) dispatch(resp go3270.Response) bool {
	switch resp.AID {
	case go3270.AIDPF3:
		if s.page == pongPageHelp {
			s.page = pongPageGame
			return false
		}
		return true
	case go3270.AIDPF1:
		s.page = pongPageHelp
		return false
	case go3270.AIDPF5:
		s.page = pongPageGame
		s.newMatch()
		return false
	case go3270.AIDPF6:
		s.page = pongPageGame
		s.level = (s.level + 1) % len(pongLevels)
		s.note(fmt.Sprintf("Level %s.", pongLevels[s.level].name), false)
		return false
	case go3270.AIDPF7:
		s.step('U')
		return false
	case go3270.AIDPF8:
		s.step('D')
		return false
	case go3270.AIDEnter:
		if s.page == pongPageHelp {
			s.page = pongPageGame
			return false
		}
		s.run(resp.Values["bat"])
		return false
	}
	s.page = pongPageGame
	s.note(fmt.Sprintf("%s does nothing here. PF1 lists the keys that do.",
		go3270.AIDtoString(resp.AID)), true)
	return false
}

func (s *pongSession) run(text string) {
	if s.over {
		s.note("The match is over. PF5 starts another one.", true)
		return
	}
	moves, err := parseGameMoves(strings.TrimSpace(text), "UD.", pongMaxMoves)
	if err != nil {
		s.note(err.Error(), true)
		return
	}
	if len(moves) == 0 {
		moves = []byte{'.'}
	}
	scored := s.yourScore + s.machineScore
	for _, move := range moves {
		if s.over {
			break
		}
		s.step(move)
	}
	if !s.over && s.yourScore+s.machineScore == scored {
		s.note(fmt.Sprintf("%d %s. Rally of %d.", len(moves),
			gamePlural(len(moves), "tick", "ticks"), s.rally), false)
	}
}

func (s *pongSession) step(move byte) {
	if s.over {
		s.note("The match is over. PF5 starts another one.", true)
		return
	}
	s.page = pongPageGame
	switch move {
	case 'U':
		s.moveBat(-1)
	case 'D':
		s.moveBat(1)
	}
	s.tick()
}

func (s *pongSession) moveBat(delta int) {
	s.you = pongClamp(s.you + delta)
}

func (s *pongSession) tick() {
	next := pongPoint{x: s.ball.x + s.vx, y: s.ball.y + s.vy}

	// The top and bottom of the court are walls.
	if next.y < 0 {
		next.y, s.vy = 0, 1
	} else if next.y >= courtHeight {
		next.y, s.vy = courtHeight-1, -1
	}

	switch {
	case next.x <= 0:
		if !pongCovers(s.you, next.y) {
			s.point(false)
			return
		}
		next.x, s.vx = 1, 1
		s.vy = pongDeflect(s.you, next.y)
		s.rally++
	case next.x >= courtWidth-1:
		if !pongCovers(s.machine, next.y) {
			s.point(true)
			return
		}
		next.x, s.vx = courtWidth-2, -1
		s.vy = pongDeflect(s.machine, next.y)
		s.rally++
	}

	if s.rally > s.bestRally {
		s.bestRally = s.rally
	}
	s.ball = next
	s.moveMachine()
}

// point awards a point and serves again towards whoever conceded it.
func (s *pongSession) point(yours bool) {
	if yours {
		s.yourScore++
	} else {
		s.machineScore++
	}
	switch {
	case s.yourScore >= pongTarget:
		s.over = true
		s.note(fmt.Sprintf("%d-%d. The match is yours. PF5 for another.",
			s.yourScore, s.machineScore), false)
	case s.machineScore >= pongTarget:
		s.over = true
		s.note(fmt.Sprintf("%d-%d to the machine. PF5 for another.",
			s.machineScore, s.yourScore), true)
	case yours:
		s.note(fmt.Sprintf("Your point. %d-%d.", s.yourScore, s.machineScore), false)
	default:
		s.note(fmt.Sprintf("The machine takes the point. %d-%d.",
			s.yourScore, s.machineScore), true)
	}
	if yours {
		s.serve(1)
		return
	}
	s.serve(-1)
}

// moveMachine is the whole of the opponent: a bat that tries to put its
// middle where the ball is, and is prevented from doing it perfectly by the
// level.
func (s *pongSession) moveMachine() {
	level := pongLevels[s.level]
	if level.blind && s.vx < 0 {
		return
	}
	if level.hesitate > 0 && s.rng.Intn(100) < level.hesitate {
		return
	}
	target := s.ball.y - pongBatHeight/2
	switch {
	case target > s.machine:
		s.machine = pongClamp(s.machine + 1)
	case target < s.machine:
		s.machine = pongClamp(s.machine - 1)
	}
}

// pongDeflect sends the ball away from the half of the bat it struck, which
// is the only control you have over where it goes.
func pongDeflect(bat, y int) int {
	if y-bat < pongBatHeight/2 {
		return -1
	}
	return 1
}

func pongCovers(bat, y int) bool {
	return y >= bat && y < bat+pongBatHeight
}

func pongClamp(top int) int {
	if top < 0 {
		return 0
	}
	if max := courtHeight - pongBatHeight; top > max {
		return max
	}
	return top
}

func (s *pongSession) note(msg string, isErr bool) {
	s.msg, s.msgErr = msg, isErr
}

func (s *pongSession) render() (go3270.Screen, int, int) {
	if s.page == pongPageHelp {
		return gameFrame(gameChrome{
			id:    "PNG020",
			title: "BAT AND BALL - HOW TO PLAY",
			keys:  "PF3=Back Enter=Back",
			msg:   "PF3 returns to the game.",
		}, gameHelpBody(pongHelpLines)), gameKeyRow, 0
	}

	painter := newCourtPainter()
	painter.paint(s.ball.x, s.ball.y, "O", go3270.Yellow)
	for i := 0; i < pongBatHeight; i++ {
		painter.paint(0, s.you+i, "#", go3270.Turquoise)
		painter.paint(courtWidth-1, s.machine+i, "#", go3270.Red)
	}
	for y := 0; y < courtHeight; y += 2 {
		painter.paint(courtWidth/2, y, ":", go3270.Blue)
	}

	body := append(courtBorder(go3270.Blue), painter.screen()...)
	entry, cursorCol := gameEntry("BAT ===>", 12, "bat", "", "U D or . to hold, e.g. UUD")
	body = append(body, entry...)

	screen := gameFrame(gameChrome{
		id:    "PNG010",
		title: "BAT AND BALL",
		right: fmt.Sprintf("YOU %d MACHINE %d RALLY %d BEST %d",
			s.yourScore, s.machineScore, s.rally, s.bestRally),
		keys: fmt.Sprintf("Enter=Play PF1=Help PF3=Exit PF5=New match PF6=Level %s "+
			"PF7=Up PF8=Down", pongLevels[s.level].name),
		msg:    s.msg,
		msgErr: s.msgErr,
	}, body)
	return screen, gameEntryRow, cursorCol
}

var pongHelpLines = []string{
	"THE GAME:",
	"  Your bat is on the left, the machine's on the right, and the first to",
	"  five points takes the match. The ball moves one cell for every move you",
	"  send, and waits for you in between.",
	"",
	"MOVING:",
	"  PF7 and PF8 move your bat up and down, and take a tick with them. The",
	"  entry line takes a run of moves and plays them in order:",
	"",
	"    UUD      up, up, down",
	"    3U       up three times",
	"    ..       hold still for two ticks",
	"",
	"  Which half of the bat the ball strikes decides which way it leaves.",
	"",
	"LEVELS (PF6 cycles):",
	"  EASY and FAIR only move while the ball is coming towards them, and",
	"  hesitate. HARD tracks the ball wherever it is, and does not.",
}
