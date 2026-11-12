package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

func newRunCommand(opts *Options) *cobra.Command {
	var noMask bool
	var mountFiles bool
	var watch bool
	cmd := &cobra.Command{
		Use:   "run [--no-mask] [--mount] [--watch] -- COMMAND [ARGS...]",
		Short: "Run a command with secrets injected and output masked",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithSecrets(cmd, opts, args, !noMask, mountFiles, watch)
		},
	}
	cmd.Flags().BoolVar(&noMask, "no-mask", false, "disable output masking (dangerous)")
	cmd.Flags().BoolVar(&mountFiles, "mount", false, "write file secrets to a private temp directory and expose their paths as env vars")
	cmd.Flags().BoolVar(&watch, "watch", false, "restart the command when the environment's secrets change")
	return cmd
}

func runWithSecrets(cmd *cobra.Command, opts *Options, args []string, maskOutput, mountFiles, watch bool) error {
	for {
		restart, err := runWithSecretsOnce(cmd, opts, args, maskOutput, mountFiles, watch)
		if err != nil || !restart {
			return err
		}
		fmt.Fprintln(opts.output().Err, "Secrets changed; restarting…")
	}
}

func runWithSecretsOnce(cmd *cobra.Command, opts *Options, args []string, maskOutput, mountFiles, watch bool) (bool, error) {
	ctx := cmd.Context()
	resolved, err := opts.resolveContext(true)
	if err != nil {
		return false, err
	}
	c, err := resolved.newClient()
	if err != nil {
		return false, err
	}
	var result secretsListResponse
	path := secretsBase(resolved) + ":resolve?channel=cli"
	if opts.Overlay != "" {
		path += "&overlay=" + url.QueryEscape(opts.Overlay)
	}
	if err := c.Get(ctx, path, &result); err != nil {
		if !resolved.Cache {
			return false, err
		}
		cached, cacheErr := loadSecretCache(resolved)
		if cacheErr != nil {
			return false, err
		}
		result.Secrets = cached
		fmt.Fprintln(opts.output().Err, "warning: server unavailable; using the encrypted offline cache")
	} else if resolved.Cache {
		if cacheErr := saveSecretCache(resolved, result.Secrets); cacheErr != nil {
			fmt.Fprintf(opts.output().Err, "warning: could not write the offline cache: %v\n", cacheErr)
		}
	}

	entries, values, cleanup, err := prepareSecretEnv(result.Secrets, mountFiles)
	if err != nil {
		return false, err
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
		return false, err
	}
	stderrPipe, err := child.StderrPipe()
	if err != nil {
		return false, err
	}

	m := newMasker(values)
	stdout := &maskWriter{dst: os.Stdout, m: m}
	stderr := &maskWriter{dst: opts.output().Err, m: m}
	if !maskOutput {
		stdout = &maskWriter{dst: os.Stdout}
		stderr = &maskWriter{dst: opts.output().Err}
	}

	if err := child.Start(); err != nil {
		return false, fmt.Errorf("could not start %s: %w", args[0], err)
	}
	restart := make(chan struct{}, 1)
	watchCtx, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()
	if watch {
		go func() {
			if watchForSecretChange(watchCtx, resolved) {
				select {
				case restart <- struct{}{}:
				default:
				}
				if child.Process != nil {
					_ = child.Process.Kill()
				}
			}
		}()
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
	cancelWatch()
	select {
	case <-restart:
		cleanup()
		return true, nil
	default:
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			return false, &ExitError{Code: exitErr.ExitCode(), Err: waitErr, Silent: true}
		}
		return false, waitErr
	}
	return false, nil
}

type watchEvent struct {
	Type        string `json:"type"`
	Project     string `json:"project"`
	Environment string `json:"environment"`
}

func watchForSecretChange(ctx context.Context, resolved *resolvedContext) bool {
	endpoint := resolved.Server + "/api/v1/orgs/" + url.PathEscape(resolved.Org) + "/events"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	request.Header.Set("Authorization", "Bearer "+resolved.Token)
	request.Header.Set("Accept", "text/event-stream")
	response, err := (&http.Client{Timeout: 0}).Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event watchEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event); err != nil {
			continue
		}
		if event.Type != "secrets.changed" {
			continue
		}
		if event.Project != "" && event.Project != resolved.Project {
			continue
		}
		if event.Environment != "" && event.Environment != resolved.Env {
			continue
		}
		return true
	}
	return false
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
