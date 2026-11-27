// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azidentity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

// cliTimeout is the default timeout for authentication attempts via CLI tools
const cliTimeout = 10 * time.Second

// executor runs a command and returns its output or an error
type executor func(ctx context.Context, credName, command string) ([]byte, error)

var shellExec = func(ctx context.Context, credName, command string) ([]byte, error) {
	// set a default timeout for this authentication iff the caller hasn't done so already
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		ctx, cancel = context.WithTimeout(ctx, cliTimeout)
		defer cancel()
	}
	cmd, err := buildCmd(ctx, credName, command)
	if err != nil {
		return nil, err
	}
	cmd.Env = os.Environ()
	stderr := bytes.Buffer{}
	cmd.Stderr = &stderr
	cmd.WaitDelay = 100 * time.Millisecond

	stdout, err := cmd.Output()
	if errors.Is(err, exec.ErrWaitDelay) && len(stdout) > 0 {
		// The child process wrote to stdout and exited without closing it.
		// Swallow this error and return stdout because it may contain a token.
		return stdout, nil
	}
	if err != nil {
		msg := strings.Trim(stderr.String(), "\r\n")
		var exErr *exec.ExitError
		if errors.As(err, &exErr) && exErr.ExitCode() == 127 || strings.Contains(msg, "' is not recognized") {
			return nil, newCredentialUnavailableError(credName, "executable not found on path")
		}
		switch credName {
		case credNameAzureDeveloperCLI:
			msg = extractAzdError(msg)
		case credNameAzurePowerShell:
			if strings.Contains(msg, "Connect-AzAccount") {
				msg = `Please run "Connect-AzAccount" to set up an account`
			}
			if strings.Contains(msg, noAzAccountModule) {
				msg = noAzAccountModule
			}
		}
		if msg == "" {
			msg = err.Error()
		}
		return nil, newAuthenticationFailedError(credName, msg, nil)
	}

	return stdout, nil
}

// unavailableIfInDAC returns err or, if the credential was invoked by DefaultAzureCredential, a
// credentialUnavailableError having the same message. This ensures DefaultAzureCredential will try
// the next credential in its chain (another developer credential).
func unavailableIfInDAC(err error, inDefaultChain bool) error {
	if err != nil && inDefaultChain && !errors.As(err, new(credentialUnavailable)) {
		err = NewCredentialUnavailableError(err.Error())
	}
	return err
}

// validScope is for credentials authenticating via external tools. The authority validates scopes for all other credentials.
func validScope(scope string) bool {
	for _, r := range scope {
		if !alphanumeric(r) && r != '.' && r != '-' && r != '_' && r != '/' && r != ':' {
			return false
		}
	}
	return true
}

// extractAzdError extracts a human-readable error message from azd's stderr JSON output.
// azd writes JSON error messages to stderr. The format depends on the azd version:
//   - v1.23.7+: {"error":"...","message":"...","suggestion":"..."} (may be preceded by an empty consoleMessage line)
//   - pre-v1.23.7: {"type":"consoleMessage","data":{"message":"..."}}
//
// Prefer the structured "error" format, fall back to legacy consoleMessage.
func extractAzdError(msg string) string {
	lines := strings.Split(msg, "\n")
	fallback := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)

		var errObj struct {
			Error string `json:"error"`
		}
		if json.Unmarshal([]byte(line), &errObj) == nil && errObj.Error != "" {
			return errObj.Error
		}

		if fallback == "" {
			var obj struct {
				Data struct {
					Message string `json:"message"`
				} `json:"data"`
			}
			if json.Unmarshal([]byte(line), &obj) == nil {
				if m := strings.TrimSpace(obj.Data.Message); m != "" {
					fallback = m
				}
			}
		}
	}
	if fallback != "" {
		return fallback
	}
	return msg
}
