package cli

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

var stdin = bufio.NewReader(os.Stdin)

func promptLine(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	line, err := stdin.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func promptSecret(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	line, err := stdin.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// promptChoice prints a numbered list and returns the selected index. defaultIdx
// is used when the user presses enter.
func promptChoice(prompt string, options []string, defaultIdx int) (int, error) {
	if len(options) == 0 {
		return -1, fmt.Errorf("no options available")
	}
	fmt.Fprintln(os.Stderr, prompt)
	for i, option := range options {
		fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, option)
	}
	for {
		hint := fmt.Sprintf("Choose [1-%d] (default %d): ", len(options), defaultIdx+1)
		line, err := promptLine(hint)
		if err != nil {
			return -1, err
		}
		if line == "" {
			return defaultIdx, nil
		}
		n, err := strconv.Atoi(line)
		if err == nil && n >= 1 && n <= len(options) {
			return n - 1, nil
		}
		fmt.Fprintln(os.Stderr, "Enter a number from the list.")
	}
}

func confirm(prompt string) (bool, error) {
	line, err := promptLine(prompt + " [y/N]: ")
	if err != nil {
		return false, err
	}
	line = strings.ToLower(line)
	return line == "y" || line == "yes", nil
}
