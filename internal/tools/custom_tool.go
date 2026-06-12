package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// CustomTool wraps a user-defined shell command as a tool.Registry-compatible Tool.
// Env vars are pre-decrypted at load time and never re-read from the store.
type CustomTool struct {
	name        string
	description string
	parameters  map[string]any
	command     string
	workingDir  string
	timeout     time.Duration
	envPairs    []string // "KEY=value" pairs injected into the subprocess environment
}

func NewCustomTool(name, description string, parameters map[string]any, command, workingDir string, timeoutSeconds int, envVars map[string]string) *CustomTool {
	timeout := time.Duration(timeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	pairs := make([]string, 0, len(envVars))
	for k, v := range envVars {
		pairs = append(pairs, k+"="+v)
	}
	return &CustomTool{
		name:        name,
		description: description,
		parameters:  parameters,
		command:     command,
		workingDir:  workingDir,
		timeout:     timeout,
		envPairs:    pairs,
	}
}

func (t *CustomTool) Name() string             { return t.name }
func (t *CustomTool) Description() string      { return t.description }
func (t *CustomTool) Parameters() map[string]any { return t.parameters }

func (t *CustomTool) Execute(ctx context.Context, args map[string]any) *Result {
	tctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	// Route through the platform shell so the command can contain pipes and redirections.
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(tctx, "cmd", "/C", t.command)
	} else {
		cmd = exec.CommandContext(tctx, "sh", "-c", t.command)
	}
	if t.workingDir != "" {
		cmd.Dir = t.workingDir
	}

	// Inject pre-decrypted env vars.
	cmd.Env = append(cmd.Env, t.envPairs...)

	// Inject tool arguments as TOOL_ARG_<UPPER_NAME>=<json_value> env vars.
	for k, v := range args {
		var val string
		switch sv := v.(type) {
		case string:
			val = sv
		default:
			b, _ := json.Marshal(v)
			val = string(b)
		}
		envKey := "TOOL_ARG_" + strings.ToUpper(k)
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", envKey, val))
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		slog.Warn("custom_tool.exec_error",
			"tool", t.name, "error", err, "stderr", stderr.String())
		msg := fmt.Sprintf("command failed: %v", err)
		if s := strings.TrimSpace(stderr.String()); s != "" {
			msg += "\n" + s
		}
		return ErrorResult(msg)
	}

	out := strings.TrimRight(stdout.String(), "\n")
	if out == "" {
		out = "(no output)"
	}
	return NewResult(out)
}
