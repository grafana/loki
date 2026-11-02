package logline

import (
	"flag"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v4/pkg/logline/format"
)

func TestCompactionConfig_FlagDefaults(t *testing.T) {
	var cfg CompactionConfig
	fs := flag.NewFlagSet("test", flag.PanicOnError)
	cfg.RegisterFlagsWithPrefix("logline-compaction", fs)
	require.NoError(t, fs.Parse(nil))

	require.EqualValues(t, DefaultMaxJobSize, cfg.MaxJobSize)
	require.Equal(t, DefaultJobTimeout, cfg.JobTimeout)
	require.Equal(t, DefaultDownloadConcurrency, cfg.DownloadConcurrency)
	require.Equal(t, PostingsEncodingDeltaVarInt, cfg.PostingsEncoding)
	require.Empty(t, cfg.JobStateDir)
	require.Empty(t, cfg.SchedulerAddress)
}

func TestCompactionConfig_MaxJobSizeTakesUnits(t *testing.T) {
	var cfg CompactionConfig
	fs := flag.NewFlagSet("test", flag.PanicOnError)
	cfg.RegisterFlagsWithPrefix("logline-compaction", fs)
	require.NoError(t, fs.Parse([]string{"-logline-compaction.max-job-size=100GiB"}))

	require.EqualValues(t, 100<<30, cfg.MaxJobSize)
}

func TestCompactionConfig_ValidateScheduler(t *testing.T) {
	valid := func() CompactionConfig {
		return CompactionConfig{MaxJobSize: DefaultMaxJobSize, JobStateDir: t.TempDir()}
	}

	cfg := valid()
	require.NoError(t, cfg.ValidateScheduler())
	require.Equal(t, DefaultJobTimeout, cfg.JobTimeout)

	cfg = valid()
	cfg.JobStateDir = ""
	require.ErrorContains(t, cfg.ValidateScheduler(), "job_state_dir")

	cfg = valid()
	cfg.MaxJobSize = 0
	require.ErrorContains(t, cfg.ValidateScheduler(), "max_job_size")

	cfg = valid()
	cfg.JobTimeout = -time.Second
	require.ErrorContains(t, cfg.ValidateScheduler(), "job_timeout")

	// The scheduler does not need compactor settings.
	cfg = valid()
	require.Empty(t, cfg.SchedulerAddress)
	require.NoError(t, cfg.ValidateScheduler())
}

func TestCompactionConfig_ValidateCompactor(t *testing.T) {
	cfg := CompactionConfig{}
	require.ErrorContains(t, cfg.ValidateCompactor(), "scheduler_address")

	// The compactor does not need scheduler settings.
	cfg = CompactionConfig{SchedulerAddress: "http://logline-scheduler:3100"}
	require.NoError(t, cfg.ValidateCompactor())
	require.Equal(t, DefaultDownloadConcurrency, cfg.DownloadConcurrency)
	require.NotEmpty(t, cfg.CompactorID, "compactor_id defaults to the hostname")

	cfg = CompactionConfig{SchedulerAddress: "http://logline-scheduler:3100", DownloadConcurrency: -1}
	require.ErrorContains(t, cfg.ValidateCompactor(), "download_concurrency")

	cfg = CompactionConfig{SchedulerAddress: "http://logline-scheduler:3100"}
	require.NoError(t, cfg.ValidateCompactor())
	require.Equal(t, PostingsEncodingDeltaVarInt, cfg.PostingsEncoding)

	cfg = CompactionConfig{SchedulerAddress: "http://logline-scheduler:3100", PostingsEncoding: "zstd"}
	require.ErrorContains(t, cfg.ValidateCompactor(), "postings_encoding")
}

func TestCompactionConfig_WriterPostingsEncoding(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  format.PostingsEncoding
	}{
		{"", format.PostingsEncodingFastDeltaVarIntBlocked},
		{PostingsEncodingDeltaVarInt, format.PostingsEncodingFastDeltaVarIntBlocked},
		{PostingsEncodingEliasFano, format.PostingsEncodingFastEliasFanoBlocked},
	} {
		t.Run(tc.value, func(t *testing.T) {
			cfg := CompactionConfig{PostingsEncoding: tc.value}
			got, err := cfg.WriterPostingsEncoding()
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	cfg := CompactionConfig{PostingsEncoding: "Elias-Fano"}
	_, err := cfg.WriterPostingsEncoding()
	require.ErrorContains(t, err, `got "Elias-Fano"`)
}

func TestRetentionConfig_Validate(t *testing.T) {
	cfg := RetentionConfig{}
	require.NoError(t, cfg.Validate())
	require.Equal(t, DefaultCompactedSourceDeleteDelay, cfg.CompactedSourceDeleteDelay)
	require.Equal(t, DefaultDeleteConcurrency, cfg.DeleteConcurrency)

	cfg = RetentionConfig{CompactedSourceDeleteDelay: -time.Second}
	require.ErrorContains(t, cfg.Validate(), "compacted_source_delete_delay")

	cfg = RetentionConfig{DeleteConcurrency: -1}
	require.ErrorContains(t, cfg.Validate(), "delete_concurrency")
}
