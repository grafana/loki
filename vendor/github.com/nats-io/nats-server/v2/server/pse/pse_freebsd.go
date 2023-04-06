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

//go:build !amd64
// +build !amd64

package pse

/*
#include <sys/types.h>
#include <sys/sysctl.h>
#include <sys/user.h>
#include <stddef.h>
#include <unistd.h>

long pagetok(long size)
{
    int pageshift, pagesize;

    pagesize = getpagesize();
    pageshift = 0;

    while (pagesize > 1) {
        pageshift++;
        pagesize >>= 1;
    }

    return (size << pageshift);
}

int getusage(double *pcpu, unsigned int *rss, unsigned int *vss)
{
    int mib[4], ret;
    size_t len;
    struct kinfo_proc kp;

    len = 4;
    sysctlnametomib("kern.proc.pid", mib, &len);

    mib[3] = getpid();
    len = sizeof(kp);

    ret = sysctl(mib, 4, &kp, &len, NULL, 0);
    if (ret != 0) {
        return (errno);
    }

    *rss = pagetok(kp.ki_rssize);
    *vss = kp.ki_size;
    *pcpu = (double)kp.ki_pctcpu / FSCALE;

    return 0;
}

*/
import "C"

import (
	"syscall"
)

// This is a placeholder for now.
func ProcUsage(pcpu *float64, rss, vss *int64) error {
	var r, v C.uint
	var c C.double

	if ret := C.getusage(&c, &r, &v); ret != 0 {
		return syscall.Errno(ret)
	}

	*pcpu = float64(c)
	*rss = int64(r)
	*vss = int64(v)

	return nil
}
