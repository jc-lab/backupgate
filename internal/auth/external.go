// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ExternalProvider authenticates by executing an external program.
// Placeholders in args: %u = username, %p = password, %k = key.
type ExternalProvider struct {
	command string
	args    []string
	timeout time.Duration
}

// NewExternalProvider creates an ExternalProvider with the given command template.
func NewExternalProvider(command string, args []string, timeout time.Duration) *ExternalProvider {
	return &ExternalProvider{
		command: command,
		args:    args,
		timeout: timeout,
	}
}

// Authenticate executes the external program with substituted placeholders.
// exit code 0 = success, non-zero = failure.
func (p *ExternalProvider) Authenticate(ctx context.Context, req *AuthRequest) (string, error) {
	if p.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, p.command, p.buildArgs(req)...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", fmt.Errorf("%w: external program %q timed out: %v", ErrAuthentication, p.command, ctx.Err())
	}
	if err != nil {
		exitCode := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		if len(output) > 0 {
			return "", fmt.Errorf("%v: %s", formatError(p.command, exitCode), strings.TrimSpace(string(output)))
		}
		return "", formatError(p.command, exitCode)
	}
	return req.Username, nil
}

// buildArgs replaces %u, %p, %k placeholders in the configured args.
func (p *ExternalProvider) buildArgs(req *AuthRequest) []string {
	result := make([]string, len(p.args))
	for i, arg := range p.args {
		replaced := arg
		replaced = replaceAll(replaced, "%u", req.Username)
		replaced = replaceAll(replaced, "%p", req.Password)
		replaced = replaceAll(replaced, "%k", req.Key)
		result[i] = replaced
	}
	return result
}

func replaceAll(s, old, new string) string {
	result := ""
	for {
		idx := indexOf(s, old)
		if idx == -1 {
			result += s
			break
		}
		result += s[:idx] + new
		s = s[idx+len(old):]
	}
	return result
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// formatError wraps an authentication failure from the external program.
func formatError(command string, exitCode int) error {
	return fmt.Errorf("%w: external program %q exited with code %d", ErrAuthentication, command, exitCode)
}
