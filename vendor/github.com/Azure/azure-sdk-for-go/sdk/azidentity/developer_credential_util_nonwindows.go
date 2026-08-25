// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

//go:build !windows

package azidentity

import (
	"context"
	"os/exec"
)

func buildCmd(ctx context.Context, _, command string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Dir = "/bin"
	return cmd, nil
}
