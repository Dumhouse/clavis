package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

/*
 * The full-screen help: group tabs, a searchable command list, and a live
 * preview with numbered examples that can be copied or run. The model is
 * plain data plus Update/View, so it is tested without a terminal.
 */

type helpMode int

const (
	modeList    helpMode = iota // typing filters the list
	modeDetail                  // pick, copy or run an example
	modeEdit                    // edit the command line before running it
	modeConfirm                 // risky command: type yes
)

// helpStore remembers what you open and star, in your config folder.
type helpStore struct {
	Opened    map[string]int `json:"opened"`
	Favorites []string       `json:"favorites"`
}

var helpStorePath = func() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "clavis", "help.json")
}

func loadHelpStore() helpStore {
	s := helpStore{Opened: map[string]int{}}
	if path := helpStorePath(); path != "" {
		if data, err := os.ReadFile(path); err == nil { //nolint:gosec // G304: the user's own config file
			_ = json.Unmarshal(data, &s)
		}
	}
	if s.Opened == nil {
		s.Opened = map[string]int{}
	}
	return s
}

func (s helpStore) save() {
	path := helpStorePath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	if data, err := json.Marshal(s); err == nil {
		_ = os.WriteFile(path, data, 0o600)
	}
}

func (s helpStore) isFavorite(path string) bool {
	for _, f := range s.Favorites {
		if f == path {
			return true
		}
	}
	return false
}

func (s *helpStore) toggleFavorite(path string) {
	for i, f := range s.Favorites {
		if f == path {
			s.Favorites = append(s.Favorites[:i], s.Favorites[i+1:]...)
			return
		}
	}
	s.Favorites = append(s.Favorites, path)
}

// helpContext is the header: where commands will run, and what to do next.
type helpContext struct {
	Server, Org, Project, Env string
	SignedIn, Linked          bool
}

func (c helpContext) next() string {
	switch {
	case !c.SignedIn:
		return "clavis login --server " + c.Server
	case !c.Linked:
		return "clavis init"
	}
	return ""
}

func currentHelpContext(opts *Options) helpContext {
	r, err := opts.resolveContext(false)
	if err != nil {
		return helpContext{}
	}
	return helpContext{Server: r.Server, Org: r.Org, Project: r.Project, Env: r.Env,
		SignedIn: r.Token != "", Linked: r.ConfigPath != "" || (r.Org != "" && r.Project != "")}
}

// Input events. Key names follow the usual spellings ("up", "enter",
// "ctrl+u"); typed characters arrive as Text.
type (
	helpKey   struct{ Name, Text string }
	helpMouse struct {
		X, Y   int
		Button string // "left", "wheelup", "wheeldown"
	}
	helpResize struct{ W, H int }
)

// id is the key's name, or the character typed.
func (k helpKey) id() string {
	if k.Name != "" {
		return k.Name
	}
	return k.Text
}

type helpTab struct{ Label, Group string }

var tabLabels = map[string]string{
	"get-started": "Start", "run": "Run", "secrets": "Secrets", "import": "Import", "schema": "Schema",
	"environments": "Envs", "access": "Access", "sets": "Sets", "automation": "Automate", "agents": "Agents", "server": "Server",
}

type tabHit struct{ from, to, tab int }

type helpModel struct {
	ref     *Reference
	store   helpStore
	header  helpContext
	tabs    []helpTab
	tab     int
	query   string
	list    []RefCommand
	sel     int
	top     int
	mode    helpMode
	example int
	scroll  int
	edit    string
	confirm string
	status  string
	width   int
	height  int
	run     string // the command to run after the screen closes
	done    bool
	hits    []tabHit
	out     *Output
}

// clipboardCopy is a seam: tests replace it.
var clipboardCopy = copyToClipboard

func newHelpModel(ref *Reference, store helpStore, header helpContext) *helpModel {
	tabs := []helpTab{{"All", ""}, {"★ Saved", "*"}}
	for _, g := range ref.Groups {
		label := tabLabels[g.ID]
		if label == "" {
			label = g.Title
		}
		tabs = append(tabs, helpTab{label, g.ID})
	}
	m := &helpModel{ref: ref, store: store, header: header, tabs: tabs, width: 100, height: 30,
		out: NewTestOutput(io.Discard, io.Discard, false, true)}
	m.refilter()
	return m
}

func (m *helpModel) base() []RefCommand {
	group := m.tabs[m.tab].Group
	switch group {
	case "":
		return m.ref.Commands
	case "*":
		var out []RefCommand
		for _, c := range m.ref.Commands {
			if m.store.isFavorite(c.Path) {
				out = append(out, c)
			}
		}
		return out
	}
	var out []RefCommand
	for _, c := range m.ref.Commands {
		if c.Group == group {
			out = append(out, c)
		}
	}
	return out
}

func (m *helpModel) refilter() {
	base := m.base()
	if strings.TrimSpace(m.query) != "" {
		m.list = rankCommands(base, m.query)
	} else {
		m.list = append([]RefCommand(nil), base...)
		// Starred first, then the ones you open most; reference order otherwise.
		sort.SliceStable(m.list, func(i, j int) bool {
			fi, fj := m.store.isFavorite(m.list[i].Path), m.store.isFavorite(m.list[j].Path)
			if fi != fj {
				return fi
			}
			return m.store.Opened[m.list[i].Path] > m.store.Opened[m.list[j].Path]
		})
	}
	if m.sel >= len(m.list) {
		m.sel = max(len(m.list)-1, 0)
	}
	m.top = 0
	m.keepVisible()
}

func (m *helpModel) rows() int { return max(m.height-7, 3) }

func (m *helpModel) keepVisible() {
	if m.sel < m.top {
		m.top = m.sel
	}
	if m.sel >= m.top+m.rows() {
		m.top = m.sel - m.rows() + 1
	}
}

func (m *helpModel) current() (RefCommand, bool) {
	if m.sel < 0 || m.sel >= len(m.list) {
		return RefCommand{}, false
	}
	return m.list[m.sel], true
}

// quitWith closes the screen; line, when set, is run afterwards.
func (m *helpModel) quitWith(line string) bool {
	m.run = line
	m.done = true
	return true
}

func (m *helpModel) move(delta int) {
	m.sel = min(max(m.sel+delta, 0), max(len(m.list)-1, 0))
	m.keepVisible()
}

func (m *helpModel) switchTab(delta int) {
	m.tab = (m.tab + delta + len(m.tabs)) % len(m.tabs)
	m.sel = 0
	m.refilter()
}

func (m *helpModel) open() {
	if c, ok := m.current(); ok {
		m.mode, m.example, m.scroll, m.status = modeDetail, 0, 0, ""
		m.store.Opened[c.Path]++
		m.store.save()
	}
}

// Update applies one event and reports whether the screen should close.
func (m *helpModel) Update(msg any) bool {
	switch msg := msg.(type) {
	case helpResize:
		m.width, m.height = msg.W, msg.H
		m.keepVisible()
	case helpMouse:
		m.mouse(msg)
	case helpKey:
		if msg.Name == "ctrl+c" {
			return m.quitWith("")
		}
		switch m.mode {
		case modeList:
			m.keyList(msg)
		case modeDetail:
			m.keyDetail(msg)
		case modeEdit:
			m.keyEdit(msg)
		case modeConfirm:
			m.keyConfirm(msg)
		}
	}
	return m.done
}

func dropRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(r[:len(r)-1])
}

func (m *helpModel) keyList(msg helpKey) {
	switch msg.Name {
	case "esc":
		if m.query != "" {
			m.query = ""
			m.refilter()
			return
		}
		m.quitWith("")
		return
	case "left", "shift+tab":
		m.switchTab(-1)
	case "right", "tab":
		m.switchTab(1)
	case "up", "ctrl+p":
		m.move(-1)
	case "down", "ctrl+n":
		m.move(1)
	case "pgup":
		m.move(-m.rows())
	case "pgdown":
		m.move(m.rows())
	case "enter":
		m.open()
	case "backspace":
		m.query = dropRune(m.query)
		m.sel = 0
		m.refilter()
	case "ctrl+u":
		m.query, m.sel = "", 0
		m.refilter()
	default:
		if text := msg.Text; text != "" {
			m.query += text
			m.sel = 0
			m.refilter()
		}
	}
}

// exampleLine flattens an example (which may continue lines with a trailing
// backslash) into one command line.
func exampleLine(run string) string {
	run = strings.ReplaceAll(run, "\\\n", " ")
	return strings.Join(strings.Fields(run), " ")
}

func (m *helpModel) keyDetail(msg helpKey) {
	c, ok := m.current()
	if !ok {
		m.mode = modeList
		return
	}
	pick := func(i int) bool {
		if i >= 0 && i < len(c.Examples) {
			m.example = i
			return true
		}
		return false
	}
	switch s := msg.id(); s {
	case "esc", "backspace", "left":
		m.mode, m.status = modeList, ""
	case "q":
		m.quitWith("")
		return
	case "up", "k":
		pick(m.example - 1)
	case "down", "j":
		pick(m.example + 1)
	case "pgup":
		m.scroll = max(m.scroll-5, 0)
	case "pgdown":
		m.scroll += 5
	case "f":
		m.store.toggleFavorite(c.Path)
		m.store.save()
		if m.store.isFavorite(c.Path) {
			m.status = "★ saved " + c.Path
		} else {
			m.status = "removed " + c.Path + " from Saved"
		}
	case "c":
		if len(c.Examples) == 0 {
			m.status = "no example to copy"
			break
		}
		line := exampleLine(c.Examples[m.example].Run)
		if err := clipboardCopy(line); err != nil {
			m.status = "could not copy: " + err.Error()
		} else {
			m.status = "copied: " + line
		}
	case "r", "enter":
		if len(c.Examples) == 0 {
			m.status = "this command has no example; copy its usage instead"
			break
		}
		m.edit, m.mode, m.status = exampleLine(c.Examples[m.example].Run), modeEdit, ""
	default:
		if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
			pick(int(s[0] - '1'))
		}
	}
}

var riskyLine = regexp.MustCompile(`(?i)(\bproduction\b|--env[ =]prod\b|\bprod\b| rm | revoke| restore| rotate-root-key| reset-password|unfreeze| unlink)`)

func (m *helpModel) keyEdit(msg helpKey) {
	switch msg.Name {
	case "esc":
		m.mode = modeDetail
	case "enter":
		line := strings.TrimSpace(m.edit)
		if line == "" {
			return
		}
		if riskyLine.MatchString(" " + line + " ") {
			m.mode, m.confirm = modeConfirm, ""
			return
		}
		m.quitWith(line)
		return
	case "backspace":
		m.edit = dropRune(m.edit)
	case "ctrl+u":
		m.edit = ""
	case "ctrl+w":
		m.edit = strings.TrimRight(strings.TrimRightFunc(strings.TrimRight(m.edit, " "), func(r rune) bool { return r != ' ' }), " ")
		if m.edit != "" {
			m.edit += " "
		}
	default:
		m.edit += msg.Text
	}
}

func (m *helpModel) keyConfirm(msg helpKey) {
	switch msg.Name {
	case "esc":
		m.mode = modeEdit
	case "backspace":
		m.confirm = dropRune(m.confirm)
	case "enter":
		if strings.EqualFold(strings.TrimSpace(m.confirm), "yes") {
			m.quitWith(strings.TrimSpace(m.edit))
			return
		}
		m.mode = modeEdit
	default:
		m.confirm += msg.Text
	}
}

func (m *helpModel) narrow() bool { return m.width < 80 }

func (m *helpModel) leftWidth() int {
	if m.narrow() {
		return m.width
	}
	return min(max(m.width/3, 30), 44)
}

func (m *helpModel) mouse(msg helpMouse) {
	if m.mode == modeEdit || m.mode == modeConfirm {
		return
	}
	inList := msg.X < m.leftWidth() && (!m.narrow() || m.mode == modeList)
	switch {
	case msg.Button == "wheelup":
		if inList {
			m.move(-1)
		} else {
			m.scroll = max(m.scroll-2, 0)
		}
	case msg.Button == "wheeldown":
		if inList {
			m.move(1)
		} else {
			m.scroll += 2
		}
	case msg.Button == "left":
		if msg.Y == 2 {
			for _, hit := range m.hits {
				if msg.X >= hit.from && msg.X < hit.to {
					m.tab, m.sel = hit.tab, 0
					m.mode = modeList
					m.refilter()
				}
			}
		} else if row := msg.Y - 5; inList && row >= 0 && m.top+row < len(m.list) {
			m.sel = m.top + row
			m.mode = modeList
		}
	}
}

// ---- view ----

func paint(color string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#" + color))
}

var (
	cAccent = paint(helpAccent)
	cCode   = paint(helpCode)
	cDim    = paint(helpDim)
	cFaint  = paint(helpFaint)
	cTitle  = paint(helpTitle).Bold(true)
	cWarn   = paint("f59e0b")
)

func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return lipgloss.NewStyle().Inline(true).Width(width).MaxWidth(width).Render(s)
}

func (m *helpModel) View() string {
	width := max(m.width, 20)
	var b strings.Builder
	line := func(s string) { b.WriteString(fit(s, width)); b.WriteString("\n") }

	// Header: title and where commands will run.
	line(cAccent.Bold(true).Render("Clavis help") + cFaint.Render("  "+fmt.Sprintf("%d commands", len(m.ref.Commands))))
	line(m.contextLine())

	// Tabs.
	var tabs strings.Builder
	m.hits = m.hits[:0]
	x := 0
	for i, tab := range m.tabs {
		label := " " + tab.Label + " "
		style := cDim
		if i == m.tab {
			style = cAccent.Bold(true).Underline(true)
		}
		m.hits = append(m.hits, tabHit{x, x + len([]rune(label)), i})
		tabs.WriteString(style.Render(label))
		x += len([]rune(label))
	}
	line(tabs.String())
	line(cFaint.Render(strings.Repeat("─", width)))

	// Body.
	body := m.body(width)
	for _, l := range body {
		line(l)
	}
	line(cFaint.Render(strings.Repeat("─", width)))
	b.WriteString(fit(m.footer(), width))
	return b.String()
}

func (m *helpModel) contextLine() string {
	h := m.header
	switch {
	case !h.SignedIn:
		return cWarn.Render("! not signed in") + cFaint.Render("  next: ") + cCode.Render(h.next())
	case !h.Linked:
		return cAccent.Render("✓ signed in") + cFaint.Render("  next: ") + cCode.Render("clavis init") + cFaint.Render(" to link this folder")
	}
	return cAccent.Render("✓ ") + cDim.Render(fmt.Sprintf("%s/%s · %s · %s", h.Org, h.Project, h.Env, h.Server))
}

func (m *helpModel) body(width int) []string {
	height := m.height - 6
	height = max(height, 4)
	left := m.leftPane(height)
	if m.narrow() {
		if m.mode == modeList {
			return pad(left, height, width)
		}
		return pad(m.rightPane(width, height), height, width)
	}
	lw := m.leftWidth()
	right := m.rightPane(width-lw-3, height)
	out := make([]string, height)
	sep := cFaint.Render(" │ ")
	for i := range out {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out[i] = fit(l, lw) + sep + r
	}
	return out
}

func pad(lines []string, height, width int) []string {
	out := make([]string, height)
	for i := range out {
		if i < len(lines) {
			out[i] = fit(lines[i], width)
		}
	}
	return out
}

func (m *helpModel) leftPane(height int) []string {
	prompt := cAccent.Render("› ") + m.query
	if m.mode == modeList {
		prompt += cAccent.Render("▏")
	}
	if m.query == "" && m.mode == modeList {
		prompt += cFaint.Render("type to search")
	}
	lines := []string{prompt}
	if len(m.list) == 0 {
		msg := "No command matches. Backspace to widen."
		if m.tabs[m.tab].Group == "*" && m.query == "" {
			msg = "Nothing saved yet. Open a command and press f."
		}
		return append(lines, cFaint.Render(msg))
	}
	rows := height - 1
	for i := m.top; i < min(m.top+rows, len(m.list)); i++ {
		c := m.list[i]
		mark := " "
		if m.store.isFavorite(c.Path) {
			mark = "★"
		}
		name := mark + padRight(c.Path, 20)
		switch {
		case i == m.sel && m.mode == modeList:
			lines = append(lines, cAccent.Render("›"+name)+" "+c.Summary)
		case i == m.sel:
			lines = append(lines, cAccent.Render(" "+name)+" "+cDim.Render(c.Summary))
		default:
			lines = append(lines, " "+name+" "+cDim.Render(c.Summary))
		}
	}
	return lines
}

func (m *helpModel) rightPane(width, height int) []string {
	c, ok := m.current()
	if !ok {
		return nil
	}
	lines, exStart := m.preview(c, max(width, 20))
	if m.mode != modeList && len(exStart) > 0 {
		// Keep the chosen example in view.
		at := exStart[min(m.example, len(exStart)-1)]
		if at < m.scroll {
			m.scroll = at
		}
		if at >= m.scroll+height-1 {
			m.scroll = at - height + 2
		}
	}
	m.scroll = min(max(m.scroll, 0), max(len(lines)-height, 0))
	end := min(m.scroll+height, len(lines))
	view := lines[m.scroll:end]
	if m.mode == modeEdit || m.mode == modeConfirm {
		view = append(append([]string{}, view...), "")
	}
	return view
}

// preview builds the right pane and the first line of each example.
func (m *helpModel) preview(c RefCommand, width int) ([]string, []int) {
	var lines []string
	lines = append(lines, cTitle.Render("clavis "+c.Path), c.Summary)
	if len(c.Aliases) > 0 {
		lines = append(lines, cFaint.Render("also: "+strings.Join(c.Aliases, ", ")))
	}
	lines = append(lines, "", cCode.Render(c.Usage))
	if c.Description != "" && c.Description != c.Summary {
		for _, p := range strings.Split(c.Description, "\n\n") {
			lines = append(lines, "")
			lines = append(lines, wrapText(plainMarkdown(m.out, p), width)...)
		}
	}
	var starts []int
	if len(c.Examples) > 0 {
		lines = append(lines, "", cDim.Render("EXAMPLES"))
		for i, e := range c.Examples {
			if e.Title != "" {
				lines = append(lines, cFaint.Render("  # "+e.Title))
			}
			starts = append(starts, len(lines))
			for j, l := range strings.Split(e.Run, "\n") {
				num := " "
				if j == 0 && i < 9 {
					num = fmt.Sprint(i + 1)
				}
				marker := "  "
				style := cCode
				if m.mode != modeList && i == m.example {
					marker = cAccent.Render("▶ ")
					style = cAccent
				}
				lines = append(lines, cFaint.Render(num)+" "+marker+style.Render(l))
			}
		}
	}
	if len(c.Subcommands) > 0 {
		lines = append(lines, "", cDim.Render("COMMANDS"))
		for _, s := range c.Subcommands {
			lines = append(lines, "  "+cAccent.Render(strings.TrimPrefix(s, c.Path+" ")))
		}
	}
	if len(c.Flags) > 0 {
		lines = append(lines, "", cDim.Render("FLAGS"))
		for _, f := range c.Flags {
			lines = append(lines, "  "+cCode.Render(padRight("--"+f.Name, 22))+f.Usage)
		}
	}
	if len(c.Related) > 0 {
		lines = append(lines, "", cDim.Render("SEE ALSO  ")+strings.Join(c.Related, " · "))
	}
	return lines, starts
}

func (m *helpModel) footer() string {
	switch m.mode {
	case modeEdit:
		return cAccent.Render("run › ") + m.edit + cAccent.Render("▏") + cFaint.Render("   Enter run · Esc back · Ctrl-U clear")
	case modeConfirm:
		return cWarn.Render("This changes real data. Type yes, then Enter: ") + m.confirm + cAccent.Render("▏")
	case modeDetail:
		keys := "↑↓ example · 1-9 pick · c copy · r run · f ★ · PgUp/PgDn scroll · Esc back"
		if m.status != "" {
			return cAccent.Render(m.status) + cFaint.Render("  ·  Esc back")
		}
		return cFaint.Render(keys)
	}
	return cFaint.Render("type to search · ←→ groups · ↑↓ move · Enter open · Esc quit")
}

// ---- running and copying ----

// copyToClipboard uses the system clipboard tool when there is one, and the
// OSC 52 terminal sequence otherwise (works over SSH in most terminals).
func copyToClipboard(text string) error {
	tools := [][]string{{"pbcopy"}, {"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}, {"clip"}}
	for _, tool := range tools {
		path, err := exec.LookPath(tool[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, tool[1:]...) //nolint:gosec // G204: fixed list of clipboard tools
		cmd.Stdin = strings.NewReader(text)
		if cmd.Run() == nil {
			return nil
		}
	}
	_, err := fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\x07", base64.StdEncoding.EncodeToString([]byte(text)))
	return err
}

// shellRun runs a line from the help screen with the terminal attached. A
// leading "clavis" means this very binary, so it works before installing.
var shellRun = func(line string) error {
	if exe, err := os.Executable(); err == nil && (line == "clavis" || strings.HasPrefix(line, "clavis ")) {
		line = quoteExe(exe) + strings.TrimPrefix(line, "clavis")
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", line) //nolint:gosec // G204: the user typed or confirmed this line
	} else {
		cmd = exec.Command("sh", "-c", line) //nolint:gosec // G204: the user typed or confirmed this line
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func quoteExe(s string) string {
	if runtime.GOOS == "windows" {
		return `"` + s + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// parseInput turns one raw terminal read into events: typed text, special
// keys, and SGR mouse reports.
func parseInput(b []byte) []any {
	var out []any
	key := func(name string) { out = append(out, helpKey{Name: name}); b = b[1:] }
	for len(b) > 0 {
		switch c := b[0]; {
		case c == 27 && len(b) >= 2 && (b[1] == '[' || b[1] == 'O'):
			end := 2
			for end < len(b) && (b[end] < 0x40 || b[end] > 0x7e) {
				end++
			}
			end = min(end+1, len(b))
			seq := string(b[2:end])
			b = b[end:]
			out = append(out, decodeCSI(seq)...)
		case c == 27:
			key("esc")
		case c == 3 || c == 4:
			key("ctrl+c")
		case c == 13 || c == 10:
			key("enter")
		case c == 9:
			key("tab")
		case c == 127 || c == 8:
			key("backspace")
		case c == 16:
			key("ctrl+p")
		case c == 14:
			key("ctrl+n")
		case c == 21:
			key("ctrl+u")
		case c == 23:
			key("ctrl+w")
		case c < 32:
			b = b[1:]
		default:
			r, size := utf8.DecodeRune(b)
			if r != utf8.RuneError {
				out = append(out, helpKey{Text: string(r)})
			}
			b = b[size:]
		}
	}
	return out
}

// decodeCSI handles what follows "ESC [": arrows, paging, Shift-Tab and
// mouse reports ("<0;12;5M").
func decodeCSI(seq string) []any {
	if seq == "" {
		return nil
	}
	final := seq[len(seq)-1]
	switch {
	case seq[0] == '<' && final == 'M':
		parts := strings.Split(seq[1:len(seq)-1], ";")
		if len(parts) != 3 {
			return nil
		}
		code, _ := strconv.Atoi(parts[0])
		x, _ := strconv.Atoi(parts[1])
		y, _ := strconv.Atoi(parts[2])
		button := map[int]string{0: "left", 64: "wheelup", 65: "wheeldown"}[code]
		if button == "" {
			return nil
		}
		return []any{helpMouse{X: x - 1, Y: y - 1, Button: button}}
	case seq == "5~":
		return []any{helpKey{Name: "pgup"}}
	case seq == "6~":
		return []any{helpKey{Name: "pgdown"}}
	}
	names := map[byte]string{'A': "up", 'B': "down", 'C': "right", 'D': "left", 'Z': "shift+tab"}
	if name, ok := names[final]; ok && len(seq) == 1 {
		return []any{helpKey{Name: name}}
	}
	return nil
}

// runHelpScreen shows the screen on the terminal's alternate buffer and, if
// the user chose to run something, runs it after the screen has closed. It
// talks to the terminal directly, so ordinary commands never pay for a
// terminal library's start-up queries.
func runHelpScreen(opts *Options, ref *Reference, out *Output) error {
	model := newHelpModel(ref, loadHelpStore(), currentHelpContext(opts))
	stdin, stdout := int(os.Stdin.Fd()), int(os.Stdout.Fd())
	saved, err := term.MakeRaw(stdin)
	if err != nil {
		return err
	}
	size := func() (int, int) {
		if w, h, err := term.GetSize(stdout); err == nil && w > 0 && h > 0 {
			return w, h
		}
		return 100, 30
	}
	model.width, model.height = size()
	// Alternate screen, hidden cursor, mouse clicks and wheel (SGR reports).
	_, _ = fmt.Fprint(os.Stdout, "\x1b[?1049h\x1b[?25l\x1b[?1000h\x1b[?1006h")
	restore := func() {
		_, _ = fmt.Fprint(os.Stdout, "\x1b[?1006l\x1b[?1000l\x1b[?25h\x1b[?1049l")
		_ = term.Restore(stdin, saved)
	}

	events := make(chan any, 16)
	stop := make(chan struct{})
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				close(events)
				return
			}
			for _, event := range parseInput(buf[:n]) {
				select {
				case events <- event:
				case <-stop:
					return
				}
			}
		}
	}()
	go func() {
		w, h := model.width, model.height
		for {
			select {
			case <-stop:
				return
			case <-time.After(250 * time.Millisecond):
				if nw, nh := size(); nw != w || nh != h {
					w, h = nw, nh
					select {
					case events <- helpResize{nw, nh}:
					case <-stop:
						return
					}
				}
			}
		}
	}()

	draw := func() {
		view := strings.ReplaceAll(model.View(), "\n", "\x1b[K\r\n")
		_, _ = fmt.Fprint(os.Stdout, "\x1b[H"+view+"\x1b[K\x1b[J")
	}
	draw()
	for event := range events {
		if model.Update(event) {
			break
		}
		draw()
	}
	close(stop)
	restore()

	if model.run == "" {
		return nil
	}
	_, _ = fmt.Fprintf(out.Out, "%s %s\n", out.style("$", helpFaint), out.style(model.run, helpCode))
	if err := shellRun(model.run); err != nil {
		code := ExitGeneral
		var exit *exec.ExitError
		if asExit(err, &exit) {
			code = exit.ExitCode()
		}
		return &ExitError{Code: code, Err: err, Silent: true}
	}
	return nil
}

func asExit(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError) //nolint:errorlint // exec returns it unwrapped
	if ok {
		*target = e
	}
	return ok
}
