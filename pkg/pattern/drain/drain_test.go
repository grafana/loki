package drain

import (
	"bufio"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

var drainConfig = &Config{
	// At training, if at the depth of LogClusterDepth there is a cluster with
	// similarity coefficient greater that SimTh, then the log message is added
	// to that cluster. Otherwise, a new cluster is created.
	//
	// LogClusterDepth should be equal to the number of constant tokens from
	// the beginning of the message that likely determine the message contents.
	//
	//  > In this step, Drain traverses from a 1-st layer node, which
	//  > is searched in step 2, to a leaf node. This step is based on
	//  > the assumption that tokens in the beginning positions of a log
	//  > message are more likely to be constants. Specifically, Drain
	//  > selects the next internal node by the tokens in the beginning
	//  > positions of the log message
	LogClusterDepth: 8,
	// SimTh is basically a ratio of matching/total in the cluster.
	// Cluster tokens: "foo <*> bar fred"
	//       Log line: "foo bar baz qux"
	//                  *   *   *   x
	// Similarity of these sequences is 0.75 (the distance)
	// Both SimTh and MaxClusterDepth impact branching factor: the greater
	// MaxClusterDepth and SimTh, the less the chance that there will be
	// "similar" clusters, but the greater the footprint.
	SimTh:       0.4,
	MaxChildren: 100,
	ParamString: "<_>",
	MaxClusters: 300,
}

func TestDrain_TrainExtractsPatterns(t *testing.T) {
	tests := []struct {
		name      string
		drain     *Drain
		inputFile string
		patterns  []string
	}{
		{
			// High variation leads to many patterns including some that are too generic (many tokens matched) and some that are too specific (too few matchers)
			name:      "Generate patterns on high variation logfmt logs",
			drain:     New(drainConfig),
			inputFile: "testdata/agent-logfmt.txt",
			patterns: []string{
				"ts=2024-04-16T15:10:43.192290389Z caller=filetargetmanager.go:361 level=info component=logs logs_config=default msg=\"Adding target\" key=\"/var/log/pods/*19a1cce8-5f04-46e0-a124-292b0dd9b343/testcoordinator/*.log:{batch_kubernetes_io_controller_uid=\\\"25ec5edf-f78e-468b-b6f3-3b9685f0cc8f\\\", batch_kubernetes_io_job_name=\\\"testcoordinator-job-2665838\\\", container=\\\"testcoordinator\\\", controller_uid=\\\"25ec5edf-f78e-468b-b6f3-3b9685f0cc8f\\\", job=\\\"k6-cloud/testcoordinator\\\", job_name=\\\"testcoordinator-job-2665838\\\", name=\\\"testcoordinator\\\", namespace=\\\"k6-cloud\\\", pod=\\\"testcoordinator-job-2665838-9g8ds\\\"}\"",
				"ts=2024-04-16T15:10:43.551543875Z caller=filetargetmanager.go:397 level=info component=logs logs_config=default msg=\"Removing target\" key=\"/var/log/pods/*35649bfd-52ff-4281-9294-5f65fd5a89fc/marketplaces-api/*.log:{container=\\\"marketplaces-api\\\", job=\\\"grafana-com/marketplaces-api\\\", name=\\\"marketplaces-api\\\", namespace=\\\"grafana-com\\\", pod=\\\"marketplaces-api-f67ff7567-gqrvb\\\", pod_template_hash=\\\"f67ff7567\\\"}\"",
				"<_> caller=filetargetmanager.go:361 level=info component=logs logs_config=default msg=\"Adding target\" <_> container=\\\"kube-proxy\\\", <_> namespace=\\\"kube-system\\\", pod=\\\"kube-proxy-gke-ops-us-east-0-main-n2s32-1-1dd39c-32ae1dde-hmhw\\\", tier=\\\"node\\\"}\"",
				"<_> caller=filetarget.go:192 level=info component=logs logs_config=default msg=\"filetarget: watcher closed, tailer stopped, positions saved\" <_>",
				"<_> caller=tailer.go:164 level=info component=logs logs_config=default component=tailer msg=\"tail routine: tail channel closed, stopping tailer\" <_> reason=null",
				"<_> caller=tailer.go:207 level=info component=logs logs_config=default component=tailer msg=\"skipping update of position for a file which does not currently exist\" <_>",
				"<_> caller=log.go:168 component=logs logs_config=default level=info msg=\"Successfully reopened <_>",
				"<_> caller=logfmt.go:139 level=error component=logs logs_config=default component=file_pipeline component=stage type=logfmt msg=\"failed to decode logfmt\" err=\"bufio.Scanner: token too long\"",
				"<_> caller=filetargetmanager.go:181 level=info component=logs logs_config=default msg=\"received file watcher event\" <_> op=CREATE",
				"<_> caller=logfmt.go:139 level=error component=logs logs_config=default component=file_pipeline component=stage type=logfmt msg=\"failed to decode logfmt\" err=\"logfmt syntax error at pos <_> on line 1: unexpected '\\\"'\"",
				"<_> caller=filetarget.go:326 level=info component=logs logs_config=default msg=\"removing directory from watcher\" <_>",
				"<_> <_> level=info component=logs logs_config=default component=tailer <_> <_> <_> <_>",
				"<_> caller=log.go:168 component=logs logs_config=default level=info <_> <_> <_> <_> <_>",
				"<_> caller=filetarget.go:313 level=info component=logs logs_config=default msg=\"watching new directory\" <_>",
				"<_> <_> level=info component=logs logs_config=default <_> target\" <_> conprof=\\\"true\\\", <_> <_> job=\\\"hosted-grafana/grafana\\\", name=\\\"grafana\\\", namespace=\\\"hosted-grafana\\\", <_> plan=\\\"free\\\", <_> <_> <_> <_> <_>",
				"<_> level=info msg=\"finished node evaluation\" controller_id=module.http.cloudwatch_pipelines <_> <_>",
				"2024-04-16 15:10:42.555 ts=2024-04-16T15:10:42.555230437Z level=info msg=\"finished node evaluation\" controller_id=module.http.cloudwatch_pipelines node_id=prometheus.scrape.stack_378175_cloudwatch_notags duration=38.545339ms",
			},
		},
		{
			// Lower variation leads to fewer patterns including some with limited value (single lines, no matchers)
			name:      "Generate patterns on low variation logfmt logs",
			drain:     New(drainConfig),
			inputFile: "testdata/ingester-logfmt.txt",
			patterns: []string{
				"<_> caller=head.go:216 level=debug tenant=987678 msg=\"profile is empty after delta computation\" metricName=memory",
				"ts=2024-04-17T09:52:46.363974185Z caller=http.go:194 level=debug traceID=1b48f5156a61ca69 msg=\"GET /debug/pprof/delta_mutex (200) 1.161082ms\"",
				"<_> caller=http.go:194 level=debug <_> <_> msg=\"POST /ingester.v1.IngesterService/Push (200) <_>",
			},
		},
		{
			// Lower variation logs in json leads to a high number of patterns with very few matchers
			name:      "Generate patterns on json formatted logs",
			drain:     New(drainConfig),
			inputFile: "testdata/drone-json.txt",
			patterns: []string{
				"{\"id\":\"luflyGZvZnLzhQEH\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T14:55:56Z\"}",
				"{\"id\":\"luflyGZvZnLzhQEH\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T14:55:56Z\"}",
				"{\"id\":\"luflyGZvZnLzhQEH\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T14:55:56Z\"}",
				"{\"id\":\"D4Oh1ivB6cdLWa08\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T14:48:52Z\"}",
				"{\"id\":\"q62wCcIkEOueqFKF\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T15:03:28Z\"}",
				"{\"id\":\"m6SpYHzdXrDAFqDR\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T15:02:58Z\"}",
				"{\"id\":\"T0I8Dsnw3uSi3Gal\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T15:02:28Z\"}",
				"{\"id\":\"9eA72xOtx8kzMhXn\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T15:01:58Z\"}",
				"{\"id\":\"pet7QVfO1yE8fk56\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T15:01:28Z\"}",
				"{\"id\":\"15eSzaEG0enf86Kl\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T15:00:57Z\"}",
				"{\"id\":\"JO1OT5ADoNA8NYqr\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T15:00:27Z\"}",
				"{\"id\":\"Xz2OCJhgeBSRFyoN\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T14:59:57Z\"}",
				"{\"id\":\"pPc2ORUhHAhFgBg3\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T14:59:27Z\"}",
				"{\"id\":\"4G6Srn6lSwzYrx19\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T14:58:57Z\"}",
				"{\"id\":\"1Lu90T1fWzsWOKlc\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T14:58:27Z\"}",
				"{\"id\":\"4XjwwNoOwZFaWePQ\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T14:57:57Z\"}",
				"{\"id\":\"IQy23J3NON0BV10V\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T14:57:26Z\"}",
				"{\"id\":\"FQ8wCQfaR9W387cH\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T14:56:56Z\"}",
				"{\"id\":\"Hhwn7ecXjxF67DG6\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T14:56:26Z\"}",
				"{\"id\":\"q20GZcvyzMwrTGx5\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T14:55:26Z\"}",
				"{\"id\":\"3K61Yf6ImKYexoFx\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T14:54:56Z\"}",
				"{\"id\":\"SmbOO0l5aADX9BaQ\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T14:54:23Z\"}",
				"{\"id\":\"96TvvsMzSkkaW8oW\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T14:53:53Z\"}",
				"{\"id\":\"C7aYn8cb4NCrkkYI\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T14:53:23Z\"}",
				"{\"id\":\"CMG7ZwwYqNPBonAn\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T14:52:53Z\"}",
				"{\"id\":\"focV9BzODwRbWwKE\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T14:52:23Z\"}",
				"{\"id\":\"HphRnJOM8uYohf1p\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T14:51:53Z\"}",
				"{\"id\":\"m3n8GndhG45uGIQA\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T14:51:23Z\"}",
				"{\"id\":\"nTO38tWtnvRWRl1G\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T14:50:52Z\"}",
				"{\"id\":\"5qEIzErDfiALVPAN\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T14:50:22Z\"}",
				"{\"id\":\"q61oHTtF4MMiQVGH\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T14:49:52Z\"}",
				"{\"id\":\"4rNxIlhDKxGgzBHe\",\"level\":\"debug\",\"msg\":\"check capacity complete\",\"time\":\"2024-04-16T14:49:22Z\"}",
				"<_> capacity changes <_>",
				"{\"id\":\"D4Oh1ivB6cdLWa08\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T14:48:52Z\"}",
				"{\"id\":\"q62wCcIkEOueqFKF\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T15:03:28Z\"}",
				"{\"id\":\"m6SpYHzdXrDAFqDR\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T15:02:58Z\"}",
				"{\"id\":\"T0I8Dsnw3uSi3Gal\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T15:02:28Z\"}",
				"{\"id\":\"9eA72xOtx8kzMhXn\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T15:01:58Z\"}",
				"{\"id\":\"pet7QVfO1yE8fk56\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T15:01:28Z\"}",
				"{\"id\":\"15eSzaEG0enf86Kl\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T15:00:57Z\"}",
				"{\"id\":\"JO1OT5ADoNA8NYqr\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T15:00:27Z\"}",
				"{\"id\":\"Xz2OCJhgeBSRFyoN\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T14:59:57Z\"}",
				"{\"id\":\"pPc2ORUhHAhFgBg3\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T14:59:27Z\"}",
				"{\"id\":\"4G6Srn6lSwzYrx19\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T14:58:57Z\"}",
				"{\"id\":\"1Lu90T1fWzsWOKlc\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T14:58:27Z\"}",
				"{\"id\":\"4XjwwNoOwZFaWePQ\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T14:57:57Z\"}",
				"{\"id\":\"IQy23J3NON0BV10V\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T14:57:26Z\"}",
				"{\"id\":\"FQ8wCQfaR9W387cH\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T14:56:56Z\"}",
				"{\"id\":\"Hhwn7ecXjxF67DG6\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T14:56:26Z\"}",
				"{\"id\":\"luflyGZvZnLzhQEH\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T14:55:56Z\"}",
				"{\"id\":\"q20GZcvyzMwrTGx5\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T14:55:26Z\"}",
				"{\"id\":\"3K61Yf6ImKYexoFx\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T14:54:56Z\"}",
				"{\"id\":\"SmbOO0l5aADX9BaQ\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T14:54:23Z\"}",
				"{\"id\":\"96TvvsMzSkkaW8oW\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T14:53:53Z\"}",
				"{\"id\":\"C7aYn8cb4NCrkkYI\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T14:53:23Z\"}",
				"{\"id\":\"CMG7ZwwYqNPBonAn\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T14:52:53Z\"}",
				"{\"id\":\"focV9BzODwRbWwKE\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T14:52:23Z\"}",
				"{\"id\":\"HphRnJOM8uYohf1p\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T14:51:53Z\"}",
				"{\"id\":\"m3n8GndhG45uGIQA\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T14:51:23Z\"}",
				"{\"id\":\"nTO38tWtnvRWRl1G\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T14:50:52Z\"}",
				"{\"id\":\"5qEIzErDfiALVPAN\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T14:50:22Z\"}",
				"{\"id\":\"q61oHTtF4MMiQVGH\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T14:49:52Z\"}",
				"{\"id\":\"4rNxIlhDKxGgzBHe\",\"level\":\"debug\",\"max-pool\":4,\"min-pool\":0,\"msg\":\"check capacity\",\"pending-builds\":0,\"running-builds\":0,\"server-buffer\":0,\"server-capacity\":0,\"server-count\":0,\"time\":\"2024-04-16T14:49:22Z\"}",
				"{\"id\":\"D4Oh1ivB6cdLWa08\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T14:48:52Z\"}",
				"{\"id\":\"q62wCcIkEOueqFKF\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T15:03:28Z\"}",
				"{\"id\":\"m6SpYHzdXrDAFqDR\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T15:02:58Z\"}",
				"{\"id\":\"T0I8Dsnw3uSi3Gal\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T15:02:28Z\"}",
				"{\"id\":\"9eA72xOtx8kzMhXn\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T15:01:58Z\"}",
				"{\"id\":\"pet7QVfO1yE8fk56\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T15:01:27Z\"}",
				"{\"id\":\"15eSzaEG0enf86Kl\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T15:00:57Z\"}",
				"{\"id\":\"JO1OT5ADoNA8NYqr\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T15:00:27Z\"}",
				"{\"id\":\"Xz2OCJhgeBSRFyoN\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T14:59:57Z\"}",
				"{\"id\":\"pPc2ORUhHAhFgBg3\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T14:59:27Z\"}",
				"{\"id\":\"4G6Srn6lSwzYrx19\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T14:58:57Z\"}",
				"{\"id\":\"1Lu90T1fWzsWOKlc\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T14:58:27Z\"}",
				"{\"id\":\"4XjwwNoOwZFaWePQ\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T14:57:57Z\"}",
				"{\"id\":\"IQy23J3NON0BV10V\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T14:57:26Z\"}",
				"{\"id\":\"FQ8wCQfaR9W387cH\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T14:56:56Z\"}",
				"{\"id\":\"Hhwn7ecXjxF67DG6\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T14:56:26Z\"}",
				"{\"id\":\"q20GZcvyzMwrTGx5\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T14:55:26Z\"}",
				"{\"id\":\"3K61Yf6ImKYexoFx\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T14:54:56Z\"}",
				"{\"id\":\"SmbOO0l5aADX9BaQ\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T14:54:23Z\"}",
				"{\"id\":\"96TvvsMzSkkaW8oW\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T14:53:53Z\"}",
				"{\"id\":\"C7aYn8cb4NCrkkYI\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T14:53:23Z\"}",
				"{\"id\":\"CMG7ZwwYqNPBonAn\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T14:52:53Z\"}",
				"{\"id\":\"focV9BzODwRbWwKE\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T14:52:23Z\"}",
				"{\"id\":\"HphRnJOM8uYohf1p\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T14:51:53Z\"}",
				"{\"id\":\"m3n8GndhG45uGIQA\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T14:51:22Z\"}",
				"{\"id\":\"nTO38tWtnvRWRl1G\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T14:50:52Z\"}",
				"{\"id\":\"5qEIzErDfiALVPAN\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T14:50:22Z\"}",
				"{\"id\":\"q61oHTtF4MMiQVGH\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T14:49:52Z\"}",
				"{\"id\":\"4rNxIlhDKxGgzBHe\",\"level\":\"debug\",\"msg\":\"calculate server capacity\",\"time\":\"2024-04-16T14:49:22Z\"}",
				"{\"id\":\"D4Oh1ivB6cdLWa08\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T14:48:52Z\"}",
				"{\"id\":\"q62wCcIkEOueqFKF\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T15:03:28Z\"}",
				"{\"id\":\"m6SpYHzdXrDAFqDR\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T15:02:58Z\"}",
				"{\"id\":\"T0I8Dsnw3uSi3Gal\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T15:02:28Z\"}",
				"{\"id\":\"9eA72xOtx8kzMhXn\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T15:01:58Z\"}",
				"{\"id\":\"pet7QVfO1yE8fk56\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T15:01:27Z\"}",
				"{\"id\":\"15eSzaEG0enf86Kl\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T15:00:57Z\"}",
				"{\"id\":\"JO1OT5ADoNA8NYqr\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T15:00:27Z\"}",
				"{\"id\":\"Xz2OCJhgeBSRFyoN\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T14:59:57Z\"}",
				"{\"id\":\"pPc2ORUhHAhFgBg3\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T14:59:27Z\"}",
				"{\"id\":\"4G6Srn6lSwzYrx19\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T14:58:57Z\"}",
				"{\"id\":\"1Lu90T1fWzsWOKlc\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T14:58:27Z\"}",
				"{\"id\":\"4XjwwNoOwZFaWePQ\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T14:57:56Z\"}",
				"{\"id\":\"IQy23J3NON0BV10V\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T14:57:26Z\"}",
				"{\"id\":\"FQ8wCQfaR9W387cH\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T14:56:56Z\"}",
				"{\"id\":\"Hhwn7ecXjxF67DG6\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T14:56:26Z\"}",
				"{\"id\":\"q20GZcvyzMwrTGx5\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T14:55:26Z\"}",
				"{\"id\":\"3K61Yf6ImKYexoFx\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T14:54:53Z\"}",
				"{\"id\":\"SmbOO0l5aADX9BaQ\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T14:54:23Z\"}",
				"{\"id\":\"96TvvsMzSkkaW8oW\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T14:53:53Z\"}",
				"{\"id\":\"C7aYn8cb4NCrkkYI\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T14:53:23Z\"}",
				"{\"id\":\"CMG7ZwwYqNPBonAn\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T14:52:53Z\"}",
				"{\"id\":\"focV9BzODwRbWwKE\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T14:52:23Z\"}",
				"{\"id\":\"HphRnJOM8uYohf1p\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T14:51:53Z\"}",
				"{\"id\":\"m3n8GndhG45uGIQA\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T14:51:22Z\"}",
				"{\"id\":\"nTO38tWtnvRWRl1G\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T14:50:52Z\"}",
				"{\"id\":\"5qEIzErDfiALVPAN\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T14:50:22Z\"}",
				"{\"id\":\"q61oHTtF4MMiQVGH\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T14:49:52Z\"}",
				"{\"id\":\"4rNxIlhDKxGgzBHe\",\"level\":\"debug\",\"msg\":\"calculate unfinished jobs\",\"time\":\"2024-04-16T14:49:22Z\"}",
				"<_> <_> (flow; linux; helm)\"}",
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
