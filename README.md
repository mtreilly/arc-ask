# arc-ask

Ask an AI assistant questions about code, logs, or any text input.

## Overview

arc-ask has been refactored to use the **arc-ai bridge**, enabling:

- **Pi harness integration** - Full access to Pi's AI capabilities
- **Extension ecosystem** - Use Pi extensions (security, tmux, deps, etc.)
- **Better composability** - Works seamlessly with other arc tools
- **Fallback mode** - Works even without arc-ai daemon

## Prerequisites

Either:
- **arc-ai daemon** running (recommended): `arc-ai start`
- **Pi** installed globally: `npm install -g @mariozechner/pi-coding-agent`

## Installation

```bash
go install github.com/mtreilly/arc-ask@latest
```

## Usage

### Basic questions

```bash
# Simple question
arc-ask "What is the best way to handle errors in Go?"

# With piped input
cat main.go | arc-ask "Explain this code"

# Multiple files
cat *.go | arc-ask "Review these files"
```

### With arc tools

```bash
# Analyze tmux pane output
arc-tmux follow --pane dev:1.0 | arc-ask "What's causing the error?"

# Review security findings
arc-security audit --json | arc-ask "Summarize critical issues"

# Check dependencies
arc-deps check | arc-ask "Which packages should I update first?"
```

### With tools enabled

```bash
# Enable specific Pi tools
arc-ask "Analyze this" --tools security,tmux

# Available tools: security, tmux, deps, spell, typescript, semgrep
```

### With context files

```bash
arc-ask "Review implementation" --context README.md --context ARCHITECTURE.md
```

## Changes from Previous Version

### New architecture

**Before:** Direct AI client in Go (limited providers, no extensions)
**After:** Bridge to Pi harness (full Pi power, all extensions)

```
Old:  arc-ask → Go AI client → API
New:  arc-ask → arc-ai daemon → Pi → API + extensions
```

### New features

- ✅ Pi extension support (27+ tools)
- ✅ Better integration with arc toolkit
- ✅ Structured output (JSON)
- ✅ Session support (via arc-ai)
- ✅ Fallback mode (works without daemon)

### Compatibility

All existing commands work:
- ✅ `arc-ask "question"`
- ✅ `cat file | arc-ask "prompt"`
- ✅ `arc-ask --pane dev:1.0`
- ✅ `arc-ask --context README.md`

## Configuration

```bash
# Set arc-ai socket path
export ARC_AI_SOCKET="~/.config/arc/ai/daemon.sock"
```

## Performance

| Mode | Startup | Capabilities |
|------|---------|--------------|
| arc-ai daemon | Fast | Full Pi power |
| Fallback (direct Pi) | Medium | Basic Q&A |

## Examples

### Debug production issues

```bash
# Capture recent logs and analyze
arc-tmux follow --pane prod:logs.0 --lines 500 | \
  arc-ask "Find the root cause of these errors" --tools tmux
```

### Code review

```bash
# Review staged changes
git diff --staged | arc-ask "Review these changes" --context CONTRIBUTING.md
```

### Security audit

```bash
# Comprehensive security check
arc-security audit --json | \
  arc-ask "Create remediation plan for these vulnerabilities" --tools security
```

## Troubleshooting

### "arc-ai daemon not running"

```bash
# Start the daemon
arc-ai start &

# Or use fallback mode (slower)
arc-ask "question"  # Will use direct Pi execution
```

### "Pi not found"

```bash
# Install Pi globally
npm install -g @mariozechner/pi-coding-agent
```

## See Also

- [arc-ai](../arc-ai/) - AI bridge daemon
- [arc-tmux](../arc-tmux/) - Tmux integration
- [arc-security](../arc-security/) - Security scanning
- [arc-deps](../arc-deps/) - Dependency management

## License

MIT
