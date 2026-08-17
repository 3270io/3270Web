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

// Noughts and crosses, against a machine that can be made unbeatable.
//
// One transmission is one move, which is exactly what the terminal wants to
// do, and the grid is drawn out of individual fields so that each square can
// carry its own colour: crosses turquoise, noughts red, the free squares
// showing the number you would type to claim them, and the winning three
// under reverse video.

type tttPage int

const (
	tttPageGame tttPage = iota
	tttPageHelp
)

const (
	tttEmpty   = ' '
	tttPlayer  = 'X'
	tttMachine = 'O'

	// The grid's left edge. Everything else on the board is measured from
	// here: the vertical bars land on the columns of the horizontal rules,
	// and each square's mark sits in the middle of its cell.
	tttGridCol = 30
)

type tttLevel struct {
	name string
	// best is the percentage of moves played by search rather than at
	// random. A machine that always searches cannot be beaten.
	best int
}

var tttLevels = []tttLevel{
	{name: "EASY", best: 0},
	{name: "FAIR", best: 60},
	{name: "PERFECT", best: 100},
}

var tttCorners = []int{0, 2, 6, 8}

var tttLines = [8][3]int{
	{0, 1, 2}, {3, 4, 5}, {6, 7, 8},
	{0, 3, 6}, {1, 4, 7}, {2, 5, 8},
	{0, 4, 8}, {2, 4, 6},
}

type tttSession struct {
	rng *rand.Rand

	page  tttPage
	board [9]byte

	over   bool
	winner byte
	line   []int

	// starts is who opens the next game. It alternates, because a game of
	// noughts and crosses where one side always opens is a different game.
	starts byte

	level               int
	wins, losses, draws int

	msg    string
	msgErr bool
}

func handleTicTacToe(conn net.Conn) {
	defer conn.Close()

	dev, err := go3270.NegotiateTelnet(conn)
	if err != nil {
		return
	}

	s := newTicTacToeSession(time.Now().UnixNano())
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

func newTicTacToeSession(seed int64) *tttSession {
	s := &tttSession{
		rng:    rand.New(rand.NewSource(seed)),
		starts: tttPlayer,
		level:  len(tttLevels) - 1,
	}
	s.newGame()
	return s
}

func (s *tttSession) newGame() {
	s.board = [9]byte{tttEmpty, tttEmpty, tttEmpty, tttEmpty, tttEmpty,
		tttEmpty, tttEmpty, tttEmpty, tttEmpty}
	s.over = false
	s.winner = tttEmpty
	s.line = nil

	opener := s.starts
	s.starts = tttOther(s.starts)
	if opener == tttMachine {
		s.board[s.machineMove()] = tttMachine
		s.note("The machine opened. Your move.", false)
		return
	}
	s.note("Your move. Type the number of a free square.", false)
}

func (s *tttSession) dispatch(resp go3270.Response) bool {
	switch resp.AID {
	case go3270.AIDPF3:
		if s.page == tttPageHelp {
			s.page = tttPageGame
			return false
		}
		return true
	case go3270.AIDPF1:
		s.page = tttPageHelp
		return false
	case go3270.AIDPF5:
		s.page = tttPageGame
		if !s.over {
			// Abandoning a game part way through is a loss, or the score
			// line would be worth nothing.
			s.losses++
		}
		s.newGame()
		return false
	case go3270.AIDPF6:
		s.page = tttPageGame
		s.level = (s.level + 1) % len(tttLevels)
		s.note(fmt.Sprintf("Level %s. It applies from your next move.",
			tttLevels[s.level].name), false)
		return false
	case go3270.AIDEnter:
		if s.page == tttPageHelp {
			s.page = tttPageGame
			return false
		}
		s.play(resp.Values["square"])
		return false
	}
	s.page = tttPageGame
	s.note(fmt.Sprintf("%s does nothing here. PF1 lists the keys that do.",
		go3270.AIDtoString(resp.AID)), true)
	return false
}

func (s *tttSession) play(text string) {
	if s.over {
		s.note("That game is over. PF5 starts another one.", true)
		return
	}
	choice := strings.TrimSpace(text)
	if choice == "" {
		s.note("Type the number of a free square, 1 to 9.", true)
		return
	}
	if len(choice) != 1 || choice[0] < '1' || choice[0] > '9' {
		s.note(fmt.Sprintf("%q is not a square. The squares are 1 to 9.", choice), true)
		return
	}
	square := int(choice[0] - '1')
	if s.board[square] != tttEmpty {
		s.note(fmt.Sprintf("Square %d is taken by %c already.", square+1, s.board[square]), true)
		return
	}

	s.board[square] = tttPlayer
	if s.finish() {
		return
	}

	reply := s.machineMove()
	s.board[reply] = tttMachine
	if s.finish() {
		return
	}
	s.note(fmt.Sprintf("The machine took square %d. Your move.", reply+1), false)
}

// finish reports whether the game has just ended, and records it if so.
func (s *tttSession) finish() bool {
	winner, line := tttWinner(s.board)
	if winner == tttEmpty && !tttFull(s.board) {
		return false
	}
	s.over = true
	s.winner = winner
	s.line = line
	switch winner {
	case tttPlayer:
		s.wins++
		s.note("You win. PF5 for another game.", false)
	case tttMachine:
		s.losses++
		s.note("The machine wins. PF5 for another game.", true)
	default:
		s.draws++
		s.note("Drawn. PF5 for another game.", false)
	}
	return true
}

// machineMove picks the machine's square. Below the top level it plays at
// random some of the time, which is the only thing that makes it beatable.
func (s *tttSession) machineMove() int {
	free := make([]int, 0, 9)
	for i, mark := range s.board {
		if mark == tttEmpty {
			free = append(free, i)
		}
	}
	if len(free) == 0 {
		return -1
	}
	if s.rng.Intn(100) >= tttLevels[s.level].best {
		return free[s.rng.Intn(len(free))]
	}
	// The empty board is both the most expensive search in the game and the
	// least interesting one: with nothing to reply to, every opening draws,
	// so the search settles on the first square every time and the machine
	// opens in the same corner for ever. Open in a corner of its choosing
	// instead, which is the strongest opening against imperfect play anyway.
	if len(free) == len(s.board) {
		return tttCorners[s.rng.Intn(len(tttCorners))]
	}
	move, _ := tttSearch(s.board, tttMachine, 0)
	if move < 0 {
		return free[s.rng.Intn(len(free))]
	}
	return move
}

// tttSearch is minimax over the whole game, which a nine square board is
// small enough to afford. It returns the best square for the side to move and
// the value of the position to that side: positive if it can force a win,
// zero for a draw, negative if it is lost. Depth is carried so that a win
// taken now beats the same win taken in three moves' time, which is the
// difference between a machine that looks unbeatable and one that looks
// asleep.
func tttSearch(board [9]byte, mark byte, depth int) (int, int) {
	if winner, _ := tttWinner(board); winner != tttEmpty {
		return -1, depth - 10
	}
	if tttFull(board) {
		return -1, 0
	}
	bestMove, bestValue := -1, -100
	for i := 0; i < len(board); i++ {
		if board[i] != tttEmpty {
			continue
		}
		board[i] = mark
		_, reply := tttSearch(board, tttOther(mark), depth+1)
		board[i] = tttEmpty
		if value := -reply; value > bestValue {
			bestMove, bestValue = i, value
		}
	}
	return bestMove, bestValue
}

func tttWinner(board [9]byte) (byte, []int) {
	for _, line := range tttLines {
		mark := board[line[0]]
		if mark != tttEmpty && mark == board[line[1]] && mark == board[line[2]] {
			won := line
			return mark, won[:]
		}
	}
	return tttEmpty, nil
}

func tttFull(board [9]byte) bool {
	for _, mark := range board {
		if mark == tttEmpty {
			return false
		}
	}
	return true
}

func tttOther(mark byte) byte {
	if mark == tttPlayer {
		return tttMachine
	}
	return tttPlayer
}

func (s *tttSession) note(msg string, isErr bool) {
	s.msg, s.msgErr = msg, isErr
}

func (s *tttSession) render() (go3270.Screen, int, int) {
	if s.page == tttPageHelp {
		return gameFrame(gameChrome{
			id:    "TTT020",
			title: "NOUGHTS AND CROSSES - HOW TO PLAY",
			keys:  "PF3=Back Enter=Back",
			msg:   "PF3 returns to the game.",
		}, gameHelpBody(tttHelpLines)), gameKeyRow, 0
	}

	body := make(go3270.Screen, 0, 48)
	body = append(body,
		go3270.Field{Row: gameBodyTop, Col: 0, Color: go3270.White,
			Content: "You are X and the machine is O. Type the number of a free square."},
	)
	body = append(body, s.grid()...)
	body = append(body, go3270.Field{Row: gameBodyBottom - 1, Col: 0, Color: go3270.Turquoise,
		Content: fmt.Sprintf("LEVEL %-7s   PF6 changes it", tttLevels[s.level].name)})

	entry, cursorCol := gameEntry("SQUARE ===>", 1, "square", "", "1 to 9, then Enter")
	body = append(body, entry...)

	screen := gameFrame(gameChrome{
		id:     "TTT010",
		title:  "NOUGHTS AND CROSSES",
		right:  fmt.Sprintf("YOU %d  MACHINE %d  DRAWN %d", s.wins, s.losses, s.draws),
		keys:   "Enter=Take square PF1=How to play PF3=Exit PF5=New game PF6=Level",
		msg:    s.msg,
		msgErr: s.msgErr,
	}, body)
	return screen, gameEntryRow, cursorCol
}

// grid draws the board. The rules and the empty cell rows are one field each;
// the rows carrying marks are drawn as separate fields so that every square
// can have a colour of its own, which is what the attribute byte between the
// cells is there to pay for.
func (s *tttSession) grid() go3270.Screen {
	rule := "+" + strings.Repeat("-----+", 3)
	blank := "|" + strings.Repeat("     |", 3)
	winning := map[int]bool{}
	for _, square := range s.line {
		winning[square] = true
	}

	screen := make(go3270.Screen, 0, 32)
	for row := 0; row < 3; row++ {
		top := gameBodyTop + 2 + row*4
		markRow := top + 2
		screen = append(screen,
			go3270.Field{Row: top, Col: tttGridCol, Color: go3270.Blue, Content: rule},
			go3270.Field{Row: markRow - 1, Col: tttGridCol, Color: go3270.Blue, Content: blank},
			go3270.Field{Row: markRow + 1, Col: tttGridCol, Color: go3270.Blue, Content: blank},
		)
		for bar := 0; bar < 4; bar++ {
			screen = append(screen, go3270.Field{Row: markRow,
				Col: tttGridCol + bar*6, Color: go3270.Blue, Content: "|"})
		}
		for col := 0; col < 3; col++ {
			square := row*3 + col
			mark, color := s.square(square)
			field := go3270.Field{Row: markRow, Col: tttGridCol + 3 + col*6,
				Color: color, Intense: true, Content: mark}
			if winning[square] {
				field.Highlighting = go3270.ReverseVideo
			}
			screen = append(screen, field)
		}
	}
	return append(screen, go3270.Field{Row: gameBodyTop + 14, Col: tttGridCol,
		Color: go3270.Blue, Content: rule})
}

func (s *tttSession) square(i int) (string, go3270.Color) {
	switch s.board[i] {
	case tttPlayer:
		return "X", go3270.Turquoise
	case tttMachine:
		return "O", go3270.Red
	}
	return fmt.Sprintf("%d", i+1), go3270.Blue
}

var tttHelpLines = []string{
	"THE GAME:",
	"  The squares are numbered 1 to 9, left to right and top to bottom, and a",
	"  free square shows its own number. Type one on the entry line and press",
	"  Enter to take it. You are X; the machine replies as O in the same",
	"  transmission, so one Enter is one move each.",
	"",
	"  Whoever opens alternates from game to game, so when the machine opens it",
	"  has already played by the time the board is drawn.",
	"",
	"LEVELS (PF6 cycles):",
	"  EASY     the machine plays at random",
	"  FAIR     it searches for most of its moves, and blunders the rest",
	"  PERFECT  it searches every move, and cannot be beaten",
	"",
	"KEYS:",
	"  Enter    take the square typed on the entry line",
	"  PF3 exit   PF5 abandon this game and start another   PF6 change level",
}
