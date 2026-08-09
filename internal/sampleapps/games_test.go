package sampleapps

import (
	"fmt"
	"math/rand"
	"os/exec"
	"strings"
	"testing"

	"github.com/racingmars/go3270"
)

// The games are drawn out of far more fields than the other samples — a
// coloured field per character cell in places — so the layout checks matter
// here even more than they do for the pet store. They are the same checks:
// nothing off the screen, no two fields fighting over a cell, no writable
// field name used twice.

type gameUnderTest struct {
	name     string
	id       string
	helpID   string
	render   func() (go3270.Screen, int, int)
	dispatch func(go3270.Response) bool
}

func gamesUnderTest(seed int64) []gameUnderTest {
	wordle := newWordleSession(seed)
	noughts := newTicTacToeSession(seed)
	snake := newSnakeSession(seed)
	pong := newPongSession(seed)
	return []gameUnderTest{
		{"word guess", "WRD010", "WRD020", wordle.render, wordle.dispatch},
		{"noughts and crosses", "TTT010", "TTT020", noughts.render, noughts.dispatch},
		{"snake", "SNK010", "SNK020", snake.render, snake.dispatch},
		{"bat and ball", "PNG010", "PNG020", pong.render, pong.dispatch},
	}
}

func TestGameScreensCarryTheirIdentifier(t *testing.T) {
	for _, game := range gamesUnderTest(1) {
		screen, _, _ := game.render()
		requireScreenID(t, game.name, screen, game.id)

		game.dispatch(go3270.Response{AID: go3270.AIDPF1, Values: map[string]string{}})
		help, _, _ := game.render()
		requireScreenID(t, game.name+" help", help, game.helpID)

		game.dispatch(go3270.Response{AID: go3270.AIDPF3, Values: map[string]string{}})
		back, _, _ := game.render()
		requireScreenID(t, game.name+" after help", back, game.id)
	}
}

func requireScreenID(t *testing.T, name string, screen go3270.Screen, id string) {
	t.Helper()
	for _, f := range screen {
		if strings.Contains(f.Content, id) {
			return
		}
	}
	t.Errorf("the %s screen does not print its own identifier %s", name, id)
}

// Every key, on every screen, with whatever happens to be typed in the entry
// field. Nothing may produce a screen a terminal would draw wrong, and
// nothing may panic — which is the reason a game whose board is an array
// indexed by a typed number is worth pointing generated input at.
func TestGameScreensStayLegalUnderEveryKey(t *testing.T) {
	aids := []go3270.AID{
		go3270.AIDEnter, go3270.AIDClear, go3270.AIDPA1, go3270.AIDPA2,
		go3270.AIDPF1, go3270.AIDPF2, go3270.AIDPF3, go3270.AIDPF4, go3270.AIDPF5,
		go3270.AIDPF6, go3270.AIDPF7, go3270.AIDPF8, go3270.AIDPF9, go3270.AIDPF10,
		go3270.AIDPF11, go3270.AIDPF12, go3270.AIDPF13, go3270.AIDPF24,
	}
	junk := []string{"", "X", "1", "5", "9", "0", "-1", "RRDD", "3R2D", "UUDD",
		"CRANE", "ZZZZZ", "  ", "..", "99R", "20D", "SELECT * FROM", "12345678901234567890"}

	for _, game := range gamesUnderTest(7) {
		step := 0
		for round := 0; round < 25 && !t.Failed(); round++ {
			for _, aid := range aids {
				screen, _, _ := game.render()
				values := map[string]string{}
				for _, f := range screen {
					if f.Write && f.Name != "" {
						values[f.Name] = junk[step%len(junk)]
						step++
					}
				}
				game.dispatch(go3270.Response{AID: aid, Values: values})

				after, row, col := game.render()
				name := fmt.Sprintf("%s after %s", game.name, go3270.AIDtoString(aid))
				checkScreenBounds(t, name, after, gameRows, gameCols)
				checkNoFieldCollisions(t, name, after, gameCols)
				checkUniqueWritableNames(t, name, after)
				if row < 0 || row >= gameRows || col < 0 || col >= gameCols {
					t.Fatalf("%s: cursor at %d,%d is off a %dx%d screen",
						name, row, col, gameRows, gameCols)
				}
				if t.Failed() {
					return
				}
			}
		}
	}
}

// A help screen longer than the body is silently cut off at the bottom, and
// the line that goes missing is the last key in the list.
func TestGameHelpScreensFitTheBody(t *testing.T) {
	rows := gameBodyBottom - gameBodyTop + 1
	for name, lines := range map[string][]string{
		"word guess":          wordleHelpLines,
		"noughts and crosses": tttHelpLines,
		"snake":               snakeHelpLines,
		"bat and ball":        pongHelpLines,
	} {
		if len(lines) > rows {
			t.Errorf("the %s help screen has %d lines but the body is %d rows",
				name, len(lines), rows)
		}
		for i, line := range lines {
			if len(line) > gameCols-2 {
				t.Errorf("%s help line %d is %d characters wide", name, i+1, len(line))
			}
		}
	}
}

// The word game.

func TestWordleScoresDuplicateLetters(t *testing.T) {
	tests := []struct {
		guess, answer, want string
	}{
		{"CRANE", "CRANE", "GGGGG"},
		{"CRANE", "TRACE", "yGG-G"},
		{"SPEED", "ERASE", "y-yy-"},
		{"EERIE", "ERASE", "G-y-G"},
		{"LEVEL", "LEMON", "GG---"},
		{"ABOUT", "CRANE", "y----"},
	}
	for _, test := range tests {
		got := wordleMarksString(wordleScore(test.guess, test.answer))
		if got != test.want {
			t.Errorf("%s against %s marked %s, want %s",
				test.guess, test.answer, got, test.want)
		}
	}
}

func wordleMarksString(marks []wordleMark) string {
	var b strings.Builder
	for _, mark := range marks {
		switch mark {
		case wordleHit:
			b.WriteByte('G')
		case wordlePresent:
			b.WriteByte('y')
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

func TestWordleWordListIsUsable(t *testing.T) {
	seen := map[string]bool{}
	for _, word := range wordleWords {
		switch {
		case len(word) != wordleLength:
			t.Errorf("%q is %d letters, and every word has to be %d", word, len(word), wordleLength)
		case !wordleIsLetters(word):
			t.Errorf("%q is not five capital letters", word)
		case seen[word]:
			t.Errorf("%q is in the word list twice", word)
		}
		seen[word] = true
	}
	if len(wordleWords) < 100 {
		t.Errorf("the word list has only %d words in it", len(wordleWords))
	}
}

func TestWordleRejectsWhatIsNotAGuess(t *testing.T) {
	s := newWordleSession(3)
	s.answer = "CRANE"

	for _, guess := range []string{"", "CRAN", "CRANES", "12345", "ZZZZZ"} {
		s.submit(guess)
		if len(s.rounds) != 0 {
			t.Fatalf("%q was accepted as a guess", guess)
		}
		if !s.msgErr {
			t.Errorf("%q was refused without saying so", guess)
		}
	}
}

func TestWordleRunsOutOfGoesAndKeepsScore(t *testing.T) {
	s := newWordleSession(4)
	s.answer = "CRANE"

	for i := 0; i < wordleMaxRounds; i++ {
		s.submit("about")
	}
	if !s.over || s.won {
		t.Fatalf("after %d wrong guesses the game is over=%v won=%v", wordleMaxRounds, s.over, s.won)
	}
	if s.played != 1 || s.wins != 0 || s.streak != 0 {
		t.Errorf("a loss recorded played=%d wins=%d streak=%d", s.played, s.wins, s.streak)
	}
	if !strings.Contains(s.msg, "CRANE") {
		t.Errorf("losing did not say what the word was: %q", s.msg)
	}

	s.newGame()
	s.answer = "TRACE"
	s.submit("crane")
	s.submit("trace")
	if !s.won || s.wins != 1 || s.streak != 1 || s.played != 2 {
		t.Fatalf("a win recorded won=%v wins=%d streak=%d played=%d",
			s.won, s.wins, s.streak, s.played)
	}
	// The tracker carries the best thing known about each letter, so the C of
	// a first guess that had it in the wrong place must not overwrite the
	// green it earned in the second.
	marks := s.letterMarks()
	if marks['T'] != wordleHit || marks['C'] != wordleHit || marks['N'] != wordleMiss {
		t.Errorf("the letter tracker holds T=%d C=%d N=%d", marks['T'], marks['C'], marks['N'])
	}
}

func TestWordleGiveUpShowsTheWord(t *testing.T) {
	s := newWordleSession(5)
	s.answer = "QUIET"
	s.giveUp()
	if !s.over || !strings.Contains(s.msg, "QUIET") {
		t.Errorf("giving up left over=%v and the message %q", s.over, s.msg)
	}
	if s.played != 1 || s.wins != 0 {
		t.Errorf("giving up recorded played=%d wins=%d", s.played, s.wins)
	}
}

// Noughts and crosses.

func TestNoughtsAndCrossesPerfectLevelNeverLoses(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	for round := 0; round < 60; round++ {
		s := newTicTacToeSession(int64(round))
		s.level = len(tttLevels) - 1
		// Two games per session, because who opens alternates and the
		// machine defending is a different problem from the machine
		// attacking.
		for game := 0; game < 2; game++ {
			for !s.over {
				free := tttFreeSquares(s.board)
				if len(free) == 0 {
					break
				}
				s.play(fmt.Sprintf("%d", free[rng.Intn(len(free))]+1))
			}
			if s.winner == tttPlayer {
				t.Fatalf("a perfect machine lost this board: %q", string(s.board[:]))
			}
			s.dispatch(go3270.Response{AID: go3270.AIDPF5, Values: map[string]string{}})
		}
	}
}

func TestNoughtsAndCrossesPerfectPlayDrawsItself(t *testing.T) {
	board := [9]byte{tttEmpty, tttEmpty, tttEmpty, tttEmpty, tttEmpty,
		tttEmpty, tttEmpty, tttEmpty, tttEmpty}
	mark := byte(tttPlayer)
	for !tttFull(board) {
		move, _ := tttSearch(board, mark, 0)
		if move < 0 {
			t.Fatalf("the search had no move on %q", string(board[:]))
		}
		board[move] = mark
		if winner, _ := tttWinner(board); winner != tttEmpty {
			t.Fatalf("%c won a game both sides played perfectly: %q", winner, string(board[:]))
		}
		mark = tttOther(mark)
	}
}

func TestNoughtsAndCrossesTakesTheWinItIsOffered(t *testing.T) {
	s := newTicTacToeSession(2)
	s.level = len(tttLevels) - 1
	// O to complete the top row, X nowhere near a line of its own.
	s.board = [9]byte{tttMachine, tttMachine, tttEmpty,
		tttPlayer, tttEmpty, tttEmpty,
		tttPlayer, tttEmpty, tttEmpty}
	if move := s.machineMove(); move != 2 {
		t.Errorf("the machine played square %d instead of the winning square 3", move+1)
	}
}

func TestNoughtsAndCrossesRefusesASquareThatIsTaken(t *testing.T) {
	s := newTicTacToeSession(6)
	s.board[4] = tttPlayer
	s.play("5")
	if !s.msgErr || !strings.Contains(s.msg, "taken") {
		t.Errorf("taking a taken square said %q", s.msg)
	}
	for _, bad := range []string{"", "0", "10", "A", " "} {
		before := s.board
		s.play(bad)
		if s.board != before {
			t.Fatalf("%q changed the board", bad)
		}
	}
}

func tttFreeSquares(board [9]byte) []int {
	free := make([]int, 0, len(board))
	for i, mark := range board {
		if mark == tttEmpty {
			free = append(free, i)
		}
	}
	return free
}

// The snake.

func TestSnakeEatsAndGrows(t *testing.T) {
	s := newSnakeSession(8)
	head := s.body[0]
	s.food = snakePoint{x: head.x + 1, y: head.y}
	length := len(s.body)

	s.step('R')
	if s.score != snakeFoodScore {
		t.Fatalf("eating scored %d, want %d", s.score, snakeFoodScore)
	}
	for i := 0; i < snakeGrowth; i++ {
		s.food = snakePoint{x: 0, y: courtHeight - 1}
		s.step('.')
	}
	if want := length + snakeGrowth; len(s.body) != want {
		t.Errorf("the snake is %d long after eating, want %d", len(s.body), want)
	}
}

func TestSnakeDiesAtTheWallUnlessTheWallIsOpen(t *testing.T) {
	s := newSnakeSession(9)
	for i := 0; i < courtHeight+2 && s.alive; i++ {
		s.step('U')
	}
	if s.alive {
		t.Fatal("the snake went through the top of the court")
	}
	if !strings.Contains(s.msg, "wall") {
		t.Errorf("dying at the wall said %q", s.msg)
	}

	s = newSnakeSession(9)
	s.wrap = true
	for i := 0; i < courtHeight+2; i++ {
		s.step('U')
	}
	if !s.alive {
		t.Fatalf("the snake died with the walls open: %q", s.msg)
	}
}

func TestSnakeDiesOnItself(t *testing.T) {
	s := newSnakeSession(10)
	s.body = []snakePoint{{x: 5, y: 5}, {x: 4, y: 5}, {x: 4, y: 6}, {x: 5, y: 6}, {x: 6, y: 6}}
	s.food = snakePoint{x: courtWidth - 1, y: courtHeight - 1}
	s.dir = snakePoint{x: 1}

	s.step('D')
	if s.alive {
		t.Fatalf("the snake ran through its own body: %v", s.body)
	}
}

func TestSnakeWillNotTurnBackOnItself(t *testing.T) {
	s := newSnakeSession(11)
	s.food = snakePoint{x: courtWidth - 1, y: courtHeight - 1}
	head := s.body[0]

	s.step('L')
	if !s.alive {
		t.Fatal("turning back on itself killed the snake")
	}
	if s.body[0].x != head.x+1 {
		t.Errorf("the snake moved to %v, and should have carried on right from %v", s.body[0], head)
	}
}

func TestSnakePlaysAWholeTypedRun(t *testing.T) {
	s := newSnakeSession(12)
	s.food = snakePoint{x: courtWidth - 1, y: courtHeight - 1}
	head := s.body[0]

	s.run("2R2D")
	if !s.alive {
		t.Fatalf("a four tick run killed the snake: %q", s.msg)
	}
	if want := (snakePoint{x: head.x + 2, y: head.y + 2}); s.body[0] != want {
		t.Errorf("the head is at %v after 2R2D, want %v", s.body[0], want)
	}

	s.run("bad")
	if !s.msgErr {
		t.Error("a move string of nonsense was accepted")
	}
}

// The bat and ball.

func TestPongBatReturnsTheBall(t *testing.T) {
	s := newPongSession(13)
	s.level = 0
	s.you = 4
	s.ball = pongPoint{x: 1, y: 5}
	s.vx, s.vy = -1, 1

	s.tick()
	if s.vx != 1 {
		t.Errorf("the ball left the bat going %d, want 1", s.vx)
	}
	if s.machineScore != 0 {
		t.Errorf("a returned ball conceded a point")
	}
	if s.rally != 1 {
		t.Errorf("the rally is %d after one return", s.rally)
	}
}

func TestPongMissConcedesAPointAndServes(t *testing.T) {
	s := newPongSession(14)
	s.you = 0
	s.ball = pongPoint{x: 1, y: courtHeight - 2}
	s.vx, s.vy = -1, 1

	s.tick()
	if s.machineScore != 1 {
		t.Fatalf("missing the ball left the score at %d-%d", s.yourScore, s.machineScore)
	}
	if s.ball.x != courtWidth/2 {
		t.Errorf("the ball was not served from the middle: %v", s.ball)
	}
	if s.vx != -1 {
		t.Errorf("the serve went %d, and should go back to the player who conceded", s.vx)
	}
}

func TestPongMatchEndsAtTheTarget(t *testing.T) {
	s := newPongSession(15)
	s.machineScore = pongTarget - 1
	s.you = 0
	s.ball = pongPoint{x: 1, y: courtHeight - 2}
	s.vx, s.vy = -1, 1

	s.tick()
	if !s.over {
		t.Fatalf("the match is not over at %d-%d", s.yourScore, s.machineScore)
	}
	s.step('U')
	if !s.msgErr || !strings.Contains(s.msg, "PF5") {
		t.Errorf("playing on after the match said %q", s.msg)
	}
}

func TestPongBallBouncesOffTheTop(t *testing.T) {
	s := newPongSession(16)
	s.ball = pongPoint{x: 5, y: 0}
	s.vx, s.vy = 1, -1

	s.tick()
	if s.vy != 1 || s.ball.y != 0 {
		t.Errorf("the ball is at y=%d going %d after hitting the top", s.ball.y, s.vy)
	}
}

func TestPongBatStaysInTheCourt(t *testing.T) {
	s := newPongSession(17)
	for i := 0; i < courtHeight*2; i++ {
		s.moveBat(-1)
	}
	if s.you != 0 {
		t.Errorf("the bat went to %d off the top of the court", s.you)
	}
	for i := 0; i < courtHeight*2; i++ {
		s.moveBat(1)
	}
	if want := courtHeight - pongBatHeight; s.you != want {
		t.Errorf("the bat went to %d off the bottom of the court, want %d", s.you, want)
	}
}

// Typed move strings.

func TestParseGameMoves(t *testing.T) {
	tests := []struct {
		text, allowed, want string
		wantErr             bool
	}{
		{text: "RRDD", allowed: "UDLR.", want: "RRDD"},
		{text: "3R2D", allowed: "UDLR.", want: "RRRDD"},
		{text: "r r d", allowed: "UDLR.", want: "RRD"},
		{text: "..", allowed: "UD.", want: ".."},
		{text: "", allowed: "UDLR.", want: ""},
		{text: "99U", allowed: "UD.", want: strings.Repeat("U", 6)},
		{text: "L", allowed: "UD.", wantErr: true},
		{text: "?", allowed: "UDLR.", wantErr: true},
	}
	for _, test := range tests {
		moves, err := parseGameMoves(test.text, test.allowed, 6)
		if test.wantErr {
			if err == nil {
				t.Errorf("%q was accepted where %s are the moves", test.text, test.allowed)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q was refused: %v", test.text, err)
			continue
		}
		if got := string(moves); got != test.want {
			t.Errorf("%q became %q, want %q", test.text, got, test.want)
		}
	}
}

// End to end, through a real terminal, which is the only check that the
// datastream these screens emit is one a 3270 client will accept.
func TestGamesThroughS3270(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the terminal test in short mode")
	}
	binary, err := exec.LookPath("s3270")
	if err != nil {
		t.Skip("s3270 is not installed on this machine")
	}

	t.Run("word guess", func(t *testing.T) {
		term, screen := startGame(t, binary, "wordle")
		defer term.close()
		requireOnScreen(t, screen, "WRD010", "WORD GUESS", "GUESS 1", "GUESS 6", "LETTERS")

		entry := findText(t, screen, "GUESS ===>")
		term.typeAt(t, entry, 12, "ZZZZZ")
		screen = term.press(t, "Enter()")
		requireOnScreen(t, screen, "not in this word list")

		term.typeAt(t, entry, 12, "CRANE")
		screen = term.press(t, "Enter()")
		requireOnScreen(t, screen, "WRD010")

		screen = term.press(t, "PF(6)")
		requireOnScreen(t, screen, "The word was")

		screen = term.press(t, "PF(1)")
		requireOnScreen(t, screen, "WRD020", "HOW TO PLAY")
		screen = term.press(t, "PF(3)")
		requireOnScreen(t, screen, "WRD010")
	})

	t.Run("noughts and crosses", func(t *testing.T) {
		term, screen := startGame(t, binary, "tictactoe")
		defer term.close()
		requireOnScreen(t, screen, "TTT010", "NOUGHTS AND CROSSES", "+-----+-----+-----+", "LEVEL")

		term.typeAt(t, findText(t, screen, "SQUARE ===>"), 13, "5")
		screen = term.press(t, "Enter()")
		requireOnScreen(t, screen, "X", "The machine took square")

		screen = term.press(t, "PF(6)")
		requireOnScreen(t, screen, "Level")
	})

	t.Run("snake", func(t *testing.T) {
		term, screen := startGame(t, binary, "snake")
		defer term.close()
		requireOnScreen(t, screen, "SNK010", "SNAKE", "SCORE 0", "@")

		term.typeAt(t, findText(t, screen, "MOVE ===>"), 11, "2R")
		screen = term.press(t, "Enter()")
		requireOnScreen(t, screen, "2 ticks")

		screen = term.press(t, "PF(7)")
		requireOnScreen(t, screen, "SNK010")
		screen = term.press(t, "PF(6)")
		requireOnScreen(t, screen, "walls are now OPEN")
	})

	t.Run("bat and ball", func(t *testing.T) {
		term, screen := startGame(t, binary, "pong")
		defer term.close()
		requireOnScreen(t, screen, "PNG010", "BAT AND BALL", "YOU 0 MACHINE 0", "#", "O")

		screen = term.press(t, "PF(8)")
		requireOnScreen(t, screen, "PNG010")

		term.typeAt(t, findText(t, screen, "BAT ===>"), 10, "3U")
		screen = term.press(t, "Enter()")
		requireOnScreen(t, screen, "PNG010")

		screen = term.press(t, "PF(6)")
		requireOnScreen(t, screen, "Level")
	})
}

func startGame(t *testing.T, binary, app string) (*terminal, []string) {
	t.Helper()
	server, err := StartServerOn(app, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not start %s: %v", app, err)
	}
	t.Cleanup(func() { server.Stop() })

	// Model 2. The games draw on the default buffer whatever the terminal
	// offers, so this is the size they are designed against.
	term := startTerminal(t, binary, "2", server.Addr())
	screen := term.screen(t)
	if rows := len(screen); rows != gameRows {
		t.Fatalf("%s was given %d rows, want %d", app, rows, gameRows)
	}
	return term, screen
}

func TestEveryGameIsRegistered(t *testing.T) {
	for _, app := range []string{"wordle", "tictactoe", "snake", "pong"} {
		if handlerFor(app) == nil {
			t.Errorf("sample app %q has no handler, so it cannot be started", app)
		}
	}
}
