package cli

import (
	"strings"
	"testing"
)

func TestPromptSegment(t *testing.T) {
	if got := promptSegment("web", "dev"); got != "web:dev" {
		t.Fatalf("got %q", got)
	}
	if got := promptSegment("web", "production"); got != "web:PRODUCTION!" {
		t.Fatalf("got %q", got)
	}
	if got := promptSegment("", "dev"); got != "clavis:dev" {
		t.Fatalf("got %q", got)
	}
}

func TestPromptSnippets(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		snippet, err := promptSnippet(shell)
		if err != nil || !strings.Contains(snippet, "clavis prompt") {
			t.Fatalf("%s: %q %v", shell, snippet, err)
		}
	}
	if _, err := promptSnippet("csh"); err == nil {
		t.Fatal("unknown shell should fail")
	}
}
