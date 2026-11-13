package cli

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

/*
 * The CLI reference has one source: the cobra command tree (usage, flags,
 * subcommands) plus reference.yaml (grouping, longer descriptions, titled
 * examples, notes, related commands and topic pages). `clavis --help`, the
 * public docs site and the in-app docs are all built from it, so they can't
 * drift apart. Regenerate the docs with `make docs-cli`.
 */

//go:embed reference.yaml
var referenceYAML []byte

// Reference is the full CLI reference, serialized to JSON for the web app and
// rendered to markdown for the docs site.
type Reference struct {
	Groups      []RefGroup   `json:"groups"`
	Commands    []RefCommand `json:"commands"`
	GlobalFlags []RefFlag    `json:"globalFlags"`
	Topics      []RefTopic   `json:"topics"`
}

// RefGroup is a sidebar section holding top-level commands.
type RefGroup struct {
	ID          string   `json:"id" yaml:"id"`
	Title       string   `json:"title" yaml:"title"`
	Description string   `json:"description" yaml:"description"`
	Commands    []string `json:"commands" yaml:"commands"`
}

// RefCommand documents one command, top-level or nested.
type RefCommand struct {
	Path        string       `json:"path"`
	Slug        string       `json:"slug"`
	Parent      string       `json:"parent,omitempty"`
	Group       string       `json:"group"`
	Usage       string       `json:"usage"`
	Summary     string       `json:"summary"`
	Description string       `json:"description"`
	Examples    []RefExample `json:"examples"`
	Notes       string       `json:"notes,omitempty"`
	Flags       []RefFlag    `json:"flags"`
	Subcommands []string     `json:"subcommands"`
	Related     []string     `json:"related"`
	Aliases     []string     `json:"aliases,omitempty"`
}

// RefExample is one runnable example with an optional title.
type RefExample struct {
	Title string `json:"title,omitempty" yaml:"title"`
	Run   string `json:"run" yaml:"run"`
}

// RefFlag documents one flag.
type RefFlag struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand,omitempty"`
	Type      string `json:"type,omitempty"`
	Default   string `json:"default,omitempty"`
	Usage     string `json:"usage"`
}

// RefTopic is a reference page that is not a command (config file, exit
// codes, environment variables, ...).
type RefTopic struct {
	ID       string       `json:"id" yaml:"id"`
	Title    string       `json:"title" yaml:"title"`
	Summary  string       `json:"summary" yaml:"summary"`
	Body     string       `json:"body" yaml:"body"`
	Examples []RefExample `json:"examples,omitempty" yaml:"examples"`
}

type referenceOverlay struct {
	Groups   []RefGroup                `yaml:"groups"`
	Commands map[string]commandOverlay `yaml:"commands"`
	Topics   []RefTopic                `yaml:"topics"`
}

type commandOverlay struct {
	Description string       `yaml:"description"`
	Examples    []RefExample `yaml:"examples"`
	Notes       string       `yaml:"notes"`
	Related     []string     `yaml:"related"`
}

var (
	overlayOnce sync.Once
	overlayData referenceOverlay
	errOverlay  error
)

func loadOverlay() (referenceOverlay, error) {
	overlayOnce.Do(func() {
		errOverlay = yaml.Unmarshal(referenceYAML, &overlayData)
	})
	return overlayData, errOverlay
}

// commandPath is the command path without the leading "clavis ".
func commandPath(cmd *cobra.Command) string {
	return strings.TrimPrefix(strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()), " ")
}

func visibleChildren(cmd *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	for _, child := range cmd.Commands() {
		if child.Hidden || child.Name() == "help" || child.Name() == "completion" {
			continue
		}
		out = append(out, child)
	}
	return out
}

func walkCommands(cmd *cobra.Command, visit func(*cobra.Command)) {
	for _, child := range visibleChildren(cmd) {
		visit(child)
		walkCommands(child, visit)
	}
}

// applyReferenceExamples copies the reference examples into cobra so that
// `clavis <command> --help` shows them too.
func applyReferenceExamples(root *cobra.Command) {
	overlay, err := loadOverlay()
	if err != nil {
		return
	}
	walkCommands(root, func(cmd *cobra.Command) {
		entry, ok := overlay.Commands[commandPath(cmd)]
		if !ok || cmd.Example != "" || len(entry.Examples) == 0 {
			return
		}
		cmd.Example = formatExamples(entry.Examples)
	})
}

func formatExamples(examples []RefExample) string {
	var b strings.Builder
	for i, example := range examples {
		if i > 0 {
			b.WriteString("\n")
		}
		if example.Title != "" {
			b.WriteString("  # " + example.Title + "\n")
		}
		for _, line := range strings.Split(example.Run, "\n") {
			b.WriteString("  " + line + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// BuildReference walks the command tree and merges reference.yaml into it.
func BuildReference(root *cobra.Command) (*Reference, error) {
	overlay, err := loadOverlay()
	if err != nil {
		return nil, fmt.Errorf("reference.yaml: %w", err)
	}
	groupOf := map[string]string{}
	for _, group := range overlay.Groups {
		for _, path := range group.Commands {
			groupOf[path] = group.ID
		}
	}

	ref := &Reference{Groups: overlay.Groups, Topics: overlay.Topics}
	global := map[string]bool{}
	root.PersistentFlags().VisitAll(func(flag *pflag.Flag) {
		if flag.Hidden {
			return
		}
		global[flag.Name] = true
		ref.GlobalFlags = append(ref.GlobalFlags, refFlag(flag))
	})
	ref.GlobalFlags = append(ref.GlobalFlags, RefFlag{Name: "help", Shorthand: "h", Usage: "help for any command"})

	walkCommands(root, func(cmd *cobra.Command) {
		path := commandPath(cmd)
		top := strings.SplitN(path, " ", 2)[0]
		entry := overlay.Commands[path]
		command := RefCommand{
			Path:        path,
			Slug:        strings.ReplaceAll(path, " ", "-"),
			Group:       groupOf[top],
			Usage:       cmd.UseLine(),
			Summary:     cmd.Short,
			Description: strings.TrimSpace(entry.Description),
			Examples:    entry.Examples,
			Notes:       strings.TrimSpace(entry.Notes),
			Related:     entry.Related,
			Aliases:     cmd.Aliases,
			Flags:       []RefFlag{},
			Subcommands: []string{},
		}
		if command.Description == "" {
			command.Description = strings.TrimSpace(cmd.Long)
		}
		if command.Examples == nil {
			command.Examples = []RefExample{}
		}
		if command.Related == nil {
			command.Related = []string{}
		}
		if cmd.Parent() != root {
			command.Parent = commandPath(cmd.Parent())
		}
		if cmd.HasAvailableSubCommands() {
			command.Usage = cmd.CommandPath() + " <command>"
		}
		addFlag := func(flag *pflag.Flag) {
			if flag.Hidden || flag.Name == "help" || global[flag.Name] {
				return
			}
			command.Flags = append(command.Flags, refFlag(flag))
		}
		cmd.NonInheritedFlags().VisitAll(addFlag)
		cmd.InheritedFlags().VisitAll(addFlag)
		sort.Slice(command.Flags, func(i, j int) bool { return command.Flags[i].Name < command.Flags[j].Name })
		for _, child := range visibleChildren(cmd) {
			command.Subcommands = append(command.Subcommands, commandPath(child))
		}
		ref.Commands = append(ref.Commands, command)
	})
	return ref, nil
}

func refFlag(flag *pflag.Flag) RefFlag {
	out := RefFlag{Name: flag.Name, Shorthand: flag.Shorthand, Usage: flag.Usage}
	name, usage := pflag.UnquoteUsage(flag)
	out.Usage = usage
	switch flag.Value.Type() {
	case "bool":
	default:
		out.Type = name
		if out.Type == "" {
			out.Type = flag.Value.Type()
		}
	}
	switch flag.DefValue {
	case "", "false", "[]", "0", "0s":
	default:
		out.Default = flag.DefValue
		if flag.Value.Type() == "duration" {
			out.Default = shortDuration(flag.DefValue)
		}
	}
	return out
}

// shortDuration turns Go's "24h0m0s" into "24h" and "1h30m0s" into "1h30m".
func shortDuration(s string) string {
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// Command returns the command with the given path, or nil.
func (r *Reference) Command(path string) *RefCommand {
	for i := range r.Commands {
		if r.Commands[i].Path == path {
			return &r.Commands[i]
		}
	}
	return nil
}
