package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReferenceCoversEveryCommand(t *testing.T) {
	root := NewRootCommand()
	ref, err := BuildReference(root)
	if err != nil {
		t.Fatal(err)
	}
	overlay, err := loadOverlay()
	if err != nil {
		t.Fatal(err)
	}

	for _, command := range ref.Commands {
		if command.Group == "" {
			t.Errorf("%s: top-level command %q is in no group in reference.yaml", command.Path, strings.SplitN(command.Path, " ", 2)[0])
		}
		if command.Summary == "" {
			t.Errorf("%s: missing Short", command.Path)
		}
		if command.Description == "" {
			t.Errorf("%s: no description in reference.yaml (or Long)", command.Path)
		}
		for _, related := range command.Related {
			if ref.Command(related) == nil {
				t.Errorf("%s: related command %q does not exist", command.Path, related)
			}
		}
	}
	for path := range overlay.Commands {
		if ref.Command(path) == nil {
			t.Errorf("reference.yaml documents %q, which is not a command", path)
		}
	}
	for _, group := range ref.Groups {
		for _, path := range group.Commands {
			command := ref.Command(path)
			if command == nil || command.Parent != "" {
				t.Errorf("group %s lists %q, which is not a top-level command", group.ID, path)
			}
		}
	}
}

// Every flag used in an example must exist, so examples can't rot when a
// flag is renamed.
func TestReferenceExamplesUseRealFlags(t *testing.T) {
	ref, err := BuildReference(NewRootCommand())
	if err != nil {
		t.Fatal(err)
	}
	var examples []RefExample
	for _, command := range ref.Commands {
		examples = append(examples, command.Examples...)
	}
	for _, topic := range ref.Topics {
		examples = append(examples, topic.Examples...)
	}
	checked := 0
	for _, example := range examples {
		line := strings.ReplaceAll(example.Run, "\\\n", " ")
		for _, single := range strings.Split(line, "\n") {
			single = strings.TrimSpace(single)
			if pipe := strings.IndexAny(single, "|>"); pipe >= 0 {
				single = single[:pipe]
			}
			// completion is added by cobra at execution time.
			if !strings.HasPrefix(single, "clavis ") || strings.HasPrefix(single, "clavis completion ") {
				continue
			}
			args, err := splitCommandLine(single)
			if err != nil {
				t.Errorf("%q: %v", single, err)
				continue
			}
			root := NewRootCommand()
			cmd, rest, err := root.Find(args[1:])
			if err != nil {
				t.Errorf("%q: %v", single, err)
				continue
			}
			if err := cmd.ParseFlags(rest); err != nil {
				t.Errorf("%q: %v", single, err)
			}
			checked++
		}
	}
	if checked < 100 {
		t.Fatalf("only %d examples checked", checked)
	}
}

func TestReferenceExamplesInHelp(t *testing.T) {
	root := NewRootCommand()
	cmd, _, err := root.Find([]string{"secrets", "set"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cmd.Example, "clavis secrets set DATABASE_URL") {
		t.Fatalf("example not applied to --help:\n%s", cmd.Example)
	}
}

// The generated docs must match the command tree. Run `make docs-cli` when
// this fails.
func TestReferenceDocsUpToDate(t *testing.T) {
	ref, err := BuildReference(NewRootCommand())
	if err != nil {
		t.Fatal(err)
	}
	files, err := ReferenceFiles(ref)
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join("..", "..")
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s is out of date; run `make docs-cli`", name)
		}
	}
	entries, err := os.ReadDir(filepath.Join(repo, "docs", "reference"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if _, ok := files["docs/reference/"+entry.Name()]; !ok {
			t.Errorf("docs/reference/%s is not generated any more; run `make docs-cli`", entry.Name())
		}
	}
}
