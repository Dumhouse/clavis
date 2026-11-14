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
		"rotate credential": "connections rotate",
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

func TestFinderKeys(t *testing.T) {
	s := newFinderState(testReference(t).Commands)
	for _, r := range "upgr" {
		s.handle(keyRune, r)
	}
	if s.matches[0].Path != "upgrade" {
		t.Fatalf("first match %q", s.matches[0].Path)
	}
	s.handle(keyBackspace, 0)
	if s.query != "upg" {
		t.Fatalf("query %q", s.query)
	}
	s.handle(keyClear, 0)
	s.handle(keyDown, 0)
	s.handle(keyDown, 0)
	if s.selected != 2 {
		t.Fatalf("selected %d", s.selected)
	}
	s.handle(keyUp, 0)
	picked, done := s.handle(keyEnter, 0)
	if !done || picked != s.matches[1].Path {
		t.Fatalf("enter picked %q done=%v", picked, done)
	}
	if _, done := s.handle(keyQuit, 0); !done {
		t.Fatal("quit")
	}
	if key, _ := decodeKey([]byte{27, '[', 'A'}); key != keyUp {
		t.Fatal("arrow up")
	}
	if keys := splitKeys([]byte("up\x1b[Bé\r")); len(keys) != 5 || string(keys[2]) != "\x1b[B" || string(keys[3]) != "é" {
		t.Fatalf("split %q", keys)
	}
	if key, _ := decodeKey([]byte{27}); key != keyQuit {
		t.Fatal("esc")
	}
	var out bytes.Buffer
	view := strings.Join(s.view(NewTestOutput(&out, &out, false, false), 100), "\n")
	if !strings.Contains(view, "Enter open") || !strings.Contains(view, "clavis help ›") {
		t.Fatalf("view:\n%s", view)
	}
}
