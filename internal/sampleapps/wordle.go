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

// The word guessing game: six goes at a five letter word, with each guess
// coloured to say how close it was.
//
// Of the four games this is the one that fits a block mode terminal without
// being bent to do so — a guess is exactly one transmission — which is why it
// is also the one worth pointing a workflow recording or an assistant at.
//
// It is also a small colour test that anybody can read at a glance: three
// field colours under reverse video, twenty-six more in the letter tracker,
// and all of them changing on every transmission.

type wordlePage int

const (
	wordlePageGame wordlePage = iota
	wordlePageHelp
)

type wordleMark byte

const (
	wordleUntried wordleMark = iota
	wordleMiss
	wordlePresent
	wordleHit
)

const (
	wordleLength    = 5
	wordleMaxRounds = 6

	wordleTileCol  = 28
	wordleTileStep = 5
)

type wordleRound struct {
	guess string
	marks []wordleMark
}

type wordleSession struct {
	rng *rand.Rand

	page   wordlePage
	answer string
	rounds []wordleRound

	over bool
	won  bool

	played, wins, streak, best int

	msg    string
	msgErr bool
}

func handleWordle(conn net.Conn) {
	defer conn.Close()

	dev, err := go3270.NegotiateTelnet(conn)
	if err != nil {
		return
	}

	s := newWordleSession(time.Now().UnixNano())
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

func newWordleSession(seed int64) *wordleSession {
	s := &wordleSession{rng: rand.New(rand.NewSource(seed))}
	s.newGame()
	return s
}

func (s *wordleSession) newGame() {
	s.answer = wordleWords[s.rng.Intn(len(wordleWords))]
	s.rounds = nil
	s.over = false
	s.won = false
	s.msg = "Type a five letter word and press Enter."
	s.msgErr = false
}

func (s *wordleSession) dispatch(resp go3270.Response) bool {
	switch resp.AID {
	case go3270.AIDPF3:
		if s.page == wordlePageHelp {
			s.page = wordlePageGame
			return false
		}
		return true
	case go3270.AIDPF1:
		s.page = wordlePageHelp
		return false
	case go3270.AIDPF5:
		s.page = wordlePageGame
		if !s.over {
			s.recordLoss()
		}
		s.newGame()
		return false
	case go3270.AIDPF6:
		s.page = wordlePageGame
		s.giveUp()
		return false
	case go3270.AIDEnter:
		if s.page == wordlePageHelp {
			s.page = wordlePageGame
			return false
		}
		s.submit(resp.Values["guess"])
		return false
	}
	s.page = wordlePageGame
	s.note(fmt.Sprintf("%s does nothing here. Enter guesses, PF1 explains the rest.",
		go3270.AIDtoString(resp.AID)), true)
	return false
}

func (s *wordleSession) submit(text string) {
	if s.over {
		s.note("That game is over. PF5 starts another one.", true)
		return
	}
	guess := strings.ToUpper(strings.TrimSpace(text))
	switch {
	case guess == "":
		s.note("Type a word first.", true)
		return
	case len(guess) != wordleLength:
		s.note(fmt.Sprintf("%s has %d letters. Guesses are %d.", guess, len(guess), wordleLength), true)
		return
	case !wordleIsLetters(guess):
		s.note(fmt.Sprintf("%s is not five letters.", guess), true)
		return
	case !wordleKnown(guess):
		s.note(fmt.Sprintf("%s is not in this word list. PF1 says what is.", guess), true)
		return
	}

	s.rounds = append(s.rounds, wordleRound{guess: guess, marks: wordleScore(guess, s.answer)})
	switch {
	case guess == s.answer:
		s.over, s.won = true, true
		s.played++
		s.wins++
		s.streak++
		if s.streak > s.best {
			s.best = s.streak
		}
		s.note(fmt.Sprintf("%s, in %d. PF5 for another.", s.answer, len(s.rounds)), false)
	case len(s.rounds) >= wordleMaxRounds:
		s.over = true
		s.recordLoss()
		s.note(fmt.Sprintf("Out of goes. The word was %s. PF5 for another.", s.answer), true)
	default:
		s.note(fmt.Sprintf("%d %s left.", wordleMaxRounds-len(s.rounds),
			gamePlural(wordleMaxRounds-len(s.rounds), "go", "goes")), false)
	}
}

func (s *wordleSession) giveUp() {
	if s.over {
		s.note("That game is already over. PF5 starts another one.", true)
		return
	}
	s.over = true
	s.recordLoss()
	s.note(fmt.Sprintf("The word was %s. PF5 for another.", s.answer), true)
}

func (s *wordleSession) recordLoss() {
	s.played++
	s.streak = 0
}

func (s *wordleSession) note(msg string, isErr bool) {
	s.msg, s.msgErr = msg, isErr
}

func (s *wordleSession) render() (go3270.Screen, int, int) {
	if s.page == wordlePageHelp {
		return gameFrame(gameChrome{
			id:    "WRD020",
			title: "WORD GUESS - HOW TO PLAY",
			keys:  "PF3=Back Enter=Back",
			msg:   "PF3 returns to the game.",
		}, gameHelpBody(wordleHelpLines)), gameKeyRow, 0
	}

	body := make(go3270.Screen, 0, 64)
	body = append(body,
		go3270.Field{Row: gameBodyTop, Col: 0, Color: go3270.White,
			Content: "Six goes at a five letter word. Type one below and press Enter."},
	)
	body = append(body, wordleLegend()...)
	body = append(body, s.guessRows()...)
	body = append(body, s.letterTracker()...)
	body = append(body, go3270.Field{Row: gameBodyBottom, Col: 0, Color: go3270.Turquoise,
		Content: fmt.Sprintf("PLAYED %d   WON %d   STREAK %d   BEST STREAK %d",
			s.played, s.wins, s.streak, s.best)})

	entry, cursorCol := gameEntry("GUESS ===>", wordleLength, "guess", "",
		"five letters, then Enter")
	body = append(body, entry...)

	right := fmt.Sprintf("GO %d OF %d", len(s.rounds)+1, wordleMaxRounds)
	switch {
	case s.won:
		right = fmt.Sprintf("SOLVED IN %d", len(s.rounds))
	case s.over:
		right = "GAME OVER"
	}
	screen := gameFrame(gameChrome{
		id:     "WRD010",
		title:  "WORD GUESS",
		right:  right,
		keys:   "Enter=Guess PF1=How to play PF3=Exit PF5=New word PF6=Give up",
		msg:    s.msg,
		msgErr: s.msgErr,
	}, body)
	return screen, gameEntryRow, cursorCol
}

func wordleLegend() go3270.Screen {
	return go3270.Screen{
		wordleTile(gameBodyTop+1, 2, 'A', wordleHit),
		{Row: gameBodyTop + 1, Col: 6, Color: go3270.Green, Content: "RIGHT PLACE"},
		wordleTile(gameBodyTop+1, 20, 'B', wordlePresent),
		{Row: gameBodyTop + 1, Col: 24, Color: go3270.Yellow, Content: "WRONG PLACE"},
		wordleTile(gameBodyTop+1, 38, 'C', wordleMiss),
		{Row: gameBodyTop + 1, Col: 42, Color: go3270.Blue, Content: "NOT IN THE WORD"},
	}
}

// guessRows draws the six rows of tiles: the guesses made, then an empty rack
// for the ones still to come.
func (s *wordleSession) guessRows() go3270.Screen {
	screen := make(go3270.Screen, 0, wordleMaxRounds*(wordleLength+1))
	for i := 0; i < wordleMaxRounds; i++ {
		row := gameBodyTop + 3 + i*2
		screen = append(screen, go3270.Field{Row: row, Col: 18, Color: go3270.Blue,
			Content: fmt.Sprintf("GUESS %d", i+1)})
		for j := 0; j < wordleLength; j++ {
			col := wordleTileCol + j*wordleTileStep
			if i < len(s.rounds) {
				screen = append(screen,
					wordleTile(row, col, s.rounds[i].guess[j], s.rounds[i].marks[j]))
				continue
			}
			screen = append(screen, go3270.Field{Row: row, Col: col,
				Color: go3270.Blue, Content: " . "})
		}
	}
	return screen
}

// letterTracker is the alphabet, coloured by the best thing known about each
// letter so far. It is the part of the game a player actually reads, and it is
// twenty-six one character fields in two rows.
func (s *wordleSession) letterTracker() go3270.Screen {
	known := s.letterMarks()
	screen := make(go3270.Screen, 0, 28)
	for line := 0; line < 2; line++ {
		row := gameBodyTop + 15 + line
		label := "LETTERS"
		if line == 1 {
			label = ""
		}
		if label != "" {
			screen = append(screen, go3270.Field{Row: row, Col: 0,
				Color: go3270.White, Content: label})
		}
		for i := 0; i < 13; i++ {
			letter := byte('A' + line*13 + i)
			color, highlight := wordleMarkStyle(known[letter])
			screen = append(screen, go3270.Field{Row: row, Col: 12 + i*3,
				Color: color, Highlighting: highlight, Intense: true,
				Content: string(letter)})
		}
	}
	return screen
}

func (s *wordleSession) letterMarks() map[byte]wordleMark {
	marks := map[byte]wordleMark{}
	for _, round := range s.rounds {
		for i := 0; i < wordleLength; i++ {
			letter := round.guess[i]
			if round.marks[i] > marks[letter] {
				marks[letter] = round.marks[i]
			}
		}
	}
	return marks
}

func wordleTile(row, col int, letter byte, mark wordleMark) go3270.Field {
	color, highlight := wordleMarkStyle(mark)
	return go3270.Field{Row: row, Col: col, Color: color, Highlighting: highlight,
		Intense: true, Content: fmt.Sprintf(" %c ", letter)}
}

func wordleMarkStyle(mark wordleMark) (go3270.Color, go3270.Highlight) {
	switch mark {
	case wordleHit:
		return go3270.Green, go3270.ReverseVideo
	case wordlePresent:
		return go3270.Yellow, go3270.ReverseVideo
	case wordleMiss:
		return go3270.Blue, go3270.ReverseVideo
	}
	return go3270.White, go3270.DefaultHighlight
}

// wordleScore marks a guess against the answer.
//
// The duplicate letter rule is the whole of the difficulty here: a letter of
// the answer can only be claimed once, and an exact match claims it first.
// Guessing SPEED against ERASE has to mark one E, not two.
func wordleScore(guess, answer string) []wordleMark {
	marks := make([]wordleMark, len(guess))
	unclaimed := map[byte]int{}
	for i := 0; i < len(guess) && i < len(answer); i++ {
		if guess[i] == answer[i] {
			marks[i] = wordleHit
			continue
		}
		unclaimed[answer[i]]++
	}
	for i := 0; i < len(guess); i++ {
		if marks[i] == wordleHit {
			continue
		}
		if unclaimed[guess[i]] > 0 {
			marks[i] = wordlePresent
			unclaimed[guess[i]]--
			continue
		}
		marks[i] = wordleMiss
	}
	return marks
}

func wordleIsLetters(word string) bool {
	for i := 0; i < len(word); i++ {
		if word[i] < 'A' || word[i] > 'Z' {
			return false
		}
	}
	return len(word) > 0
}

func wordleKnown(word string) bool {
	for _, candidate := range wordleWords {
		if candidate == word {
			return true
		}
	}
	return false
}

var wordleHelpLines = []string{
	"THE GAME:",
	"  Six goes at a five letter word. Type a guess on the entry line and press",
	"  Enter. Every letter of the guess is then coloured:",
	"",
	"    GREEN    the letter is in the word, and in that place",
	"    YELLOW   the letter is in the word, somewhere else",
	"    BLUE     the letter is not in the word at all",
	"",
	"  A letter of the answer can only be claimed once, so a guess with two of",
	"  a letter against an answer with one marks only one of them.",
	"",
	"  The alphabet along the bottom carries the best thing known about every",
	"  letter so far, which is where most of the game is actually played.",
	"",
	"KEYS:",
	"  Enter    submit the guess typed on the entry line",
	"  PF3 exit   PF5 give up on this word and start another   PF6 show it",
}

// wordleWords is both the answer list and the list of guesses the game will
// accept. One list rather than two keeps the rule simple enough to state on
// the help screen: if it is not here, it is not a word as far as this sample
// is concerned.
var wordleWords = []string{
	"ABOUT", "ABOVE", "ACTOR", "ADMIT", "ADOPT", "AFTER", "AGAIN", "AGENT",
	"AGREE", "ALARM", "ALBUM", "ALERT", "ALIKE", "ALIVE", "ALLOW", "ALONE",
	"ALONG", "ALTER", "AMBER", "AMONG", "ANGEL", "ANGER", "ANGLE", "ANGRY",
	"APART", "APPLE", "APPLY", "APRON", "ARENA", "ARGUE", "ARISE", "AROMA",
	"ARRAY", "ARROW", "ASIDE", "ASSET", "AUDIO", "AUDIT", "AVOID", "AWAKE",
	"AWARD", "AWARE", "BADGE", "BAKER", "BASIC", "BASIN", "BEACH", "BEGAN",
	"BEGIN", "BEING", "BELOW", "BENCH", "BERRY", "BIRTH", "BLACK", "BLADE",
	"BLAME", "BLANK", "BLAST", "BLEND", "BLIND", "BLOCK", "BLOOD", "BOARD",
	"BOAST", "BONUS", "BOOST", "BOOTH", "BOUND", "BRAIN", "BRAND", "BRAVE",
	"BREAD", "BREAK", "BREED", "BRICK", "BRIEF", "BRING", "BROAD", "BROWN",
	"BRUSH", "BUILD", "BUILT", "BUNCH", "BURST", "CABIN", "CABLE", "CANDY",
	"CARGO", "CARRY", "CARVE", "CATCH", "CAUSE", "CHAIN", "CHAIR", "CHALK",
	"CHARM", "CHART", "CHASE", "CHEAP", "CHECK", "CHEST", "CHIEF", "CHILD",
	"CHILL", "CHOIR", "CHOSE", "CIVIC", "CLAIM", "CLASS", "CLEAN", "CLEAR",
	"CLERK", "CLICK", "CLIFF", "CLIMB", "CLOCK", "CLOSE", "CLOTH", "CLOUD",
	"COACH", "COAST", "COVER", "CRAFT", "CRANE", "CRASH", "CRAZY", "CREAM",
	"CRIME", "CROSS", "CROWD", "CROWN", "CRUDE", "CURVE", "CYCLE", "DAILY",
	"DAIRY", "DANCE", "DEALT", "DEBUG", "DECAY", "DELAY", "DEPTH", "DIARY",
	"DIRTY", "DOZEN", "DRAFT", "DRAIN", "DRAMA", "DREAM", "DRESS", "DRIED",
	"DRIFT", "DRINK", "DRIVE", "EAGER", "EAGLE", "EARLY", "EARTH", "EIGHT",
	"ELDER", "ELECT", "EMPTY", "ENEMY", "ENJOY", "ENTER", "ENTRY", "EQUAL",
	"ERASE", "ERROR", "EVENT", "EVERY", "EXACT", "EXIST", "EXTRA", "FAITH",
	"FALSE", "FANCY", "FAULT", "FEAST", "FENCE", "FERRY", "FEVER", "FIELD",
	"FIFTH", "FIFTY", "FIGHT", "FINAL", "FIRST", "FLAME", "FLASH", "FLEET",
	"FLOAT", "FLOOD", "FLOOR", "FLOUR", "FLUID", "FOCUS", "FORCE", "FORGE",
	"FOUND", "FRAME", "FRANK", "FRAUD", "FRESH", "FRONT", "FROST", "FRUIT",
	"GLASS", "GLOBE", "GRACE", "GRADE", "GRAIN", "GRAND", "GRANT", "GRAPH",
	"GRASP", "GRASS", "GREAT", "GREEN", "GREET", "GROUP", "GUARD", "GUESS",
	"GUEST", "GUIDE", "HABIT", "HAPPY", "HARSH", "HEART", "HEAVY", "HOBBY",
	"HONEY", "HORSE", "HOTEL", "HOUSE", "HUMAN", "HUMID", "IDEAL", "IMAGE",
	"INDEX", "INNER", "INPUT", "ISSUE", "IVORY", "JOINT", "JUDGE", "KNEEL",
	"KNIFE", "KNOCK", "KNOWN", "LABEL", "LARGE", "LASER", "LATER", "LAUGH",
	"LAYER", "LEARN", "LEASE", "LEAST", "LEAVE", "LEGAL", "LEMON", "LEVEL",
	"LIGHT", "LIMIT", "LINEN", "LOCAL", "LODGE", "LOGIC", "LOOSE", "LOWER",
	"LOYAL", "LUCKY", "LUNCH", "MAGIC", "MAJOR", "MAPLE", "MARCH", "MATCH",
	"MAYOR", "MEDAL", "MEDIA", "MERCY", "MERGE", "METAL", "METER", "MIGHT",
	"MINOR", "MINUS", "MIXED", "MODEL", "MONEY", "MONTH", "MORAL", "MOTOR",
	"MOUNT", "MOUSE", "MOUTH", "MOVIE", "MUSIC", "NAVAL", "NERVE", "NEVER",
	"NEWLY", "NIGHT", "NOBLE", "NOISE", "NORTH", "NOTED", "NOVEL", "NURSE",
	"OCCUR", "OCEAN", "OFFER", "OFTEN", "ORDER", "ORGAN", "OTHER", "OUGHT",
	"OUNCE", "OUTER", "OWNER", "PAINT", "PANEL", "PAPER", "PARTY", "PATCH",
	"PEACE", "PEARL", "PENNY", "PHASE", "PHONE", "PHOTO", "PIANO", "PIECE",
	"PILOT", "PITCH", "PLACE", "PLAIN", "PLANE", "PLANT", "PLATE", "POINT",
	"POUND", "POWER", "PRESS", "PRICE", "PRIDE", "PRIME", "PRINT", "PRIOR",
	"PRIZE", "PROOF", "PROUD", "PROVE", "QUEEN", "QUERY", "QUEST", "QUICK",
	"QUIET", "QUITE", "QUOTA", "QUOTE", "RADIO", "RAISE", "RANCH", "RANGE",
	"RAPID", "RATIO", "REACH", "READY", "REALM", "REBEL", "REFER", "RELAX",
	"RELAY", "REPLY", "RIDGE", "RIGHT", "RIVAL", "RIVER", "ROAST", "ROBOT",
	"ROUGH", "ROUND", "ROUTE", "ROYAL", "RURAL", "SALAD", "SCALE", "SCENE",
	"SCOPE", "SCORE", "SCOUT", "SENSE", "SERVE", "SEVEN", "SHADE", "SHAFT",
	"SHAKE", "SHAPE", "SHARE", "SHARP", "SHEEP", "SHEET", "SHELF", "SHELL",
	"SHIFT", "SHINE", "SHIRT", "SHOCK", "SHOOT", "SHORE", "SHORT", "SIGHT",
	"SIREN", "SIXTY", "SKILL", "SLEEP", "SLIDE", "SLOPE", "SMALL", "SMART",
	"SMILE", "SMOKE", "SNAKE", "SOLID", "SOLVE", "SOUND", "SOUTH", "SPACE",
	"SPARE", "SPEAK", "SPEED", "SPELL", "SPEND", "SPINE", "SPLIT", "SPORT",
	"STACK", "STAFF", "STAGE", "STAIR", "STAKE", "STAMP", "STAND", "START",
	"STATE", "STEAM", "STEEL", "STICK", "STILL", "STOCK", "STONE", "STORE",
	"STORM", "STORY", "STOVE", "STRIP", "STUDY", "STYLE", "SUGAR", "SUITE",
	"SUPER", "SWEET", "SWIFT", "SWING", "TABLE", "TASTE", "TEACH", "TERMS",
	"THANK", "THEFT", "THEIR", "THEME", "THERE", "THICK", "THING", "THINK",
	"THIRD", "THOSE", "THREE", "THROW", "TIGER", "TIGHT", "TIMER", "TITLE",
	"TODAY", "TOKEN", "TOOTH", "TOPIC", "TOTAL", "TOUCH", "TOUGH", "TOWEL",
	"TOWER", "TRACE", "TRACK", "TRADE", "TRAIL", "TRAIN", "TREAT", "TREND",
	"TRIAL", "TRIBE", "TRICK", "TRUCK", "TRUST", "TRUTH", "TWICE", "UNCLE",
	"UNDER", "UNION", "UNITE", "UNTIL", "UPPER", "UPSET", "URBAN", "USAGE",
	"USUAL", "VALID", "VALUE", "VALVE", "VAPOR", "VAULT", "VERSE", "VIDEO",
	"VISIT", "VITAL", "VOICE", "WAGON", "WASTE", "WATCH", "WATER", "WHEAT",
	"WHEEL", "WHERE", "WHICH", "WHILE", "WHITE", "WHOLE", "WOMAN", "WORLD",
	"WORRY", "WORSE", "WORST", "WORTH", "WOULD", "WRITE", "WRONG", "YIELD",
	"YOUNG", "YOUTH",
}
