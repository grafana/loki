package drain

import (
	"bufio"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDrain_TrainExtractsPatterns(t *testing.T) {
	tests := []struct {
		name      string
		drain     *Drain
		inputFile string
		patterns  []string
	}{
		{
			// High variation leads to many patterns including some that are too generic (many tokens matched) and some that are too specific (too few matchers)
			name:      `Generate patterns on high variation logfmt logs`,
			drain:     New(DefaultConfig()),
			inputFile: `testdata/agent-logfmt.txt`,
			patterns: []string{
				`ts=2024-04-16T15:10:43.192290389Z caller=filetargetmanager.go:361 level=info component=logs logs_config=default msg="Adding target" key="/var/log/pods/*19a1cce8-5f04-46e0-a124-292b0dd9b343/testcoordinator/*.log:{batch_kubernetes_io_controller_uid=\"25ec5edf-f78e-468b-b6f3-3b9685f0cc8f\", batch_kubernetes_io_job_name=\"testcoordinator-job-2665838\", container=\"testcoordinator\", controller_uid=\"25ec5edf-f78e-468b-b6f3-3b9685f0cc8f\", job=\"k6-cloud/testcoordinator\", job_name=\"testcoordinator-job-2665838\", name=\"testcoordinator\", namespace=\"k6-cloud\", pod=\"testcoordinator-job-2665838-9g8ds\"}"`,
				`<_> <_> level=info component=logs logs_config=default <_> target" <_> <_> <_> <_> <_> <_>`,
				`<_> caller=filetarget.go:192 level=info component=logs logs_config=default msg="filetarget: watcher closed, tailer stopped, positions saved" <_>`,
				`<_> caller=tailer.go:164 level=info component=logs logs_config=default component=tailer msg="tail routine: tail channel closed, stopping tailer" <_> reason=null`,
				`<_> caller=tailer.go:207 level=info component=logs logs_config=default component=tailer msg="skipping update of position for a file which does not currently exist" <_>`,
				`<_> caller=log.go:168 component=logs logs_config=default level=info msg="Successfully reopened <_>`,
				`<_> caller=logfmt.go:139 level=error component=logs logs_config=default component=file_pipeline component=stage type=logfmt msg="failed to decode logfmt" err="bufio.Scanner: token too long"`,
				`<_> caller=filetargetmanager.go:181 level=info component=logs logs_config=default msg="received file watcher event" <_> op=CREATE`,
				`<_> caller=logfmt.go:139 level=error component=logs logs_config=default component=file_pipeline component=stage type=logfmt msg="failed to decode logfmt" err="logfmt syntax error at pos <_> on line 1: unexpected '\"'"`,
				`<_> <_> level=info component=logs logs_config=default <_> <_> <_> <_> <_>`,
				`<_> caller=log.go:168 component=logs logs_config=default level=info <_> <_> <_> <_> <_>`,
				`<_> caller=filetarget.go:313 level=info component=logs logs_config=default msg="watching new directory" <_>`,
				`<_> <_> level=info component=logs logs_config=default <_> target" <_> conprof=\"true\", <_> <_> job=\"hosted-grafana/grafana\", name=\"grafana\", namespace=\"hosted-grafana\", <_> plan=\"free\", <_> <_> <_> <_> <_>`,
				`<_> level=info msg="finished node evaluation" controller_id=module.http.cloudwatch_pipelines <_> <_>`,
				`2024-04-16 15:10:42.555 ts=2024-04-16T15:10:42.555230437Z level=info msg="finished node evaluation" controller_id=module.http.cloudwatch_pipelines node_id=prometheus.scrape.stack_378175_cloudwatch_notags duration=38.545339ms`,
			},
		},
		{
			// Lower variation leads to fewer patterns including some with limited value (single lines, no matchers)
			name:      `Generate patterns on low variation logfmt logs`,
			drain:     New(DefaultConfig()),
			inputFile: `testdata/ingester-logfmt.txt`,
			patterns: []string{
				`<_> caller=head.go:216 level=debug tenant=987678 msg="profile is empty after delta computation" metricName=memory`,
				`ts=2024-04-17T09:52:46.363974185Z caller=http.go:194 level=debug traceID=1b48f5156a61ca69 msg="GET /debug/pprof/delta_mutex (200) 1.161082ms"`,
				`<_> caller=http.go:194 level=debug <_> <_> msg="POST /ingester.v1.IngesterService/Push (200) <_>`, // A perfect log line: Abstracted the variable part but kept the constants.
			},
		},
		{
			// Lower variation logs in json leads to a high number of patterns with very few matchers
			name:      `Generate patterns on json formatted logs`,
			drain:     New(DefaultConfig()),
			inputFile: `testdata/drone-json.txt`,
			patterns: []string{
				`<_> capacity <_>`,
				`<_> capacity changes <_>`,
				`{"id":"D4Oh1ivB6cdLWa08","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T14:48:52Z"}`,
				`{"id":"q62wCcIkEOueqFKF","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T15:03:28Z"}`,
				`{"id":"m6SpYHzdXrDAFqDR","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T15:02:58Z"}`,
				`{"id":"T0I8Dsnw3uSi3Gal","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T15:02:28Z"}`,
				`{"id":"9eA72xOtx8kzMhXn","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T15:01:58Z"}`,
				`{"id":"pet7QVfO1yE8fk56","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T15:01:28Z"}`,
				`{"id":"15eSzaEG0enf86Kl","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T15:00:57Z"}`,
				`{"id":"JO1OT5ADoNA8NYqr","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T15:00:27Z"}`,
				`{"id":"Xz2OCJhgeBSRFyoN","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T14:59:57Z"}`,
				`{"id":"pPc2ORUhHAhFgBg3","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T14:59:27Z"}`,
				`{"id":"4G6Srn6lSwzYrx19","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T14:58:57Z"}`,
				`{"id":"1Lu90T1fWzsWOKlc","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T14:58:27Z"}`,
				`{"id":"4XjwwNoOwZFaWePQ","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T14:57:57Z"}`,
				`{"id":"IQy23J3NON0BV10V","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T14:57:26Z"}`,
				`{"id":"FQ8wCQfaR9W387cH","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T14:56:56Z"}`,
				`{"id":"Hhwn7ecXjxF67DG6","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T14:56:26Z"}`,
				`{"id":"luflyGZvZnLzhQEH","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T14:55:56Z"}`,
				`{"id":"q20GZcvyzMwrTGx5","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T14:55:26Z"}`,
				`{"id":"3K61Yf6ImKYexoFx","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T14:54:56Z"}`,
				`{"id":"SmbOO0l5aADX9BaQ","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T14:54:23Z"}`,
				`{"id":"96TvvsMzSkkaW8oW","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T14:53:53Z"}`,
				`{"id":"C7aYn8cb4NCrkkYI","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T14:53:23Z"}`,
				`{"id":"CMG7ZwwYqNPBonAn","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T14:52:53Z"}`,
				`{"id":"focV9BzODwRbWwKE","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T14:52:23Z"}`,
				`{"id":"HphRnJOM8uYohf1p","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T14:51:53Z"}`,
				`{"id":"m3n8GndhG45uGIQA","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T14:51:23Z"}`,
				`{"id":"nTO38tWtnvRWRl1G","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T14:50:52Z"}`,
				`{"id":"5qEIzErDfiALVPAN","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T14:50:22Z"}`,
				`{"id":"q61oHTtF4MMiQVGH","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T14:49:52Z"}`,
				`{"id":"4rNxIlhDKxGgzBHe","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"2024-04-16T14:49:22Z"}`,
				`<_> server <_>`,
				`<_> unfinished <_>`,
				`<_> <_> (flow; linux; helm)"}`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := os.Open(tt.inputFile)
			require.NoError(t, err)
			defer file.Close()

			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				line := scanner.Text()
				tt.drain.Train(line, 0)
			}

			var output []string
			clusters := tt.drain.Clusters()
			for _, cluster := range clusters {
				output = append(output, cluster.String())
			}

			require.Equal(t, tt.patterns, output)
		})
	}
}
