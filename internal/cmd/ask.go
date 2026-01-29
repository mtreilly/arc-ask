// Copyright (c) 2025 Arc Engineering
// SPDX-License-Identifier: MIT

package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yourorg/arc-prompt/pkg/prompt"
	"github.com/yourorg/arc-sdk/ai"
	"github.com/yourorg/arc-sdk/errors"
	"github.com/yourorg/arc-sdk/output"
	"github.com/yourorg/arc-tmux/pkg/tmux"
	"gopkg.in/yaml.v3"
)

const defaultModel = "claude-sonnet-4-5-20250929"

// newAskCmd creates the ask command.
func newAskCmd(aiCfg *ai.Config) *cobra.Command {
	var (
		provider      string
		model         string
		pane          string
		vars          map[string]string
		lines         int
		listTemplates bool
		contextFiles  []string
		maxTokens     int
		temperature   float64
		outputOpts    output.OutputOptions
	)

	cmd := &cobra.Command{
		Use:   "arc-ask [prompt-or-question]",
		Short: "Ask an AI agent a question (pipe-friendly)",
		Long: `Ask an AI agent a question about stdin input or a direct question.

If a prompt starts with @, load template from ~/.config/arc/prompts/.
Otherwise, use the argument as a natural language question.

Reads from stdin if available. Use --pane to auto-capture from tmux.`,
		Example: `  # Summarize errors from stdin
  cat logs.txt | arc-ask "what's wrong?"

  # Apply a template to a captured tmux pane
  arc tmux capture --pane fe:4.1 | arc-ask @detect-errors

  # Capture directly from a pane with extra lines + context
  arc-ask @check-health --pane api:1.0 --lines 300 --context README.md

  # Force a specific model and emit JSON for scripting
  git diff --staged | arc-ask "summarize these changes" --model claude-haiku-4-5-20251001 --output json

  # Discover template inventory
  arc-ask --list-templates`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Handle --list-templates flag
			if listTemplates {
				return listAvailableTemplates(cmd.OutOrStdout())
			}

			if err := outputOpts.Resolve(); err != nil {
				return err
			}

			// 1. Gather input
			input, err := gatherInput(cmd, pane, lines)
			if err != nil {
				return err
			}

			// 2. Resolve prompt
			effectiveModel := model
			if effectiveModel == "" {
				effectiveModel = defaultModel
			}
			systemPrompt, userPrompt, promptModel, err := resolvePrompt(args, vars, input, effectiveModel)
			if err != nil {
				return err
			}

			finalModel := promptModel
			if finalModel == "" {
				finalModel = effectiveModel
			}

			userWithContext, err := mergeContextToPrompt(userPrompt, contextFiles)
			if err != nil {
				return err
			}

			// 3. Build effective config with flag overrides
			cfg := *aiCfg
			if provider != "" {
				cfg.Provider = provider
			}
			cfg.DefaultModel = finalModel

			// 4. Create AI client and service
			client, err := ai.NewClient(cfg)
			if err != nil {
				return errors.NewCLIError("failed to create AI client").WithCause(err)
			}
			service := ai.NewService(client, cfg)

			// 5. Run AI request
			response, err := service.Run(cmd.Context(), ai.RunOptions{
				Model:       finalModel,
				System:      systemPrompt,
				Prompt:      userWithContext,
				MaxTokens:   maxTokens,
				Temperature: temperature,
			})
			if err != nil {
				return errors.NewCLIError("AI request failed").WithCause(err)
			}

			// 6. Output result
			return outputResult(cmd.OutOrStdout(), outputOpts, response, provider, finalModel)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringVar(&provider, "provider", "", "AI provider override")
	cmd.Flags().StringVarP(&model, "model", "m", "", "Model to use (default: "+defaultModel+")")
	cmd.Flags().StringVar(&pane, "pane", "", "Auto-capture from tmux pane (e.g., session:0.0)")
	cmd.Flags().StringToStringVarP(&vars, "var", "v", nil, "Template variables (key=value)")
	cmd.Flags().IntVar(&lines, "lines", 200, "Lines to capture from pane (0=all)")
	cmd.Flags().BoolVar(&listTemplates, "list-templates", false, "List available prompt templates")
	cmd.Flags().StringArrayVarP(&contextFiles, "context", "c", []string{}, "Add context file(s)")
	cmd.Flags().IntVar(&maxTokens, "max-tokens", 0, "Maximum tokens for response (0 = default)")
	cmd.Flags().Float64Var(&temperature, "temperature", 0, "Temperature for generation (0 = default)")
	outputOpts.AddOutputFlags(cmd, output.OutputTable)

	// Shell completion
	_ = cmd.RegisterFlagCompletionFunc("pane", completePanes)
	_ = cmd.RegisterFlagCompletionFunc("model", completeModels)

	return cmd
}

// gatherInput collects input from either --pane or stdin.
func gatherInput(cmd *cobra.Command, pane string, lines int) (string, error) {
	if pane != "" {
		// Auto-capture from tmux pane
		if err := tmux.ValidateTarget(pane); err != nil {
			return "", errors.NewCLIError(fmt.Sprintf("invalid pane target %q", pane)).
				WithHint("Pane format must be: session:window.pane (e.g., fe:0.0)")
		}
		content, err := tmux.Capture(pane, lines)
		if err != nil {
			return "", errors.NewCLIError(fmt.Sprintf("pane %q not found", pane)).
				WithHint("Check that the tmux session and pane exist").
				WithSuggestions("tmux list-panes -a")
		}
		return content, nil
	}

	// Check if stdin is piped
	stdin := cmd.InOrStdin()
	if f, ok := stdin.(*os.File); ok {
		stat, err := f.Stat()
		if err != nil {
			return "", errors.NewCLIError("failed to check stdin").WithCause(err)
		}

		// If stdin is a pipe or file (not a terminal), read it
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			data, err := io.ReadAll(stdin)
			if err != nil {
				return "", errors.NewCLIError("failed to read piped input").WithCause(err)
			}
			return string(data), nil
		}
	}

	// No input - this is fine for direct questions
	return "", nil
}

// resolvePrompt handles @template or direct question.
func resolvePrompt(args []string, vars map[string]string, input, defaultModel string) (system, user, model string, err error) {
	if len(args) == 0 {
		return "", "", "", errors.NewCLIError("no prompt or question specified").
			WithSuggestions(
				"Ask a direct question: arc-ask \"What is this?\"",
				"Use a template: arc-ask @detect-errors",
				"List templates: arc-ask --list-templates",
			)
	}

	arg := args[0]

	if strings.HasPrefix(arg, "@") {
		templateName := strings.TrimPrefix(arg, "@")

		p, err := prompt.LoadWithDefaults(templateName)
		if err != nil {
			return "", "", "", errors.NewCLIError(fmt.Sprintf("template %q not found", templateName)).
				WithHint("Check available templates with: arc-ask --list-templates").
				WithSuggestions(
					"arc-ask --list-templates",
					fmt.Sprintf("Create template at: ~/.config/arc/prompts/%s.yaml", templateName),
				)
		}

		data := cloneStringMap(vars)
		data["Input"] = input

		system, user, err := p.Execute(data)
		if err != nil {
			return "", "", "", errors.NewCLIError(fmt.Sprintf("failed to render template %q", templateName)).
				WithCause(err).
				WithHint("Check that all required variables are provided")
		}

		model := defaultModel
		if p.Model != "" {
			model = p.Model
		}

		return system, user, model, nil
	}

	// Direct question
	userPrompt := arg
	if input != "" {
		userPrompt = fmt.Sprintf("%s\n\nInput:\n%s", arg, input)
	}

	return "", userPrompt, defaultModel, nil
}

// mergeContextToPrompt adds context files to the prompt.
func mergeContextToPrompt(promptText string, contextFiles []string) (string, error) {
	if len(contextFiles) == 0 {
		return promptText, nil
	}

	var builder strings.Builder
	builder.WriteString(promptText)

	for _, raw := range contextFiles {
		path := strings.TrimSpace(raw)
		if strings.HasPrefix(path, "@") {
			path = strings.TrimPrefix(path, "@")
		}
		if path == "" {
			continue
		}

		info, err := os.Stat(path)
		if err != nil {
			return "", errors.NewCLIError(fmt.Sprintf("failed to read context %q", path)).
				WithCause(err).
				WithHint("Ensure the file exists and is accessible")
		}
		if info.IsDir() {
			return "", errors.NewCLIError(fmt.Sprintf("context path %q is a directory", path)).
				WithHint("Provide a file path (e.g., README.md)")
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return "", errors.NewCLIError(fmt.Sprintf("failed to read context %q", path)).
				WithCause(err)
		}

		builder.WriteString("\n\nContext (")
		builder.WriteString(path)
		builder.WriteString("):\n")
		builder.Write(data)
	}

	return builder.String(), nil
}

// listAvailableTemplates lists all available prompt templates.
func listAvailableTemplates(w io.Writer) error {
	templates, err := prompt.List()
	if err != nil {
		return errors.NewCLIError("failed to list templates").WithCause(err)
	}

	if len(templates) == 0 {
		fmt.Fprintln(w, "No templates found in ~/.config/arc/prompts/")
		fmt.Fprintln(w, "\nCreate a template with:")
		fmt.Fprintln(w, "  mkdir -p ~/.config/arc/prompts")
		fmt.Fprintln(w, "  $EDITOR ~/.config/arc/prompts/my-template.yaml")
		return nil
	}

	fmt.Fprintf(w, "Available templates (%d found):\n\n", len(templates))

	for _, name := range templates {
		p, err := prompt.Load(name)
		if err != nil {
			fmt.Fprintf(w, "  @%-25s (error loading: %v)\n", name, err)
			continue
		}

		desc := "No description"
		if p.Metadata != nil {
			if d, ok := p.Metadata["description"].(string); ok && d != "" {
				desc = d
			}
		}

		fmt.Fprintf(w, "  @%-25s %s\n", name, desc)
	}

	fmt.Fprintf(w, "\nUsage: arc-ask @template-name\n")
	fmt.Fprintf(w, "Template directory: ~/.config/arc/prompts/\n")

	return nil
}

// outputResult formats and outputs the AI response.
func outputResult(w io.Writer, opts output.OutputOptions, resp ai.Response, provider, model string) error {
	switch {
	case opts.Is(output.OutputJSON):
		result := map[string]string{
			"response": strings.TrimSpace(resp.Text),
			"provider": provider,
			"model":    model,
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	case opts.Is(output.OutputYAML):
		result := map[string]string{
			"response": strings.TrimSpace(resp.Text),
			"provider": provider,
			"model":    model,
		}
		enc := yaml.NewEncoder(w)
		defer enc.Close()
		return enc.Encode(result)
	case opts.Is(output.OutputQuiet):
		return nil
	default:
		fmt.Fprintln(w, strings.TrimSpace(resp.Text))
		return nil
	}
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return make(map[string]string)
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// completePanes provides shell completion for pane targets.
func completePanes(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	panes, err := tmux.ListPanes()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var completions []string
	for _, pane := range panes {
		id := pane.FormattedID()
		if toComplete == "" || strings.HasPrefix(id, toComplete) {
			completions = append(completions, id)
		}
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
}

// completeModels provides shell completion for model names.
func completeModels(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	models := []string{
		"claude-sonnet-4-5-20250929\tClaude Sonnet general-purpose reasoning",
		"claude-haiku-4-5-20251001\tFast, cost-effective responses",
		"claude-opus-4-20250514\tHigh-capability reasoning",
	}
	return models, cobra.ShellCompDirectiveNoFileComp
}
