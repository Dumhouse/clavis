package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dumhouse/clavis/internal/version"
	"github.com/dumhouse/clavis/pkg/client"
)

const mcpProtocolVersion = "2024-11-05"

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type toolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type mcpServer struct {
	opts     *Options
	resolved *resolvedContext
	client   *client.Client      // static agent token (legacy, headless use)
	session  *agentSessionSource // delegated session started with the person's login
	encoder  *json.Encoder
}

// api returns the client for the current agent credential.
func (s *mcpServer) api(ctx context.Context) (*client.Client, error) {
	if s.session != nil {
		return s.session.client(ctx)
	}
	return s.client, nil
}

func newMCPCommand(opts *Options) *cobra.Command {
	var agent string
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run a Model Context Protocol server over stdio",
		Long: "Expose list_secrets, check_env, run_with_secrets, and request_secret to\n" +
			"MCP clients. Values are only returned to allowed commands; every run is\n" +
			"checked against the agent's server-side allowlist and audited.\n\n" +
			"With --agent, clavis mcp uses your own `clavis login` to start a one-hour\n" +
			"agent session, keeps it in memory, and renews it. Nothing secret goes in\n" +
			"the MCP config file. Logging out ends the session.",
		Example: "  clavis mcp --agent claude-code\n" +
			"  claude mcp add clavis -- clavis mcp --agent claude-code",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolved, err := opts.resolveContext(true)
			if err != nil {
				return err
			}
			agent = firstNonEmpty(agent, os.Getenv("CLAVIS_AGENT"))
			server := &mcpServer{opts: opts, resolved: resolved, encoder: json.NewEncoder(os.Stdout)}
			stderr := cmd.ErrOrStderr()
			switch {
			case agent != "":
				if strings.HasPrefix(resolved.Token, "clv_at_") || strings.HasPrefix(resolved.Token, "clv_mt_") {
					return fmt.Errorf("--agent uses your own login; remove CLAVIS_TOKEN (an agent or machine token) from the MCP config and run `clavis login`")
				}
				human, err := resolved.newClient()
				if err != nil {
					return err
				}
				server.session = newAgentSessionSource(human, resolved.Server, resolved.Org, agent)
				if _, err := server.session.client(cmd.Context()); err != nil {
					return err
				}
				fmt.Fprintf(stderr, "clavis mcp: agent %s, session held in memory and renewed hourly\n", agent)
			case strings.HasPrefix(resolved.Token, "clv_at_"):
				fmt.Fprintln(stderr, "clavis mcp: warning: using a static agent token from CLAVIS_TOKEN. "+
					"An agent that can read its MCP config can read this token. Prefer `clavis mcp --agent <name>`.")
				c, err := resolved.newClient()
				if err != nil {
					return err
				}
				server.client = c
			default:
				return fmt.Errorf("clavis mcp runs as an agent: use `clavis mcp --agent <name>` (starts a session with your login) or set CLAVIS_TOKEN to an agent token")
			}
			return server.serve(cmd.Context(), os.Stdin)
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent identity name; starts an in-memory session with your login (env CLAVIS_AGENT)")
	return cmd
}

func (s *mcpServer) serve(ctx context.Context, in io.Reader) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var request rpcRequest
		if err := json.Unmarshal(line, &request); err != nil {
			_ = s.encode(rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}
		response, notify := s.handle(ctx, &request)
		if notify {
			continue
		}
		if err := s.encode(response); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s *mcpServer) encode(response rpcResponse) error {
	response.JSONRPC = "2.0"
	return s.encoder.Encode(response)
}

func (s *mcpServer) handle(ctx context.Context, request *rpcRequest) (rpcResponse, bool) {
	response := rpcResponse{JSONRPC: "2.0", ID: request.ID}
	switch request.Method {
	case "initialize":
		response.Result = map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "clavis", "version": version.Version},
		}
	case "notifications/initialized":
		return response, true
	case "ping":
		response.Result = map[string]any{}
	case "tools/list":
		response.Result = map[string]any{"tools": mcpTools()}
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			response.Error = &rpcError{Code: -32602, Message: "invalid tool parameters"}
			return response, false
		}
		result, err := s.callTool(ctx, params.Name, params.Arguments)
		if err != nil {
			result = &toolResult{Content: []toolContent{{Type: "text", Text: err.Error()}}, IsError: true}
		}
		response.Result = result
	default:
		response.Error = &rpcError{Code: -32601, Message: "method not found"}
	}
	return response, false
}

func mcpTools() []toolDefinition {
	return []toolDefinition{
		{
			Name:        "list_secrets",
			Description: "List secret key names with schema type, kind, required environments, and descriptions. Never returns values.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "check_env",
			Description: "Check required keys and schema coverage for an environment. Reports values that require read-values as unchecked.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"environment": map[string]any{"type": "string", "description": "Environment slug (defaults to the linked environment)"},
				},
			},
		},
		{
			Name:        "run_with_secrets",
			Description: "Run a command with secrets injected. The command must match the agent's server-side allowlist. Output is masked.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command":         map[string]any{"type": "string"},
					"args":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"timeout_seconds": map[string]any{"type": "number", "minimum": 1, "maximum": 600},
				},
				"required": []string{"command"},
			},
		},
		{
			Name:        "request_secret",
			Description: "Ask a human to add a missing secret. Creates an audited request; never creates a value.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key":    map[string]any{"type": "string"},
					"reason": map[string]any{"type": "string"},
				},
				"required": []string{"key"},
			},
		},
	}
}

func (s *mcpServer) callTool(ctx context.Context, name string, raw json.RawMessage) (*toolResult, error) {
	switch name {
	case "list_secrets":
		return s.listSecrets(ctx)
	case "check_env":
		return s.checkEnv(ctx, raw)
	case "run_with_secrets":
		return s.runWithSecrets(ctx, raw)
	case "request_secret":
		return s.requestSecret(ctx, raw)
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

func (s *mcpServer) listSecrets(ctx context.Context) (*toolResult, error) {
	var schema schemaResponse
	c, err := s.api(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.Get(ctx, schemaBase(s.resolved), &schema); err != nil {
		return nil, err
	}
	var secrets secretsListResponse
	if err := c.Get(ctx, secretsBase(s.resolved), &secrets); err != nil {
		return nil, err
	}
	kinds := make(map[string]string, len(secrets.Secrets))
	for _, secret := range secrets.Secrets {
		kinds[secret.Key] = secret.Kind
	}
	type keyInfo struct {
		Key         string          `json:"key"`
		Kind        string          `json:"kind"`
		Type        string          `json:"type,omitempty"`
		Required    map[string]bool `json:"required,omitempty"`
		Description string          `json:"description,omitempty"`
		Example     string          `json:"example,omitempty"`
		Enum        []string        `json:"enum,omitempty"`
		InSchema    bool            `json:"in_schema"`
	}
	byKey := make(map[string]schemaJSON, len(schema.Schema))
	for _, entry := range schema.Schema {
		byKey[entry.Key] = entry
	}
	keys := make([]keyInfo, 0, len(secrets.Secrets))
	for _, secret := range secrets.Secrets {
		entry := byKey[secret.Key]
		keys = append(keys, keyInfo{
			Key: secret.Key, Kind: kinds[secret.Key], Type: entry.Type,
			Required: entry.Required, Description: entry.Description,
			Example: entry.Example, Enum: entry.Enum, InSchema: entry.Key != "",
		})
	}
	return textResult(map[string]any{
		"environment": s.resolved.Env,
		"keys":        keys,
		"note":        "Values are never returned by list_secrets. Use run_with_secrets for an allowed command.",
	})
}

func (s *mcpServer) checkEnv(ctx context.Context, raw json.RawMessage) (*toolResult, error) {
	var args struct {
		Environment string `json:"environment"`
	}
	_ = json.Unmarshal(raw, &args)
	resolved := *s.resolved
	if args.Environment != "" {
		resolved.Env = args.Environment
	}
	var schema schemaResponse
	c, err := s.api(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.Get(ctx, schemaBase(&resolved), &schema); err != nil {
		return nil, err
	}
	var project projectDetail
	if err := c.Get(ctx, projectBase(&resolved), &project); err != nil {
		return nil, err
	}
	envType := ""
	for _, env := range project.Environments {
		if env.Slug == resolved.Env {
			envType = env.Type
			break
		}
	}
	var secrets secretsListResponse
	if err := c.Get(ctx, secretsBase(&resolved), &secrets); err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(secrets.Secrets))
	for _, secret := range secrets.Secrets {
		present[secret.Key] = true
	}
	type check struct {
		Key     string `json:"key"`
		Status  string `json:"status"`
		Message string `json:"message,omitempty"`
	}
	checks := make([]check, 0, len(schema.Schema))
	ok := true
	for _, entry := range schema.Schema {
		if !present[entry.Key] {
			if entry.Required[envType] {
				ok = false
				checks = append(checks, check{Key: entry.Key, Status: "error",
					Message: fmt.Sprintf("required in %s but missing", envType)})
			} else {
				checks = append(checks, check{Key: entry.Key, Status: "warning", Message: "missing (optional)"})
			}
			continue
		}
		checks = append(checks, check{Key: entry.Key, Status: "ok"})
	}
	return textResult(map[string]any{
		"ok": ok, "environment": resolved.Env, "type": envType, "checks": checks,
		"note": "Type, pattern, and enum checks need values and run in `clavis check` with read-values or on write.",
	})
}

func (s *mcpServer) runWithSecrets(ctx context.Context, raw json.RawMessage) (*toolResult, error) {
	var args struct {
		Command        string   `json:"command"`
		Args           []string `json:"args"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(args.Command) == "" {
		return nil, errors.New("command is required")
	}
	timeout := args.TimeoutSeconds
	if timeout <= 0 {
		timeout = 120
	}
	if timeout > 600 {
		timeout = 600
	}
	commandText := strings.Join(append([]string{args.Command}, args.Args...), " ")
	path := fmt.Sprintf("%s:agent?command=%s", secretsBase(s.resolved), url.QueryEscape(commandText))
	var secrets secretsListResponse
	c, err := s.api(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.Get(ctx, path, &secrets); err != nil {
		return nil, err
	}
	entries, values, cleanup, err := prepareSecretEnv(secrets.Secrets, false)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	childCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	child := exec.CommandContext(childCtx, args.Command, args.Args...)
	child.Env = append(os.Environ(), entries...)
	child.Stdin = nil
	output, err := child.CombinedOutput()
	if len(output) > 1<<20 {
		output = output[:1<<20]
	}
	masked := newMasker(values).mask(output)
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else if errors.Is(childCtx.Err(), context.DeadlineExceeded) {
			exitCode = -1
		} else {
			return nil, err
		}
	}
	result, err := textResult(map[string]any{
		"command":   commandText,
		"exit_code": exitCode,
		"output":    string(masked),
		"masked":    true,
	})
	if exitCode != 0 {
		result.IsError = true
	}
	return result, err
}

func (s *mcpServer) requestSecret(ctx context.Context, raw json.RawMessage) (*toolResult, error) {
	var args struct {
		Key    string `json:"key"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	path := strings.TrimSuffix(secretsBase(s.resolved), "/secrets") + "/secret-requests"
	body := map[string]any{"key": args.Key, "reason": args.Reason}
	c, err := s.api(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.Post(ctx, path, body, nil); err != nil {
		return nil, err
	}
	return textResult(map[string]any{
		"requested": args.Key,
		"status":    "pending human review",
		"note":      "The request is in the audit log. No value was created.",
	})
}

func textResult(value any) (*toolResult, error) {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return &toolResult{Content: []toolContent{{Type: "text", Text: string(body)}}}, nil
}
