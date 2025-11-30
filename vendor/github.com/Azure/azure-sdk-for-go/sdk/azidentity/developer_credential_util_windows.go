// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azidentity

import (
	"context"
	"os"
	"os/exec"
	"syscall"
)

func buildCmd(ctx context.Context, credName, command string) (*exec.Cmd, error) {
	dir := os.Getenv("SYSTEMROOT")
	if dir == "" {
		return nil, newCredentialUnavailableError(credName, `environment variable "SYSTEMROOT" has no value`)
	}
	cmd := exec.CommandContext(ctx, "cmd.exe")
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: "/c " + command}
	return cmd, nil
}
