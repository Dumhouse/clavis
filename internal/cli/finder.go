package cli

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

// finderRows is how many matches the finder shows at once.
const finderRows = 10

// finderKey is one keypress, decoded from the terminal's raw bytes.
type finderKey int

const (
	keyNone finderKey = iota
	keyRune
	keyBackspace
	keyUp
	keyDown
	keyEnter
	keyTab
	keyQuit
	keyClear
)

// splitKeys cuts one raw-mode read into keys: fast typing and pastes arrive
// as several keys at once, arrow keys as 3-byte escape sequences.
func splitKeys(b []byte) [][]byte {
	var keys [][]byte
	for len(b) > 0 {
		size := 1
		switch {
		case b[0] == 27 && len(b) >= 3 && (b[1] == '[' || b[1] == 'O'):
			size = 3
		case b[0] >= utf8.RuneSelf:
			_, size = utf8.DecodeRune(b)
		}
		keys = append(keys, b[:size])
		b = b[size:]
	}
	return keys
}

// decodeKey reads one key. A lone Esc quits.
func decodeKey(b []byte) (finderKey, rune) {
	if len(b) == 0 {
		return keyNone, 0
	}
	switch b[0] {
	case 3, 4: // Ctrl-C, Ctrl-D
		return keyQuit, 0
	case 27:
		if len(b) == 1 {
			return keyQuit, 0
		}
		if len(b) >= 3 && (b[1] == '[' || b[1] == 'O') {
			switch b[2] {
			case 'A':
				return keyUp, 0
			case 'B':
				return keyDown, 0
			}
		}
		return keyNone, 0
	case 13, 10:
		return keyEnter, 0
	case 9:
		return keyTab, 0
	case 127, 8:
		return keyBackspace, 0
	case 16: // Ctrl-P
		return keyUp, 0
	case 14: // Ctrl-N
		return keyDown, 0
	case 21: // Ctrl-U
		return keyClear, 0
	}
	r, _ := utf8.DecodeRune(b)
	if r == utf8.RuneError || r < 32 {
		return keyNone, 0
	}
	return keyRune, r
}

// finderState is the finder without the terminal, so it can be tested.
type finderState struct {
	all      []RefCommand
	query    string
	matches  []RefCommand
	selected int
}

func newFinderState(commands []RefCommand) *finderState {
	s := &finderState{all: commands}
	s.refilter()
	return s
}

func (s *finderState) refilter() {
	s.matches = rankCommands(s.all, s.query)
	if s.selected >= len(s.matches) {
		s.selected = max(len(s.matches)-1, 0)
	}
}

// handle applies a key; it returns the chosen command path on Enter, and
// done when the finder should close.
func (s *finderState) handle(key finderKey, r rune) (picked string, done bool) {
	switch key {
	case keyRune:
		s.query += string(r)
		s.selected = 0
		s.refilter()
	case keyBackspace:
		if s.query != "" {
			_, size := utf8.DecodeLastRuneInString(s.query)
			s.query = s.query[:len(s.query)-size]
			s.refilter()
		}
	case keyClear:
		s.query, s.selected = "", 0
		s.refilter()
	case keyUp:
		if s.selected > 0 {
			s.selected--
		}
	case keyDown:
		if s.selected < len(s.matches)-1 {
			s.selected++
		}
	case keyTab:
		// Complete the query to the highlighted command.
		if len(s.matches) > 0 {
			s.query = s.matches[s.selected].Path + " "
			s.selected = 0
			s.refilter()
		}
	case keyEnter:
		if len(s.matches) > 0 {
			return s.matches[s.selected].Path, true
		}
	case keyQuit:
		return "", true
	}
	return "", false
}

// view draws the finder: prompt, a window of matches around the selection,
// and the selected command's usage and first example.
func (s *finderState) view(out *Output, width int) []string {
	lines := []string{
		out.style("clavis help", helpAccent) + " " + out.style("›", helpFaint) + " " + s.query + out.style("▏", helpAccent),
	}
	if len(s.matches) == 0 {
		lines = append(lines, out.style("  No command matches. Backspace to widen, Esc to quit.", helpFaint))
		return lines
	}
	start := 0
	if s.selected >= finderRows {
		start = s.selected - finderRows + 1
	}
	end := min(start+finderRows, len(s.matches))
	for i := start; i < end; i++ {
		command := s.matches[i]
		summary := command.Summary
		if limit := width - 30; limit > 10 && len(summary) > limit {
			summary = summary[:limit-1] + "…"
		}
		if i == s.selected {
			lines = append(lines, out.style("› ", helpAccent)+out.style(padRight(command.Path, 24), helpAccent)+" "+summary)
		} else {
			lines = append(lines, "  "+padRight(command.Path, 24)+" "+out.style(summary, helpDim))
		}
	}
	selected := s.matches[s.selected]
	lines = append(lines, "", "  "+out.style(selected.Usage, helpCode))
	if len(selected.Examples) > 0 {
		example := strings.SplitN(selected.Examples[0].Run, "\n", 2)[0]
		lines = append(lines, "  "+out.style("$ ", helpFaint)+out.style(example, helpCode))
	}
	lines = append(lines, "", out.style(fmt.Sprintf("  %d of %d · ↑↓ move · Tab complete · Enter open · Esc quit", len(s.matches), len(s.all)), helpFaint))
	return lines
}

// runFinder runs the finder in the terminal and returns the chosen command
// path, or "" when the user quit.
func runFinder(ref *Reference, out *Output) (string, error) {
	fd := int(os.Stdin.Fd())
	saved, err := term.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	defer func() { _ = term.Restore(fd, saved) }()

	state := newFinderState(ref.Commands)
	w := os.Stdout
	drawn := 0
	draw := func() {
		width := helpWidth()
		if drawn > 0 {
			_, _ = fmt.Fprintf(w, "\x1b[%dA", drawn-1)
		}
		_, _ = fmt.Fprint(w, "\r\x1b[J")
		lines := state.view(out, width)
		_, _ = fmt.Fprint(w, strings.Join(lines, "\r\n"))
		drawn = len(lines)
	}
	clear := func() {
		if drawn > 1 {
			_, _ = fmt.Fprintf(w, "\x1b[%dA", drawn-1)
		}
		_, _ = fmt.Fprint(w, "\r\x1b[J")
	}

	_, _ = fmt.Fprint(w, "\x1b[?25l") // hide the cursor while drawing
	defer func() { _, _ = fmt.Fprint(w, "\x1b[?25h") }()
	draw()
	buf := make([]byte, 256)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			clear()
			return "", nil //nolint:nilerr // a closed stdin just ends the finder
		}
		for _, raw := range splitKeys(buf[:n]) {
			key, r := decodeKey(raw)
			if picked, done := state.handle(key, r); done {
				clear()
				return picked, nil
			}
		}
		draw()
	}
}
