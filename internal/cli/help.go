package cli

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/dumhouse/clavis/internal/version"
)

/*
 * `clavis help` and `--help` are rendered from the same reference as the
 * docs site (the command tree plus reference.yaml): a grouped overview, a
 * page per command with examples, and, in a terminal, a finder you type into.
 */

const docsReferenceURL = "https://dumhouse.github.io/clavis/reference/"

// helpColors are the accents used by help pages (the Output palette).
const (
	helpTitle  = "f4f4f5"
	helpAccent = "34d399"
	helpCode   = "93c5fd"
	helpDim    = "9ca3af"
	helpFaint  = "6b7280"
)

// startHere is what a new user needs first, in order.
var startHere = [][2]string{
	{"clavis login", "Sign in with your browser"},
	{"clavis init", "Link this folder to a project"},
	{"clavis run -- npm run dev", "Run your app with its secrets"},
	{"clavis doctor", "Check the setup when something doesn't work"},
}

func newHelpCommand(opts *Options) *cobra.Command {
	var search string
	cmd := &cobra.Command{
		Use:   "help [COMMAND]",
		Short: "Find a command and see how to use it",
		Long: "Without arguments in a terminal, opens a finder: type to filter, " +
			"↑/↓ to move, Enter to open. `clavis help COMMAND` shows one command; " +
			"`--search WORD` lists every command that mentions WORD.",
		RunE: func(cmd *cobra.Command, args []string) error {
			root := cmd.Root()
			ref, err := BuildReference(root)
			if err != nil {
				return err
			}
			out := opts.output()
			w := cmd.OutOrStdout()
			query := strings.TrimSpace(search)
			if query == "" && len(args) > 0 {
				if found, rest, err := root.Find(args); err == nil && found != root && len(rest) == 0 {
					return renderCommandHelp(w, out, ref, commandPath(found))
				}
				query = strings.Join(args, " ")
			}
			if query != "" {
				return renderSearch(w, out, ref, query)
			}
			if !opts.JSON && interactiveTerminal() {
				picked, err := runFinder(ref, out)
				if err != nil || picked == "" {
					return err
				}
				return renderCommandHelp(w, out, ref, picked)
			}
			renderOverview(w, out, ref)
			return nil
		},
	}
	cmd.Flags().StringVarP(&search, "search", "s", "", "list commands that mention this word")
	return cmd
}

// installHelp makes --help, `clavis` alone and `clavis help` use the
// reference-based pages.
func installHelp(root *cobra.Command, opts *Options) {
	root.SetHelpCommand(newHelpCommand(opts))
	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		ref, err := BuildReference(cmd.Root())
		if err != nil {
			defaultHelp(cmd, args)
			return
		}
		out := opts.output()
		if cmd == cmd.Root() {
			renderOverview(cmd.OutOrStdout(), out, ref)
			return
		}
		if err := renderCommandHelp(cmd.OutOrStdout(), out, ref, commandPath(cmd)); err != nil {
			defaultHelp(cmd, args)
		}
	})
}

func interactiveTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("TERM") != "dumb"
}

func helpWidth() int {
	if width, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && width > 40 {
		return min(width, 100)
	}
	return 90
}

func findRefCommand(ref *Reference, path string) (RefCommand, bool) {
	for _, command := range ref.Commands {
		if command.Path == path {
			return command, true
		}
	}
	return RefCommand{}, false
}

// renderOverview is `clavis`, `clavis --help` and `clavis help` outside a
// terminal: what to run first, then every command by group.
func renderOverview(w io.Writer, out *Output, ref *Reference) {
	p := func(format string, args ...any) { _, _ = fmt.Fprintf(w, format, args...) }
	p("%s %s  %s\n\n", out.style("Clavis", helpAccent), out.style("— open-source secrets management", helpTitle), out.style(version.Version, helpFaint))

	p("%s\n", out.style("START HERE", helpDim))
	for _, line := range startHere {
		p("  %s %s\n", out.style(padRight(line[0], 28), helpCode), line[1])
	}

	summaries := map[string]string{}
	for _, command := range ref.Commands {
		summaries[command.Path] = command.Summary
	}
	for _, group := range ref.Groups {
		p("\n%s\n", out.style(strings.ToUpper(group.Title), helpDim))
		for _, path := range group.Commands {
			p("  %s %s\n", out.style(padRight(path, 14), helpAccent), summaries[path])
		}
	}

	p("\n%s\n", out.style("MORE HELP", helpDim))
	p("  %s %s\n", out.style(padRight("clavis help", 28), helpCode), "find a command by typing (in a terminal)")
	p("  %s %s\n", out.style(padRight("clavis help COMMAND", 28), helpCode), "usage, examples and flags for one command")
	p("  %s %s\n", out.style(padRight("clavis help -s WORD", 28), helpCode), "every command that mentions WORD")
	p("  %s %s\n", out.style(padRight("clavis COMMAND --help", 28), helpCode), "same as clavis help COMMAND")
	p("  %s\n", out.style("Docs: "+docsReferenceURL, helpFaint))
}

// renderCommandHelp is one command's page.
func renderCommandHelp(w io.Writer, out *Output, ref *Reference, path string) error {
	command, ok := findRefCommand(ref, path)
	if !ok {
		return fmt.Errorf("no command %q", path)
	}
	width := helpWidth()
	p := func(format string, args ...any) { _, _ = fmt.Fprintf(w, format, args...) }
	heading := func(title string) { p("\n%s\n", out.style(strings.ToUpper(title), helpDim)) }

	p("%s  %s\n", out.style("clavis "+command.Path, helpAccent), command.Summary)
	if len(command.Aliases) > 0 {
		names := make([]string, len(command.Aliases))
		for i, alias := range command.Aliases {
			names[i] = strings.TrimSpace(strings.TrimSuffix("clavis "+command.Parent, " ") + " " + alias)
			names[i] = strings.Join(strings.Fields(names[i]), " ")
		}
		p("%s\n", out.style("Also: "+strings.Join(names, ", "), helpFaint))
	}

	heading("Usage")
	p("  %s\n", out.style(command.Usage, helpCode))

	if command.Description != "" && command.Description != command.Summary {
		for _, paragraph := range strings.Split(command.Description, "\n\n") {
			p("\n")
			for _, line := range wrapText(plainMarkdown(out, paragraph), width-2) {
				p("  %s\n", line)
			}
		}
	}

	if len(command.Examples) > 0 {
		heading("Examples")
		for i, example := range command.Examples {
			if i > 0 {
				p("\n")
			}
			if example.Title != "" {
				p("  %s\n", out.style("# "+example.Title, helpFaint))
			}
			for j, line := range strings.Split(example.Run, "\n") {
				prompt := "$ "
				if j > 0 {
					prompt = "  "
				}
				p("  %s%s\n", out.style(prompt, helpFaint), out.style(line, helpCode))
			}
		}
	}

	if len(command.Subcommands) > 0 {
		heading("Commands")
		for _, sub := range command.Subcommands {
			child, _ := findRefCommand(ref, sub)
			name := strings.TrimPrefix(sub, command.Path+" ")
			p("  %s %s\n", out.style(padRight(name, 16), helpAccent), child.Summary)
		}
	}

	if len(command.Flags) > 0 {
		heading("Flags")
		for _, flag := range command.Flags {
			name := "--" + flag.Name
			if flag.Shorthand != "" {
				name = "-" + flag.Shorthand + ", " + name
			}
			if flag.Type != "" {
				name += " " + flag.Type
			}
			usage := flag.Usage
			if flag.Default != "" {
				usage += out.style(" (default "+flag.Default+")", helpFaint)
			}
			p("  %s %s\n", out.style(padRight(name, 26), helpCode), usage)
		}
	}
	if command.Notes != "" {
		heading("Notes")
		for _, line := range wrapText(plainMarkdown(out, command.Notes), width-2) {
			p("  %s\n", line)
		}
	}
	if len(command.Related) > 0 {
		related := make([]string, len(command.Related))
		for i, name := range command.Related {
			related[i] = "clavis " + name
		}
		heading("See also")
		p("  %s\n", strings.Join(related, " · "))
	}
	p("\n%s\n", out.style("Global flags: --json --server --org --project --env --debug  ·  "+docsReferenceURL+command.Slug, helpFaint))
	return nil
}

// renderSearch lists commands matching a query, best first.
func renderSearch(w io.Writer, out *Output, ref *Reference, query string) error {
	matches := rankCommands(ref.Commands, query)
	if len(matches) == 0 {
		_, _ = fmt.Fprintf(w, "No command mentions %q. Try %s, or see %s\n", query, out.style("clavis help", helpCode), docsReferenceURL)
		return &ExitError{Code: ExitNotFound, Err: fmt.Errorf("no match for %q", query), Silent: true}
	}
	_, _ = fmt.Fprintf(w, "%s\n", out.style(fmt.Sprintf("Commands matching %q", query), helpDim))
	for _, command := range matches[:min(len(matches), 15)] {
		_, _ = fmt.Fprintf(w, "  %s %s\n", out.style(padRight(command.Path, 22), helpAccent), command.Summary)
	}
	_, _ = fmt.Fprintf(w, "\n%s\n", out.style("clavis help COMMAND shows one of them.", helpFaint))
	return nil
}

// rankCommands orders commands by how well they match query: the command
// name first, then words in its summary, aliases and description, then a
// fuzzy match of the letters in order (so "sst" also finds "secrets set").
func rankCommands(commands []RefCommand, query string) []RefCommand {
	query = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(query), "clavis ")))
	if query == "" {
		return commands
	}
	type scored struct {
		command RefCommand
		score   int
	}
	var hits []scored
	for _, command := range commands {
		path := strings.ToLower(command.Path)
		text := strings.ToLower(command.Path + " " + command.Summary + " " + strings.Join(command.Aliases, " "))
		score := 0
		switch {
		case path == query:
			score = 1000
		case strings.HasPrefix(path, query):
			score = 800
		case strings.Contains(path, query):
			score = 600
		case containsAll(text, query):
			score = 400
		case containsAll(strings.ToLower(command.Description), query):
			score = 200
		case subsequence(strings.ReplaceAll(path, " ", ""), strings.ReplaceAll(query, " ", "")):
			score = 100
		}
		if score > 0 {
			// Prefer shorter, top-level commands among equals.
			hits = append(hits, scored{command, score - len(path)})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	out := make([]RefCommand, len(hits))
	for i, hit := range hits {
		out[i] = hit.command
	}
	return out
}

func containsAll(text, query string) bool {
	for _, word := range strings.Fields(query) {
		if !strings.Contains(text, word) {
			return false
		}
	}
	return true
}

func subsequence(text, query string) bool {
	i := 0
	for _, r := range text {
		if i < len(query) && byte(r) == query[i] {
			i++
		}
	}
	return i == len(query)
}

var (
	mdLink = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	mdBold = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	mdCode = regexp.MustCompile("`([^`]+)`")
)

// plainMarkdown turns the reference's light markdown into terminal text:
// links keep their text, bold markers go, code is colored.
func plainMarkdown(out *Output, text string) string {
	text = mdLink.ReplaceAllString(text, "$1")
	text = mdBold.ReplaceAllString(text, "$1")
	return mdCode.ReplaceAllStringFunc(text, func(match string) string {
		return out.style(strings.Trim(match, "`"), helpCode)
	})
}

// wrapText re-flows a paragraph to width, keeping list items on their own
// lines. Width counts visible characters, so colored words wrap correctly.
func wrapText(text string, width int) []string {
	// Join the YAML's hard-wrapped lines back into paragraphs and list items.
	var lines []string
	for _, raw := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(raw)
		switch {
		case trimmed == "":
			lines = append(lines, "")
		case strings.HasPrefix(trimmed, "- ") || len(lines) == 0 || lines[len(lines)-1] == "":
			lines = append(lines, trimmed)
		default:
			lines[len(lines)-1] += " " + trimmed
		}
	}
	var out []string
	for _, line := range lines {
		indent := ""
		if strings.HasPrefix(strings.TrimSpace(line), "- ") {
			indent = "  "
		}
		current := ""
		for _, word := range strings.Fields(line) {
			if current != "" && visibleLen(current)+1+visibleLen(word) > width {
				out = append(out, current)
				current = indent + word
				continue
			}
			if current == "" {
				current = word
			} else {
				current += " " + word
			}
		}
		out = append(out, current)
	}
	return out
}

var ansiCodes = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func visibleLen(s string) int {
	return len([]rune(ansiCodes.ReplaceAllString(s, "")))
}

func padRight(s string, width int) string {
	if n := visibleLen(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s + " "
}
