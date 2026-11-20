package query

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestMergeJobsClosesPartFiles(t *testing.T) {
	if os.Getenv("LOKI_TEST_MERGE_FILE_LIMIT") != "1" {
		executable, err := os.Executable()
		require.NoError(t, err)
		cmd := exec.Command(executable, "-test.run=^TestMergeJobsClosesPartFiles$", "-test.timeout=30s")
		cmd.Env = append(os.Environ(), "LOKI_TEST_MERGE_FILE_LIMIT=1")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
		return
	}

	var limit unix.Rlimit
	require.NoError(t, unix.Getrlimit(unix.RLIMIT_NOFILE, &limit))
	limit.Cur = 64
	require.NoError(t, unix.Setrlimit(unix.RLIMIT_NOFILE, &limit))

	for _, keepParts := range []bool{false, true} {
		t.Run(fmt.Sprintf("keep_parts=%t", keepParts), func(t *testing.T) {
			q := &Query{
				Start:            time.Unix(0, 0),
				End:              time.Unix(0, 0).Add(128 * time.Hour),
				Forward:          true,
				ParallelDuration: time.Hour,
				PartPathPrefix:   filepath.Join(t.TempDir(), "export"),
				MergeParts:       true,
				KeepParts:        keepParts,
			}
			jobs := q.parallelJobs()
			var expected bytes.Buffer
			for i, job := range jobs {
				line := fmt.Sprintf("line %d\n", i)
				expected.WriteString(line)
				require.NoError(t, os.WriteFile(job.q.outputFilename(), []byte(line), 0o600))
				close(job.done)
			}

			merged, err := os.CreateTemp(t.TempDir(), "merged")
			require.NoError(t, err)
			previousStdout := os.Stdout
			os.Stdout = merged
			t.Cleanup(func() {
				os.Stdout = previousStdout
				require.NoError(t, merged.Close())
			})

			require.NoError(t, q.mergeJobs(jobs))
			actual, err := os.ReadFile(merged.Name())
			require.NoError(t, err)
			require.Equal(t, expected.String(), string(actual))
			for _, job := range jobs {
				if keepParts {
					require.FileExists(t, job.q.outputFilename())
				} else {
					require.NoFileExists(t, job.q.outputFilename())
				}
			}
		})
	}
}

func TestMergeJobsCopyErrorKeepsPart(t *testing.T) {
	q := &Query{MergeParts: true, PartPathPrefix: filepath.Join(t.TempDir(), "export")}
	job := newParallelJob(q)
	require.NoError(t, os.WriteFile(q.outputFilename(), []byte("line\n"), 0o600))
	close(job.done)

	closedOutput, err := os.CreateTemp(t.TempDir(), "closed")
	require.NoError(t, err)
	require.NoError(t, closedOutput.Close())
	previousStdout := os.Stdout
	os.Stdout = closedOutput
	t.Cleanup(func() { os.Stdout = previousStdout })

	err = q.mergeJobs([]*parallelJob{job})
	require.ErrorIs(t, err, os.ErrClosed)
	require.ErrorContains(t, err, "copying file error")
	require.FileExists(t, q.outputFilename())
}
