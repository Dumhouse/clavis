package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
)

func newRunCommand(opts *Options) *cobra.Command {
	var noMask bool
	var mountFiles bool
	cmd := &cobra.Command{
		Use:   "run [--no-mask] [--mount] -- COMMAND [ARGS...]",
		Short: "Run a command with secrets injected and output masked",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithSecrets(cmd, opts, args, !noMask, mountFiles)
		},
	}
	cmd.Flags().BoolVar(&noMask, "no-mask", false, "disable output masking (dangerous)")
	cmd.Flags().BoolVar(&mountFiles, "mount", false, "write file secrets to a private temp directory and expose their paths as env vars")
	return cmd
}

func runWithSecrets(cmd *cobra.Command, opts *Options, args []string, maskOutput, mountFiles bool) error {
	ctx := cmd.Context()
	resolved, err := opts.resolveContext(true)
	if err != nil {
		return err
	}
	c, err := resolved.newClient()
	if err != nil {
		return err
	}
	var result secretsListResponse
	if err := c.Get(ctx, secretsBase(resolved)+":resolve?channel=cli", &result); err != nil {
		return err
	}

	entries, values, cleanup, err := prepareSecretEnv(result.Secrets, mountFiles)
	if err != nil {
		return err
	}
	defer cleanup()
	env := append(os.Environ(), entries...)
	if len(values) == 0 {
		fmt.Fprintln(opts.output().Err, "warning: no secrets found; running with the current environment")
	}

	child := exec.CommandContext(ctx, args[0], args[1:]...)
	child.Env = env
	child.Stdin = os.Stdin
	stdoutPipe, err := child.StdoutPipe()
	if err != nil {
		return err
	}
	stderrPipe, err := child.StderrPipe()
	if err != nil {
		return err
	}

	m := newMasker(values)
	stdout := &maskWriter{dst: os.Stdout, m: m}
	stderr := &maskWriter{dst: opts.output().Err, m: m}
	if !maskOutput {
		stdout = &maskWriter{dst: os.Stdout}
		stderr = &maskWriter{dst: opts.output().Err}
	}

	if err := child.Start(); err != nil {
		return fmt.Errorf("could not start %s: %w", args[0], err)
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	defer close(signals)
	go func() {
		if _, ok := <-signals; !ok {
			return
		}
		cleanup()
		if child.Process != nil {
			_ = child.Process.Kill()
		}
	}()
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(stdout, stdoutPipe)
		_ = stdout.Flush()
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(stderr, stderrPipe)
		_ = stderr.Flush()
		done <- struct{}{}
	}()
	<-done
	<-done
	waitErr := child.Wait()
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			return &ExitError{Code: exitErr.ExitCode(), Err: waitErr, Silent: true}
		}
		return waitErr
	}
	return nil
}

// prepareSecretEnv splits secrets into environment variables and, when mount
// is set, private files whose paths are exposed instead of their contents.
func prepareSecretEnv(secrets []secretJSON, mount bool) ([]string, []string, func(), error) {
	entries := make([]string, 0, len(secrets))
	values := make([]string, 0, len(secrets))
	cleanup := func() {}
	if !mount {
		for _, secret := range secrets {
			if secret.Value == "" {
				continue
			}
			entries = append(entries, secret.Key+"="+secret.Value)
			values = append(values, secret.Value)
		}
		return entries, values, cleanup, nil
	}
	dir, err := os.MkdirTemp("", "clavis-secrets-")
	if err != nil {
		return nil, nil, cleanup, fmt.Errorf("create mount directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return nil, nil, cleanup, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	for _, secret := range secrets {
		if secret.Value == "" {
			continue
		}
		if secret.Kind != "file" {
			entries = append(entries, secret.Key+"="+secret.Value)
			values = append(values, secret.Value)
			continue
		}
		name := filepath.Base(secret.Key)
		if name == "." || name == "/" || name == "" {
			cleanup()
			return nil, nil, func() {}, fmt.Errorf("invalid file secret name %q", secret.Key)
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(secret.Value), 0o600); err != nil {
			cleanup()
			return nil, nil, func() {}, fmt.Errorf("write file secret %s: %w", secret.Key, err)
		}
		entries = append(entries, secret.Key+"="+path)
	}
	return entries, values, cleanup, nil
}
