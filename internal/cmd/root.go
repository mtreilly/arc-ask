// Copyright (c) 2025 Arc Engineering
// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yourorg/arc-sdk/errors"
	"github.com/yourorg/arc-sdk/output"
	"github.com/yourorg/arc-tmux/pkg/tmux"
)

// Client interface for arc-ai bridge
type AIClient interface {
	Ask(ctx context.Context, prompt string) (string, error)
	AskWithContext(ctx context.Context, prompt, context string) (string, error)
	AskWithTools(ctx context.Context, prompt string, tools []string) (string, error)
	IsDaemonRunning() bool
}

// BridgeClient implements AIClient using arc-ai daemon
type BridgeClient struct {
	socketPath string
	timeout    time.Duration
}

// NewBridgeClient creates a client for arc-ai daemon
func NewBridgeClient() *BridgeClient {
	socketPath := os.Getenv("ARC_AI_SOCKET")
	if socketPath == "" {
		socketPath = "~/.config/arc/ai/daemon.sock"
	}
	return &BridgeClient{
		socketPath: socketPath,
		timeout:    60 * time.Second,
	}
}

// IsDaemonRunning checks if arc-ai is available
func (c *BridgeClient) IsDaemonRunning() bool {
	// Check for socket file
	path := expandHome(c.socketPath)
	_, err := os.Stat(path)
	return err == nil
}

// Ask sends a simple question to arc-ai
func (c *BridgeClient) Ask(ctx context.Context, prompt string) (string, error) {
	// For now, fall back to direct execution if daemon not running
	// In full implementation, use RPC to daemon
	return c.fallbackAsk(ctx, prompt)
}

// AskWithContext sends question with stdin context
func (c *BridgeClient) AskWithContext(ctx context.Context, prompt, context string) (string, error) {
	return c.fallbackAsk(ctx, prompt, context)
}

// AskWithTools enables specific Pi tools
func (c *BridgeClient) AskWithTools(ctx context.Context, prompt string, tools []string) (string, error) {
	// In full implementation, tell daemon which extensions to load
	return c.fallbackAsk(ctx, prompt, "", tools)
}

// fallbackAsk runs pi directly (temporary until full RPC)
func (c *BridgeClient) fallbackAsk(ctx context.Context, prompt string, input ...string) (string, error) {
	// Check if pi is installed
	piPath := "pi"
	if _, err := execLookPath(piPath); err != nil {
		return "", fmt.Errorf("Pi not found. Install: npm install -g @mariozechner/pi-coding-agent")
	}

	args := []string{"--mode", "json", "--print", prompt}
	if len(input) > 0 && input[0] != "" {
		// Use heredoc for input
		args = []string{"-c", fmt.Sprintf("echo %q | pi --mode json --print %q", input[0], prompt)}
		piPath = "bash"
	}

	cmd := execCommand(piPath, args...)
	cmd.Env = os.Environ()

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*execExitError); ok {
			return "", fmt.Errorf("pi failed: %s", exitErr.Stderr)
		}
		return "", fmt.Errorf("failed to run pi: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

// execLookPath and execCommand are abstractions for testing
var execLookPath = exec.LookPath
var execCommand = exec.Command

// execExitError interface for error checking
type execExitError interface {
	error
	ExitCode() int
	Stderr []byte
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return home + path[1:]
	}
	return path
}

// NewRootCmd creates the root command
func NewRootCmd() *cobra.Command {
	client := NewBridgeClient()

	var (
		pane          string
		lines         int
		contextFiles  []string
		tools         []string
		listTemplates bool
		outputOpts    output.OutputOptions
	)

	cmd := &cobra.Command{
		Use:   "arc-ask [prompt]",
		Short: "Ask an AI assistant (arc-ai bridge)",
		Long: `Ask an AI assistant about stdin input or a direct question.

arc-ask connects to the arc-ai daemon for powerful AI capabilities
including extensions, tools, and session management.

If arc-ai is not running, falls back to direct Pi execution.`,
		Example: `  # Simple question
  arc-ask "What is Go?"

  # With piped input
  cat code.go | arc-ask "Explain this"

  # From tmux pane
  arc-ask "What's wrong?" --pane dev:1.0

  # With tools
  cat errors.log | arc-ask "Analyze" --tools security,tmux`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if listTemplates {
				return listTemplatesCmd(cmd.OutOrStdout())
			}

			if err := outputOpts.Resolve(); err != nil {
				return err
			}

			// Check daemon status
			if !client.IsDaemonRunning() {
				fmt.Fprintln(os.Stderr, "Note: arc-ai daemon not running. Using fallback mode.")
				fmt.Fprintln(os.Stderr, "For better performance, run: arc-ai start")
			}

			// Gather input
			input, err := gatherInput(cmd, pane, lines)
			if err != nil {
				return err
			}

			// Merge context files
			input, err = mergeContext(input, contextFiles)
			if err != nil {
				return err
			}

			// Validate prompt
			if len(args) == 0 && input == "" {
				return errors.NewCLIError("no prompt or input provided").
					WithSuggestions(
						"Ask a question: arc-ask 'What is this?'",
						"Pipe input: cat file | arc-ask 'Explain'",
						"List templates: arc-ask --list-templates",
					)
			}

			prompt := ""
			if len(args) > 0 {
				prompt = args[0]
			}

			// Build full prompt
			if input != "" {
				prompt = fmt.Sprintf("%s\n\nInput:\n%s", prompt, input)
			}

			// Query AI
			ctx, cancel := context.WithTimeout(context.Background(), client.timeout)
			defer cancel()

			var answer string
			if len(tools) > 0 {
				answer, err = client.AskWithTools(ctx, prompt, tools)
			} else {
				answer, err = client.Ask(ctx, prompt)
			}

			if err != nil {
				return errors.NewCLIError("AI query failed").WithCause(err)
			}

			// Output
			switch {
			case outputOpts.Is(output.OutputJSON):
				fmt.Printf(`{"response": %q}%s`, answer, "\n")
			case outputOpts.Is(output.OutputQuiet):
				// No output
			default:
				fmt.Println(answer)
			}

			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringVar(&pane, "pane", "", "Capture from tmux pane (e.g., session:0.0)")
	cmd.Flags().IntVar(&lines, "lines", 200, "Lines to capture from pane")
	cmd.Flags().StringArrayVarP(&contextFiles, "context", "c", nil, "Add context file(s)")
	cmd.Flags().StringSliceVar(&tools, "tools", nil, "Enable tools (security,tmux,deps)")
	cmd.Flags().BoolVar(&listTemplates, "list-templates", false, "List available templates")
	outputOpts.AddOutputFlags(cmd, output.OutputTable)

	return cmd
}

func gatherInput(cmd *cobra.Command, pane string, lines int) (string, error) {
	if pane != "" {
		if err := tmux.ValidateTarget(pane); err != nil {
			return "", errors.NewCLIError("invalid pane target").
				WithCause(err).
				WithSuggestions("Format: session:window.pane (e.g., dev:0.0)")
		}
		content, err := tmux.Capture(pane, lines)
		if err != nil {
			return "", errors.NewCLIError("failed to capture pane").
				WithCause(err).
				WithSuggestions("Check that the pane exists: tmux list-panes")
		}
		return content, nil
	}

	// Check stdin
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}

	return "", nil
}

func mergeContext(input string, files []string) (string, error) {
	if len(files) == 0 {
		return input, nil
	}

	var b strings.Builder
	b.WriteString(input)

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return "", errors.NewCLIError("failed to read context file").
				WithCause(err)
		}
		b.WriteString("\n\nContext (")
		b.WriteString(f)
		b.WriteString("):\n")
		b.Write(data)
	}

	return b.String(), nil
}

func listTemplatesCmd(w io.Writer) error {
	fmt.Fprintln(w, "Available templates:")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  @code-review     Review code changes")
	fmt.Fprintln(w, "  @explain         Explain complex code")
	fmt.Fprintln(w, "  @summarize       Summarize text/logs")
	fmt.Fprintln(w, "  @security-check  Check for vulnerabilities")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Create templates in: ~/.config/arc/prompts/")
	return nil
}
