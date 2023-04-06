// Copyright 2015-2018 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Copied from pse_darwin.go

package pse

import (
	"fmt"
	"os"
	"os/exec"
)

// ProcUsage returns CPU usage
func ProcUsage(pcpu *float64, rss, vss *int64) error {
	pidStr := fmt.Sprintf("%d", os.Getpid())
	out, err := exec.Command("ps", "o", "pcpu=,rss=,vsz=", "-p", pidStr).Output()
	if err != nil {
		*rss, *vss = -1, -1
		return fmt.Errorf("ps call failed:%v", err)
	}
	fmt.Sscanf(string(out), "%f %d %d", pcpu, rss, vss)
	*rss *= 1024 // 1k blocks, want bytes.
	*vss *= 1024 // 1k blocks, want bytes.
	return nil
}
