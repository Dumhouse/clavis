package cli

import (
	"bytes"
	"strings"
	"testing"
)

func testReference(t *testing.T) *Reference {
	t.Helper()
	ref, err := BuildReference(NewRootCommand())
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestHelpOverviewListsEveryGroup(t *testing.T) {
	ref := testReference(t)
	var out bytes.Buffer
	renderOverview(&out, NewTestOutput(&out, &out, false, false), ref)
	for _, want := range []string{"START HERE", "clavis doctor", "GET STARTED", "SERVER AND ADMINISTRATION", "clavis help -s WORD"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("overview lacks %q:\n%s", want, out.String())
		}
	}
}

func TestHelpCommandPage(t *testing.T) {
	ref := testReference(t)
	var out bytes.Buffer
	if err := renderCommandHelp(&out, NewTestOutput(&out, &out, false, false), ref, "sets use"); err != nil {
		t.Fatal(err)
	}
	page := out.String()
	for _, want := range []string{"clavis sets use", "Also: clavis sets import", "USAGE", "EXAMPLES", "$ clavis sets use shared-api --env staging", "reference/sets-use"} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "](") || strings.Contains(page, "**") {
		t.Errorf("markdown left in page:\n%s", page)
	}
}

func TestRankCommands(t *testing.T) {
	ref := testReference(t)
	first := func(query string) string {
		matches := rankCommands(ref.Commands, query)
		if len(matches) == 0 {
			return ""
		}
		return matches[0].Path
	}
	cases := map[string]string{
		"secrets set":       "secrets set",
		"clavis upgrade":    "upgrade",
		"doc":               "doctor",
		"rotate credential": "tokens rotate",
	}
	for query, want := range cases {
		if got := first(query); got != want {
			t.Errorf("%q → %q, want %q", query, got, want)
		}
	}
	found := false
	for _, command := range rankCommands(ref.Commands, "sst") {
		found = found || command.Path == "secrets set"
	}
	if !found {
		t.Error("fuzzy sst should find secrets set")
	}
}

func key(m *helpModel, keys ...string) {
	for _, k := range keys {
		switch k {
		case "enter", "esc", "down", "right", "backspace":
			m.Update(helpKey{Name: k})
		default:
			m.Update(helpKey{Text: k})
		}
	}
}

func testModel(t *testing.T) *helpModel {
	t.Helper()
	path := t.TempDir() + "/help.json"
	old := helpStorePath
	helpStorePath = func() string { return path }
	t.Cleanup(func() { helpStorePath = old })
	return newHelpModel(testReference(t), loadHelpStore(), helpContext{Server: "https://x"})
}

func TestHelpScreenSearchOpenCopy(t *testing.T) {
	m := testModel(t)
	var copied string
	old := clipboardCopy
	clipboardCopy = func(s string) error { copied = s; return nil }
	t.Cleanup(func() { clipboardCopy = old })

	key(m, "u", "p", "g", "r")
	if c, _ := m.current(); c.Path != "upgrade" {
		t.Fatalf("search → %q", c.Path)
	}
	key(m, "enter")
	if m.mode != modeDetail {
		t.Fatal("enter should open the command")
	}
	key(m, "2", "c")
	if copied != "clavis upgrade --yes" {
		t.Fatalf("copied %q", copied)
	}
	key(m, "f")
	if !m.store.isFavorite("upgrade") {
		t.Fatal("f should save the command")
	}
	if got := loadHelpStore(); !got.isFavorite("upgrade") || got.Opened["upgrade"] != 1 {
		t.Fatalf("store not persisted: %+v", got)
	}
	if view := m.View(); !strings.Contains(view, "EXAMPLES") || !strings.Contains(view, "▶") || !strings.Contains(view, "saved upgrade") {
		t.Fatalf("view:\n%s", view)
	}
}

func TestHelpScreenRunAndConfirm(t *testing.T) {
	m := testModel(t)
	key(m, "d", "o", "c", "t", "o", "r", "enter", "r", "enter")
	if m.run != "clavis doctor" {
		t.Fatalf("run %q (mode %d)", m.run, m.mode)
	}

	m = testModel(t)
	key(m, "e", "n", "v", " ", "f", "r", "e", "e", "z", "e", "enter", "r")
	if m.mode != modeEdit || !strings.Contains(m.edit, "freeze") {
		t.Fatalf("edit mode: %d %q", m.mode, m.edit)
	}
	m.edit = "clavis env freeze --env production"
	key(m, "enter")
	if m.mode != modeConfirm || m.run != "" {
		t.Fatalf("production must ask first: mode %d run %q", m.mode, m.run)
	}
	key(m, "y", "e", "s", "enter")
	if m.run != "clavis env freeze --env production" {
		t.Fatalf("run %q", m.run)
	}
}

func TestHelpScreenTabsAndLayout(t *testing.T) {
	m := testModel(t)
	key(m, "right", "right")
	if m.tabs[m.tab].Group != "get-started" {
		t.Fatalf("tab %+v", m.tabs[m.tab])
	}
	for _, c := range m.list {
		if c.Group != "get-started" {
			t.Fatalf("%s is in %s", c.Path, c.Group)
		}
	}
	for _, size := range [][2]int{{120, 40}, {70, 24}} {
		m.Update(helpResize{W: size[0], H: size[1]})
		if view := m.View(); !strings.Contains(view, "Clavis help") {
			t.Fatalf("%v view:\n%s", size, view)
		}
	}
	if !strings.Contains(m.contextLine(), "not signed in") {
		t.Fatalf("header %q", m.contextLine())
	}
}

func TestExampleLineJoinsContinuations(t *testing.T) {
	if got := exampleLine("clavis a \\\n  --b c"); got != "clavis a --b c" {
		t.Fatalf("got %q", got)
	}
}

func TestParseInput(t *testing.T) {
	events := parseInput([]byte("a\x1b[B\x1b[5~\x1b[<64;10;5M\x1b[<0;3;3M\r\x1b"))
	want := []any{
		helpKey{Text: "a"}, helpKey{Name: "down"}, helpKey{Name: "pgup"},
		helpMouse{X: 9, Y: 4, Button: "wheelup"}, helpMouse{X: 2, Y: 2, Button: "left"},
		helpKey{Name: "enter"}, helpKey{Name: "esc"},
	}
	if len(events) != len(want) {
		t.Fatalf("events %#v", events)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Errorf("event %d = %#v, want %#v", i, events[i], want[i])
		}
	}
	if got := parseInput([]byte("é")); len(got) != 1 || got[0] != (helpKey{Text: "é"}) {
		t.Fatalf("utf8 %#v", got)
	}
}

func TestHelpScreenMouse(t *testing.T) {
	m := testModel(t)
	m.Update(helpMouse{X: 5, Y: 7, Button: "left"}) // third list row
	if m.sel != 2 {
		t.Fatalf("click selected %d", m.sel)
	}
	m.View()
	m.Update(helpMouse{X: 20, Y: 2, Button: "left"}) // a tab
	if m.tab == 0 {
		t.Fatal("tab click ignored")
	}
}

func TestTourRunsOnlyWhenAsked(t *testing.T) {
	var ran []string
	steps := []tourStep{
		{Title: "one", Command: "clavis one"},
		{Title: "two", Command: "clavis two", Check: func() (bool, string) { return false, "do two by hand" }},
		{Title: "three", Command: "clavis three"},
	}
	var out bytes.Buffer
	// Run one; run two (check fails) then skip; skip three.
	in := strings.NewReader("\n\ns\ns\n")
	err := runTour(in, NewTestOutput(&out, &out, false, false), steps, func(line string) error {
		ran = append(ran, line)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ran, ",") != "clavis one,clavis two" {
		t.Fatalf("ran %v", ran)
	}
	for _, want := range []string{"1/3", "✓ done", "✗ not yet: do two by hand", "1 of 3 steps done"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q:\n%s", want, out.String())
		}
	}

	ran, out = nil, bytes.Buffer{}
	_ = runTour(strings.NewReader("q\n"), NewTestOutput(&out, &out, false, false), steps, func(l string) error { ran = append(ran, l); return nil })
	if len(ran) != 0 || !strings.Contains(out.String(), "Stopped after 0 of 3") {
		t.Fatalf("quit ran %v:\n%s", ran, out.String())
	}
}
