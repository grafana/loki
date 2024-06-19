package drain

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slices"

	"github.com/grafana/loki/v3/pkg/logql/log/pattern"
)

func TestDrain_TrainExtractsPatterns(t *testing.T) {
	t.Parallel()

	// Set this so the test will print the patterns found, in string slice format for easy copy-paste
	outputPatternsForTestUpdate := false

	tests := []struct {
		drain     *Drain
		inputFile string
		patterns  []string
	}{
		{
			drain:     New(DefaultConfig(), nil),
			inputFile: `testdata/agent-logfmt.txt`,
			patterns: []string{
				`ts=2024-04-16T15:10:42.<_> level=info msg="finished node evaluation" controller_id=module.http.cloudwatch_pipelines node_id=prometheus.scrape.<_> duration=<_>.<_>`,
				`ts=2024-04-16T15:10:43.192290389Z caller=filetargetmanager.go:361 level=info component=logs logs_config=default msg="Adding target" key="/var/log/pods/*19a1cce8-5f04-46e0-a124-292b0dd9b343/testcoordinator/*.log:{batch_kubernetes_io_controller_uid=\"25ec5edf-f78e-468b-b6f3-3b9685f0cc8f\", batch_kubernetes_io_job_name=\"testcoordinator-job-2665838\", container=\"testcoordinator\", controller_uid=\"25ec5edf-f78e-468b-b6f3-3b9685f0cc8f\", job=\"k6-cloud/testcoordinator\", job_name=\"testcoordinator-job-2665838\", name=\"testcoordinator\", namespace=\"k6-cloud\", pod=\"testcoordinator-job-2665838-9g8ds\"}"`,
				`ts=2024-04-16T15:10:43.551782223Z caller=tailer.go:245 level=info component=logs logs_config=default component=tailer msg="stopped tailing file" path=/var/log/pods/grafana-com_marketplaces-api-f67ff7567-gqrvb_35649bfd-52ff-4281-9294-5f65fd5a89fc/marketplaces-api/0.log`,
				`ts=2024-04-16T15:10:43.<_> caller=filetargetmanager.go:<_> level=info component=logs logs_config=default msg="<_> target" key="/var/log/pods/*<_>/<_>/*.log:{<_>=\"<_>\", <_>=\"<_><_><_><_><_><_> <_><_><_><_><_>\", namespace=\"<_>\", pod=\"<_>\", <_>=\"<_>\"}"`,
				`ts=2024-04-16T15:10:43.<_> caller=tailer.go:<_> level=info component=logs logs_config=default component=tailer msg="<_> <_><_> <_> <_> <_><_> <_> <_><_> <_><_><_><_><_><_><_><_><_><_><_><_><_><_><_><_> <_><_><_>`,
				`ts=2024-04-16T15:10:<_>.<_> caller=filetarget.go:192 level=info component=logs logs_config=default msg="filetarget: watcher closed, tailer stopped, positions saved" path=/var/log/pods/*<_>/<_>/*.log`,
				`ts=2024-04-16T15:10:<_>.<_> caller=filetarget.go:313 level=info component=logs logs_config=default msg="watching new directory" directory=/var/log/pods/<_>/<_>`,
				`ts=2024-04-16T15:10:<_>.<_> caller=filetarget.go:313 level=info component=logs logs_config=default msg="watching new directory" directory=/var/log/pods/hosted-grafana_.<_>/<_>`,
				`ts=2024-04-16T15:10:<_>.<_> caller=filetarget.go:326 level=info component=logs logs_config=default msg="removing directory from watcher" directory=/var/log/pods/hosted-grafana_.<_>/<_>`,
				`ts=2024-04-16T15:10:<_>.<_> caller=filetargetmanager.go:181 level=info component=logs logs_config=default msg="received file watcher event" name=/var/log/pods/<_>/<_>/<_>.log op=CREATE`,
				`ts=2024-04-16T15:10:<_>.<_> caller=filetargetmanager.go:181 level=info component=logs logs_config=default msg="received file watcher event" name=/var/log/pods/<_><_><_>/<_><_><_>.<_> op=CREATE`,
				`ts=2024-04-16T15:10:<_>.<_> caller=filetargetmanager.go:181 level=info component=logs logs_config=default msg="received file watcher event" name=/var/log/pods/<_><_><_>/<_><_><_>.<_>.<_> op=CREATE`,
				`ts=2024-04-16T15:10:<_>.<_> caller=filetargetmanager.go:181 level=info component=logs logs_config=default msg="received file watcher event" name=/var/log/pods/hosted-grafana_.<_>/<_>/0.log.<_>.<_> op=CREATE`,
				`ts=2024-04-16T15:10:<_>.<_> caller=filetargetmanager.go:<_> level=info component=logs logs_config=default msg="<_> target" key="/var/log/pods/*<_>/<_>/*.log:{app=\"grafana\", conprof=\"true\", container=\"<_>\", instanceId=\"<_>\", job=\"hosted-grafana/grafana\", name=\"grafana\", namespace=\"hosted-grafana\", org=\"<_>\", plan=\"free\", pod=\"<_>\", pod_template_hash=\"<_>\", resource_version=\"<_>\", slug=\"<_>\", stackId=\"<_>\"}"`,
				`ts=2024-04-16T15:10:<_>.<_> caller=log.go:168 component=logs logs_config=default level=info msg="Re-opening moved/deleted file /var/log/pods/<_>/<_>/<_>.log ..."`,
				`ts=2024-04-16T15:10:<_>.<_> caller=log.go:168 component=logs logs_config=default level=info msg="Re-opening moved/deleted file /var/log/pods/hosted-grafana_.<_>/<_>/0.log ..."`,
				`ts=2024-04-16T15:10:<_>.<_> caller=log.go:168 component=logs logs_config=default level=info msg="Seeked /var/log/pods/<_>/<_>/0.log - &{Offset:0 Whence:0}"`,
				`ts=2024-04-16T15:10:<_>.<_> caller=log.go:168 component=logs logs_config=default level=info msg="Seeked /var/log/pods/hosted-grafana_.<_>/<_>/0.log - &{Offset:0 Whence:0}"`,
				`ts=2024-04-16T15:10:<_>.<_> caller=log.go:168 component=logs logs_config=default level=info msg="Successfully reopened /var/log/pods/<_>/<_>/<_>.log"`,
				`ts=2024-04-16T15:10:<_>.<_> caller=log.go:168 component=logs logs_config=default level=info msg="Successfully reopened /var/log/pods/hosted-grafana_.<_>/<_>/0.log"`,
				`ts=2024-04-16T15:10:<_>.<_> caller=log.go:168 component=logs logs_config=default level=info msg="Waiting for /var/log/pods/<_>/<_>/0.log to appear..."`,
				`ts=2024-04-16T15:10:<_>.<_> caller=log.go:168 component=logs logs_config=default level=info msg="Waiting for /var/log/pods/hosted-grafana_.<_>/<_>/0.log to appear..."`,
				`ts=2024-04-16T15:10:<_>.<_> caller=logfmt.go:139 level=error component=logs logs_config=default component=file_pipeline component=stage type=logfmt msg="failed to decode logfmt" err="bufio.Scanner: token too long"`,
				`ts=2024-04-16T15:10:<_>.<_> caller=logfmt.go:139 level=error component=logs logs_config=default component=file_pipeline component=stage type=logfmt msg="failed to decode logfmt" err="logfmt syntax error at pos <_> on line 1: unexpected '\"'"`,
				`ts=2024-04-16T15:10:<_>.<_> caller=tailer.go:245 level=info component=logs logs_config=default component=tailer msg="stopped tailing file" path=/var/log/pods/hosted-grafana_.<_>/<_>/0.log`,
				`ts=2024-04-16T15:10:<_>.<_> caller=tailer.go:<_> level=info component=logs logs_config=default component=tailer msg="<_> <_>: <_>" path=/var/log/pods/<_>/<_>/0.log`,
				`ts=2024-04-16T15:10:<_>.<_> caller=tailer.go:<_> level=info component=logs logs_config=default component=tailer msg="<_> <_>: <_>" path=/var/log/pods/hosted-grafana_.<_>/<_>/0.log`,
				`ts=2024-04-16T15:10:<_>.<_> caller=tailer.go:<_> level=info component=logs logs_config=default component=tailer msg="<_> <_><_> <_> <_> <_><_> <_> <_><_> <_><_><_><_><_><_><_><_><_><_><_><_><_><_><_><_><_><_> <_><_><_>`,
			},
		},
		{
			drain:     New(DefaultConfig(), nil),
			inputFile: `testdata/ingester-logfmt.txt`,
			patterns: []string{
				`ts=2024-04-17T09:52:46.363974185Z caller=http.go:194 level=debug traceID=1b48f5156a61ca69 msg="GET /debug/pprof/delta_mutex (200) 1.161082ms"`,
				`ts=2024-04-17T09:52:46.<_> caller=head.go:216 level=debug tenant=987678 msg="profile is empty after delta computation" metricName=memory`,
				`ts=2024-04-17T09:52:46.<_> caller=http.go:194 level=debug traceID=<_> orgID=<_> msg="POST /ingester.v1.IngesterService/Push (200) <_>.<_>"`,
			},
		},
		{
			drain:     New(DefaultConfig(), nil),
			inputFile: `testdata/drone-json.txt`,
			patterns: []string{
				`{"duration":<_>,"level":"debug","method":"GET","msg":"request completed","referer":"","remote":"10.136.105.40:52702","request":"/metrics","status":200,"time":"<_>:<_>:<_>","user-agent":"GrafanaAgent/v0.40.3 (flow; linux; helm)"}`,
				`{"id":"<_>","level":"debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":"<_>:<_>:<_>"}`,
				`{"id":"<_>","level":"debug","msg":"calculate server capacity","time":"<_>:<_>:<_>"}`,
				`{"id":"<_>","level":"debug","msg":"calculate unfinished jobs","time":"<_>:<_>:<_>"}`,
				`{"id":"<_>","level":"debug","msg":"check capacity complete","time":"<_>:<_>:<_>"}`,
				`{"id":"<_>","level":"debug","msg":"no capacity changes required","time":"<_>:<_>:<_>"}`,
			},
		},
		{
			drain:     New(DefaultConfig(), nil),
			inputFile: "testdata/distributor-logfmt.txt",
			patterns: []string{
				`ts=2024-05-02T12:17:22.851228301Z caller=http.go:194 level=debug traceID=1e1fe5ba1756bc38 orgID=1819 msg="POST /pyroscope/ingest?aggregationType=sum&from=1714652230&name=flamegraph.com%7Bapp_kubernetes_io_instance%3Dflamegraph-com%2Capp_kubernetes_io_name%3Dflamegraph-com%2Ccluster%3Dflamegraph.com%2Cinstance%3D10.0.11.146%3A8001%2Cjob%3Dkubernetes-pods%2Cnamespace%3Dflamegraph-com%2Cpod%3Dflamegraph-com-backend-79c858c7bf-jw2hn%2Cpod_template_hash%3D79c858c7bf%2Cpyroscope_tenant%3Dpyroscope%2Ctier%3Dbackend%7D&sampleRate=0&spyName=scrape&units=samples&until=1714652240 (200) 22.345191ms"`,
				`ts=2024-05-02T12:17:22.<_> caller=http.go:194 level=debug traceID=<_> orgID=75 msg="POST /ingest?aggregationType=&from=1714652227232613927&name=checkoutservice%7B__session_id__%3D294b9729f5a7de95%2Cnamespace%3Dotel-demo%7D&sampleRate=<_>&spyName=gospy&units=&until=1714652242232506798 (200) <_>.<_>"`,
				`ts=2024-05-02T12:17:22.<_> caller=http.go:194 level=debug traceID=<_> orgID=75 msg="POST /ingest?aggregationType=<_>&from=<_>&name=checkoutservice%7B__session_id__%3D294b9729f5a7de95%2Cnamespace%3Dotel-demo%7D&sampleRate=<_>&spyName=gospy&units=<_>&until=<_> (200) <_>.<_>"`,
				`ts=2024-05-02T12:17:<_>.<_> caller=http.go:194 level=debug traceID=<_> orgID=1819 msg="POST /pyroscope/ingest?aggregationType=sum&from=1714652230&name=flamegraph.com.frontend%7Bapp_kubernetes_io_instance%3Dflamegraph-com%2Capp_kubernetes_io_name%3Dflamegraph-com%2Ccluster%3Dflamegraph.com%2Cinstance%3D10.0.9.115%3A9091%2Cjob%3Dkubernetes-pods%2Cnamespace%3Dflamegraph-com%2Cpod%3Dflamegraph-com-frontend-6fb87f8785-pd87k%2Cpod_template_hash%3D6fb87f8785%2Cpyroscope_tenant%3Dpyroscope%2Ctier%3Dfrontend%7D&sampleRate=0&spyName=scrape&units=samples&until=1714652240 (200) <_>.<_>"`,
				`ts=2024-05-02T12:17:<_>.<_> caller=http.go:194 level=debug traceID=<_> orgID=<_> msg="POST /push.v1.PusherService/Push (<_>) <_>.<_>"`,
			},
		},
		{
			drain:     New(DefaultConfig(), nil),
			inputFile: "testdata/journald.txt",
			patterns: []string{
				`						ln --force -s /proc/$(pidof hgrun-pause)/root/bin/hgrun /bin/hgrun;`,
				`						while [ "$(pidof plugins-pause)" = "" ]; do sleep 0.5; done;`,
				`	ts=2024-05-07T11:59:32.025687537Z level=error caller=http_client.go:56 app=hgrun hgrun_version=0.1.453-59-gf3f63162a msg="request`,
				`	ts=2024-05-07T11:59:<_>.<_> level=error caller=http_client.go:56 app=hgrun hgrun_version=0.1.<_> msg="request failed" error="Get \"http://127.0.0.1:3000/api/health\": dial tcp 127.0.0.1:3000: connect: connection refused" method=GET url=http://127.0.0.1:3000/api/health`,
				`2024-05-07T11:59:43.484606Z INFO ExtHandler ExtHandler Downloading agent manifest`,
				`2024-05-07T11:59:<_>.<_> INFO TelemetryEventsCollector ExtHandler Collected 2 events for extension: Microsoft.Azure.Extensions.CustomScript`,
				`<_>.scope: Consumed <_>.<_> CPU time.`,
				`<_>.scope: Deactivated successfully.`,
				`AVC apparmor="DENIED" operation="ptrace" profile="cri-containerd.apparmor.d" pid=<_> comm="pidof" requested_mask="read" denied_mask="read" peer="unconfined"`,
				`E0507 11:59:31.928148    4734 pod_workers.go:1300] "Error syncing pod, skipping" err="unmounted volumes=[terraform-drift-detector-data], unattached volumes=[terraform-drift-detector-data], failed to process volumes=[]: context deadline exceeded" pod="terraform-drift-detector/terraform-drift-detector-d68b4c545-jg2vj" podUID="6c607496-ef26-454e-b2f2-4cb75b233fa3"`,
				`E0507 11:59:34.923938    3027 kuberuntime_manager.go:1261] container &Container{Name:mysqld-exporter,Image:prom/mysqld-exporter:v0.13.0,Command:[],Args:[--collect.info_schema.innodb_metrics],WorkingDir:,Ports:[]ContainerPort{ContainerPort{Name:http-metrics,HostPort:0,ContainerPort:9104,Protocol:TCP,HostIP:,},},Env:[]EnvVar{EnvVar{Name:MYSQL_USER,Value:,ValueFrom:&EnvVarSource{FieldRef:nil,ResourceFieldRef:nil,ConfigMapKeyRef:nil,SecretKeyRef:&SecretKeySelector{LocalObjectReference:LocalObjectReference{Name:testcrossplane-user-exporter,},Key:username,Optional:nil,},},},EnvVar{Name:MYSQL_PASSWORD,Value:,ValueFrom:&EnvVarSource{FieldRef:nil,ResourceFieldRef:nil,ConfigMapKeyRef:nil,SecretKeyRef:&SecretKeySelector{LocalObjectReference:LocalObjectReference{Name:testcrossplane-user-exporter,},Key:password,Optional:nil,},},},EnvVar{Name:MYSQL_HOST,Value:,ValueFrom:&EnvVarSource{FieldRef:nil,ResourceFieldRef:nil,ConfigMapKeyRef:nil,SecretKeyRef:&SecretKeySelector{LocalObjectReference:LocalObjectReference{Name:testcrossplane-user-exporter,},Key:endpoint,Optional:nil,},},},EnvVar{Name:MYSQL_PORT,Value:,ValueFrom:&EnvVarSource{FieldRef:nil,ResourceFieldRef:nil,ConfigMapKeyRef:nil,SecretKeyRef:&SecretKeySelector{LocalObjectReference:LocalObjectReference{Name:testcrossplane-user-exporter,},Key:port,Optional:nil,},},},EnvVar{Name:MYSQL_TLS_MODE,Value:preferred,ValueFrom:nil,},EnvVar{Name:DATA_SOURCE_NAME,Value:$(MYSQL_USER):$(MYSQL_PASSWORD)@tcp($(MYSQL_HOST):$(MYSQL_PORT))/?tls=$(MYSQL_TLS_MODE),ValueFrom:nil,},},Resources:ResourceRequirements{Limits:ResourceList{},Requests:ResourceList{},Claims:[]ResourceClaim{},},VolumeMounts:[]VolumeMount{VolumeMount{Name:kube-api-access-dzx7d,ReadOnly:true,MountPath:/var/run/secrets/kubernetes.io/serviceaccount,SubPath:,MountPropagation:nil,SubPathExpr:,},},LivenessProbe:nil,ReadinessProbe:nil,Lifecycle:nil,TerminationMessagePath:/dev/termination-log,ImagePullPolicy:IfNotPresent,SecurityContext:nil,Stdin:false,StdinOnce:false,TTY:false,EnvFrom:[]EnvFromSource{},TerminationMessagePolicy:File,VolumeDevices:[]VolumeDevice{},StartupProbe:nil,ResizePolicy:[]ContainerResizePolicy{},RestartPolicy:nil,} start failed in pod testcrossplane-exporter-c67cfc58f-vbzl4_crossplane-playground(3d49134d-3378-4ec3-824c-5ff4ea2590a5): CreateContainerConfigError: secret "testcrossplane-user-exporter" not found`,
				`E0507 11:59:35.928465    4734 pod_workers.go:1300] "Error syncing pod, skipping" err="unmounted volumes=[custom-grafana-agent], unattached volumes=[], failed to process volumes=[]: context deadline exceeded" pod="loki-dev-010/custom-grafana-agent-856948968f-6jfks" podUID="17b244cc-ecb9-4fbc-beaa-8fa47fafe013"`,
				`E0507 11:59:<_>.<_>    <_> kuberuntime_manager.go:1256] container &Container{Name:grafana,Image:us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>.1.<_>,Command:[/bin/sh],Args:[-c set -e; while [ "$(pidof hgrun-pause)" = "" ]; do sleep 0.5; done;`,
				`E0507 11:59:<_>.<_>    <_> pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"<_>\" with CrashLoopBackOff: \"back-off <_> restarting failed container=<_> pod=<_>(<_>)\"" pod="<_>/<_>" podUID="<_>"`,
				`E0507 11:59:<_>.<_>    <_> pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"<_>\" with CreateContainerConfigError: \"secret \\\"<_>\\\" not found\"" pod="<_>/<_>" podUID="<_>"`,
				`E0507 11:59:<_>.<_>    <_> pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"<_>\" with ImagePullBackOff: \"Back-off pulling image \\\"us.gcr.io/hosted-grafana/<_>:<_>.<_>.<_>\\\"\"" pod="<_>/<_>" podUID="<_>"`,
				`E0507 11:59:<_>.<_>    <_> pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"<_>\" with ImagePullBackOff: \"Back-off pulling image \\\"us.gcr.io/kubernetes-dev/<_>:<_>\\\"\"" pod="<_>/<_>" podUID="<_>"`,
				`E0507 11:59:<_>.<_>    <_> pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"grafana\" with ErrImagePull: \"[rpc error: code = NotFound desc = failed to pull and unpack image \\\"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>.1.<_>\\\": failed to resolve reference \\\"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>.1.<_>\\\": us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>.1.<_>: not found, failed to pull and unpack image \\\"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>.1.<_>\\\": failed to resolve reference \\\"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>.1.<_>\\\": unexpected status from HEAD request to https://us.gcr.io/v2/hosted-grafana/hosted-grafana-pro/manifests/<_>.1.<_>: 403 Forbidden]\"" pod="hosted-grafana/<_>" podUID="<_>"`,
				`E0507 11:59:<_>.<_>    <_> pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"pdc\" with ErrImageNeverPull: \"Container image \\\"us.gcr.io/hosted-grafana/pdc:0.1.415\\\" is not present with pull policy of Never\"" pod="pdc/<_>" podUID="<_>"`,
				`E0507 11:59:<_>.<_>    <_> prober.go:104] "Probe errored" err="rpc error: code = NotFound desc = failed to exec in container: failed to load task: no running task found: task <_> not found: not found" probeType="Readiness" pod="hosted-grafana/<_>" podUID="<_>" containerName="grafana"`,
				`E0507 11:59:<_>.<_>    <_> prober.go:239] "Unable to write all bytes from execInContainer" err="short write" expectedBytes=<_> actualBytes=10240`,
				`E0507 11:59:<_>.<_>    <_> remote_image.go:180] "PullImage from image service failed" err="rpc error: code = NotFound desc = failed to pull and unpack image \"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>.1.<_>\": failed to resolve reference \"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>.1.<_>\": us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>.1.<_>: not found" image="us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>.1.<_>"`,
				`E0507 11:59:<_>.<_>    <_> remote_image.go:180] "PullImage from image service failed" err="rpc error: code = Unknown desc = failed to pull and unpack image \"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>.1.<_>\": failed to resolve reference \"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>.1.<_>\": unexpected status from HEAD request to https://us.gcr.io/v2/hosted-grafana/hosted-grafana-pro/manifests/<_>.1.<_>: 403 Forbidden" image="us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>.1.<_>"`,
				`E0507 11:59:<_>.<_>    <_> remote_runtime.go:432] "ContainerStatus from runtime service failed" err="rpc error: code = NotFound desc = an error occurred when try to find container \"<_>\": not found" containerID="<_>"`,
				`E0507 11:59:<_>.<_>    <_> remote_runtime.go:496] "ExecSync cmd from runtime service failed" err="rpc error: code = NotFound desc = failed to exec in container: failed to load task: no running task found: task <_> not found: not found" containerID="<_>" cmd=["/bin/hgrun","check"]`,
				`I0507 11:59:31.815514    2791 azure_credentials.go:220] image(us.gcr.io/hosted-grafana/hosted-grafana-pro) is not from ACR, return empty authentication`,
				`I0507 11:59:34.518822    3224 kuberuntime_container.go:745] "Killing container with a grace period" pod="hosted-grafana/hosted-grafana-api-7b6bd9b949-9csb4" podUID="25cb986c-3d6c-4ed0-abf3-ee59ed6175f9" containerName="hgapi" containerID="containerd://c91436db00920ec961b9d5d6b4859d80a912e862e34fb5c45d8a85684fe6a97e" gracePeriod=30`,
				`I0507 11:59:34.834734    3224 reconciler_common.go:172] "operationExecutor.UnmountVolume started for volume \"kube-api-access-95j2t\" (UniqueName: \"kubernetes.io/projected/25cb986c-3d6c-4ed0-abf3-ee59ed6175f9-kube-api-access-95j2t\") pod \"25cb986c-3d6c-4ed0-abf3-ee59ed6175f9\" (UID: \"25cb986c-3d6c-4ed0-abf3-ee59ed6175f9\") "`,
				`I0507 11:59:34.834794    3224 reconciler_common.go:172] "operationExecutor.UnmountVolume started for volume \"pdc-certs\" (UniqueName: \"kubernetes.io/secret/25cb986c-3d6c-4ed0-abf3-ee59ed6175f9-pdc-certs\") pod \"25cb986c-3d6c-4ed0-abf3-ee59ed6175f9\" (UID: \"25cb986c-3d6c-4ed0-abf3-ee59ed6175f9\") "`,
				`I0507 11:59:34.834835    3224 reconciler_common.go:172] "operationExecutor.UnmountVolume started for volume \"gcs-serviceaccount\" (UniqueName: \"kubernetes.io/secret/25cb986c-3d6c-4ed0-abf3-ee59ed6175f9-gcs-serviceaccount\") pod \"25cb986c-3d6c-4ed0-abf3-ee59ed6175f9\" (UID: \"25cb986c-3d6c-4ed0-abf3-ee59ed6175f9\") "`,
				`I0507 11:59:34.841404    3224 operation_generator.go:888] UnmountVolume.TearDown succeeded for volume "kubernetes.io/projected/25cb986c-3d6c-4ed0-abf3-ee59ed6175f9-kube-api-access-95j2t" (OuterVolumeSpecName: "kube-api-access-95j2t") pod "25cb986c-3d6c-4ed0-abf3-ee59ed6175f9" (UID: "25cb986c-3d6c-4ed0-abf3-ee59ed6175f9"). InnerVolumeSpecName "kube-api-access-95j2t". PluginName "kubernetes.io/projected", VolumeGidValue ""`,
				`I0507 11:59:34.936025    3224 reconciler_common.go:300] "Volume detached for volume \"pdc-certs\" (UniqueName: \"kubernetes.io/secret/25cb986c-3d6c-4ed0-abf3-ee59ed6175f9-pdc-certs\") on node \"ip-10-60-2-58.us-east-2.compute.internal\" DevicePath \"\""`,
				`I0507 11:59:34.<_>    3224 operation_generator.go:888] UnmountVolume.TearDown succeeded for volume "kubernetes.io/secret/<_>" (OuterVolumeSpecName: "<_>") pod "25cb986c-3d6c-4ed0-abf3-ee59ed6175f9" (UID: "25cb986c-3d6c-4ed0-abf3-ee59ed6175f9"). InnerVolumeSpecName "<_>". PluginName "kubernetes.io/secret", VolumeGidValue ""`,
				`I0507 11:59:34.<_>    3224 reconciler_common.go:300] "Volume detached for volume \"<_>\" (UniqueName: \"kubernetes.io/<_>/<_>\") on node \"ip-10-60-2-58.us-east-2.compute.internal\" DevicePath \"\""`,
				`I0507 11:59:37.<_>    <_> prober.go:107] "Probe failed" probeType="Readiness" pod="<_>/<_>" podUID="<_>" containerName="<_>" probeResult="failure" output="HTTP probe failed with statuscode: <_>"`,
				`I0507 11:59:38.116658    2791 azure_credentials.go:220] image(us.gcr.io/hosted-grafana/hg-plugins) is not from ACR, return empty authentication`,
				`I0507 11:59:39.168633    2776 kubelet.go:2493] "SyncLoop (probe)" probe="readiness" status="" pod="hosted-grafana/dafdeveuwest2-grafana-7845d969b5-f8h5q"`,
				`I0507 11:59:<_>.<_>    2791 azure_credentials.go:220] image(us.gcr.io/hosted-grafana/hgrun) is not from ACR, return empty authentication`,
				`I0507 11:59:<_>.<_>    6247 prober.go:107] "Probe failed" probeType="Readiness" pod="grafana-agent/grafana-agent-helm-4" podUID="c36c5200-1cd6-4093-893c-c022f91af996" containerName="grafana-agent" probeResult="failure" output="Get \"http://10.0.99.125:3090/-/ready\": dial tcp 10.0.99.125:3090: connect: connection refused"`,
				`I0507 11:59:<_>.<_>    <_> generic.go:334] "Generic (PLEG): container finished" podID="<_>" containerID="<_>" exitCode=1`,
				`I0507 11:59:<_>.<_>    <_> kubelet.go:2498] "SyncLoop (probe)" probe="liveness" status="unhealthy" pod="hosted-grafana/<_>"`,
				`I0507 11:59:<_>.<_>    <_> kubelet.go:2498] "SyncLoop (probe)" probe="readiness" status="ready" pod="hosted-grafana/<_>"`,
				`I0507 11:59:<_>.<_>    <_> kubelet.go:<_>] "SyncLoop (PLEG): event for pod" pod="<_>/<_>" event={"ID":"<_>","Type":"<_>","Data":"<_>"}`,
				`I0507 11:59:<_>.<_>    <_> kubelet.go:<_>] "SyncLoop DELETE" source="api" pods=["hosted-grafana/<_>"]`,
				`I0507 11:59:<_>.<_>    <_> kubelet.go:<_>] "SyncLoop REMOVE" source="api" pods=["hosted-grafana/<_>"]`,
				`I0507 11:59:<_>.<_>    <_> kubelet_getters.go:187] "Pod status updated" pod="kube-system/<_>" status="Running"`,
				`I0507 11:59:<_>.<_>    <_> kubelet_pods.go:906] "Unable to retrieve pull secret, the image pull may not succeed." pod="<_>/<_>" secret="" err="secret \"<_>\" not found"`,
				`I0507 11:59:<_>.<_>    <_> kubelet_volumes.go:<_>] "Cleaned up orphaned pod volumes dir" podUID="<_>" path="/var/lib/kubelet/pods/<_>/volumes"`,
				`I0507 11:59:<_>.<_>    <_> pod_container_deletor.go:53] "DeleteContainer returned error" containerID={"Type":"containerd","ID":"<_>"} err="failed to get container status \"<_>\": rpc error: code = NotFound desc = an error occurred when try to find container \"<_>\": not found"`,
				`I0507 11:59:<_>.<_>    <_> prober.go:107] "Probe failed" probeType="Readiness" pod="hosted-grafana/<_>" podUID="<_>" containerName="grafana" probeResult="failure" output=<`,
				`I0507 11:59:<_>.<_>    <_> scope.go:117] "RemoveContainer" containerID="<_>"`,
				`I0507 11:59:<_>.<_>  <_> cache.go:40] re-using cached key and certificate`,
				`IPv4: martian source 10.132.<_>.<_> from 10.132.<_>.<_>, on dev eth0`,
				`PRC: Renewing lease on eth0.`,
				`RCV: Reply message on eth0 from fe80::e9:7eff:fedf:3d37.`,
				`Removed slice libcontainer container kubepods-burstable-pod25cb986c_3d6c_4ed0_abf3_ee59ed6175f9.slice.`,
				`Started cri-containerd-95bf586cd79d43120ff44582d4dbd2476de61744411f8515b9b2c527a41fd5d9.scope.`,
				`Started libcontainer container <_>.`,
				`XMT: Renew on eth0, interval 9700ms.`,
				`XMT: Solicit on eth0, interval <_>.`,
				`audit: type=1400 audit(<_>.<_>:<_>): apparmor="DENIED" operation="ptrace" profile="cri-containerd.apparmor.d" pid=<_> comm="pidof" requested_mask="read" denied_mask="read" peer="unconfined"`,
				`kauditd_printk_skb: <_> callbacks suppressed`,
				`ll header: 00000000: 42 01 0a 80 00 <_> 42 01 0a 80 00 01 08 00`,
				`net_ratelimit: 2 callbacks suppressed`,
				`run-containerd-io.containerd.runtime.v2.task-k8s.<_>.mount: Deactivated successfully.`,
				`run-containerd-runc-k8s.io-e5f17d69eee483ec8d43b26d5d628246984ba92f794ee5f3748935f5b6448b9b-runc.6eAyHn.mount: Deactivated successfully.`,
				`time="2024-05-07T11:59:34.519591759Z" level=info msg="StopContainer for \"c91436db00920ec961b9d5d6b4859d80a912e862e34fb5c45d8a85684fe6a97e\" with timeout 30 (s)"`,
				`time="2024-05-07T11:59:34.520032214Z" level=info msg="Stop container \"c91436db00920ec961b9d5d6b4859d80a912e862e34fb5c45d8a85684fe6a97e\" with signal terminated"`,
				`time="2024-05-07T11:59:34.591282703Z" level=info msg="StopContainer for \"c91436db00920ec961b9d5d6b4859d80a912e862e34fb5c45d8a85684fe6a97e\" returns successfully"`,
				`time="2024-05-07T11:59:34.592005066Z" level=info msg="StopPodSandbox for \"c605ad2cdc74c6b5288f2532ad71cce81a28ef6965f97a89ff6609deb825553a\""`,
				`time="2024-05-07T11:59:34.592084495Z" level=info msg="Container to stop \"c91436db00920ec961b9d5d6b4859d80a912e862e34fb5c45d8a85684fe6a97e\" must be in running or unknown state, current state \"CONTAINER_EXITED\""`,
				`time="2024-05-07T11:59:34.706960850Z" level=info msg="TearDown network for sandbox \"c605ad2cdc74c6b5288f2532ad71cce81a28ef6965f97a89ff6609deb825553a\" successfully"`,
				`time="2024-05-07T11:59:34.707025668Z" level=info msg="StopPodSandbox for \"c605ad2cdc74c6b5288f2532ad71cce81a28ef6965f97a89ff6609deb825553a\" returns successfully"`,
				`time="2024-05-07T11:59:38.117772842Z" level=info msg="PullImage \"us.gcr.io/hosted-grafana/hg-plugins:2024-05-07-v545244-f51851984\""`,
				`time="2024-05-07T11:59:38.484586527Z" level=error msg="Failed to delete exec process \"d9e0a1867ce73695ad859f2b0a76fe8f5053db8a5e49142d747e53a445729bd4\" for container \"6ad3e55547f2192f865518e75009243418b177091c1c781236e2ac8f0324b408\"" error="ttrpc: closed: unknown"`,
				`time="2024-05-07T11:59:<_>.<_>" level=error msg="ContainerStatus for \"<_>\" failed" error="rpc error: code = NotFound desc = an error occurred when try to find container \"<_>\": not found"`,
				`time="2024-05-07T11:59:<_>.<_>" level=error msg="ExecSync for \"<_>\" failed" error="rpc error: code = NotFound desc = failed to exec in container: failed to load task: no running task found: task <_> not found: not found"`,
				`time="2024-05-07T11:59:<_>.<_>" level=error msg="PullImage \"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>.1.<_>\" failed" error="failed to pull and unpack image \"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>.1.<_>\": failed to resolve reference \"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>.1.<_>\": unexpected status from HEAD request to https://us.gcr.io/v2/hosted-grafana/hosted-grafana-pro/manifests/<_>.1.<_>: 403 Forbidden"`,
				`time="2024-05-07T11:59:<_>.<_>" level=error msg="PullImage \"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>.1.<_>\" failed" error="rpc error: code = NotFound desc = failed to pull and unpack image \"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>.1.<_>\": failed to resolve reference \"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>.1.<_>\": us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>.1.<_>: not found"`,
				`time="2024-05-07T11:59:<_>.<_>" level=info msg="CreateContainer within sandbox \"<_>\" for &ContainerMetadata{Name:<_>,Attempt:<_>,} returns container id \"<_>\""`,
				`time="2024-05-07T11:59:<_>.<_>" level=info msg="CreateContainer within sandbox \"<_>\" for container &ContainerMetadata{Name:<_>,Attempt:<_>,}"`,
				`time="2024-05-07T11:59:<_>.<_>" level=info msg="ImageCreate event name:\"sha256:<_>\" labels:{key:\"io.cri-containerd.image\" value:\"managed\"}"`,
				`time="2024-05-07T11:59:<_>.<_>" level=info msg="ImageCreate event name:\"us.gcr.io/hosted-grafana/<_>:<_>.1.<_>\" labels:{key:\"io.cri-containerd.image\" value:\"managed\"}"`,
				`time="2024-05-07T11:59:<_>.<_>" level=info msg="ImageCreate event name:\"us.gcr.io/hosted-grafana/<_>@sha256:<_>\" labels:{key:\"io.cri-containerd.image\" value:\"managed\"}"`,
				`time="2024-05-07T11:59:<_>.<_>" level=info msg="ImageUpdate event name:\"sha256:<_>\" labels:{key:\"io.cri-containerd.image\" value:\"managed\"}"`,
				`time="2024-05-07T11:59:<_>.<_>" level=info msg="ImageUpdate event name:\"us.gcr.io/hosted-grafana/<_>:<_>.1.<_>\" labels:{key:\"io.cri-containerd.image\" value:\"managed\"}"`,
				`time="2024-05-07T11:59:<_>.<_>" level=info msg="ImageUpdate event name:\"us.gcr.io/hosted-grafana/<_>@sha256:<_>\" labels:{key:\"io.cri-containerd.image\" value:\"managed\"}"`,
				`time="2024-05-07T11:59:<_>.<_>" level=info msg="PullImage \"us.gcr.io/hosted-grafana/<_>:<_>.1.<_>\" returns image reference \"sha256:<_>\""`,
				`time="2024-05-07T11:59:<_>.<_>" level=info msg="PullImage \"us.gcr.io/hosted-grafana/<_>:<_>.1.<_>\""`,
				`time="2024-05-07T11:59:<_>.<_>" level=info msg="Pulled image \"us.gcr.io/hosted-grafana/<_>:<_>.1.<_>\" with image id \"sha256:<_>\", repo tag \"us.gcr.io/hosted-grafana/<_>:<_>.1.<_>\", repo digest \"us.gcr.io/hosted-grafana/<_>@sha256:<_>\", size \"<_>\" in <_>.<_>"`,
				`time="2024-05-07T11:59:<_>.<_>" level=info msg="RemoveContainer for \"<_>\" returns successfully"`,
				`time="2024-05-07T11:59:<_>.<_>" level=info msg="RemoveContainer for \"<_>\""`,
				`time="2024-05-07T11:59:<_>.<_>" level=info msg="StartContainer for \"<_>\" returns successfully"`,
				`time="2024-05-07T11:59:<_>.<_>" level=info msg="StartContainer for \"<_>\""`,
				`time="2024-05-07T11:59:<_>.<_>" level=info msg="cleaning up dead shim" namespace=k8s.io`,
				`time="2024-05-07T11:59:<_>.<_>" level=info msg="shim disconnected" id=<_> namespace=k8s.io`,
				`time="2024-05-07T11:59:<_>.<_>" level=info msg="stop pulling image us.gcr.io/hosted-grafana/<_>:<_>.1.<_>: active requests=0, bytes read=<_>"`,
				`time="2024-05-07T11:59:<_>.<_>" level=info msg="trying next host - response was http.StatusNotFound" host=us.gcr.io`,
				`time="2024-05-07T11:59:<_>.<_>" level=warning msg="cleaning up after shim disconnected" id=<_> namespace=k8s.io`,
				`var-lib-containerd-tmpmounts-containerd\<_>.mount: Deactivated successfully.`,
			},
		},
		{
			drain:     New(DefaultConfig(), nil),
			inputFile: "testdata/kafka.txt",
			patterns: []string{
				`[2024-05-07 10:55:40,626] INFO [LocalLog partition=ingest-6, dir=/bitnami/kafka/data] Deleting segment files LogSegment(baseOffset=180391157, size=16991045, lastModifiedTime=1715075754780, largestRecordTimestamp=Some(1715075754774)),LogSegment(baseOffset=180393429, size=16997692, lastModifiedTime=1715075760206, largestRecordTimestamp=Some(1715075760186)),LogSegment(baseOffset=180395889, size=16998200, lastModifiedTime=1715075765542, largestRecordTimestamp=Some(1715075765526)),LogSegment(baseOffset=180398373, size=16977347, lastModifiedTime=1715075770515, largestRecordTimestamp=Some(1715075770504)) (kafka.log.LocalLog$)`,
				`[2024-05-07 10:55:53,038] INFO [LocalLog partition=mimir-dev-09-aggregations-offsets-1, dir=/bitnami/kafka/data] Deleting segment files LogSegment(baseOffset=447957, size=948, lastModifiedTime=1715059232052, largestRecordTimestamp=Some(1715059232002)),LogSegment(baseOffset=447969, size=948, lastModifiedTime=1715059424352, largestRecordTimestamp=Some(1715059424301)) (kafka.log.LocalLog$)`,
				`[2024-05-07 10:55:53,<_>] INFO [LocalLog partition=mimir-dev-09-aggregations-offsets-0, dir=/bitnami/kafka/data] Deleting segment files LogSegment(baseOffset=<_>, size=948, lastModifiedTime=<_>, largestRecordTimestamp=Some(<_>)) (kafka.log.LocalLog$)`,
				`[2024-05-07 10:55:<_>,<_>] INFO Deleted log /bitnami/kafka/data/<_>/<_>.log.deleted. (kafka.log.LogSegment)`,
				`[2024-05-07 10:55:<_>,<_>] INFO Deleted offset index /bitnami/kafka/data/<_>/<_>.index.deleted. (kafka.log.LogSegment)`,
				`[2024-05-07 10:55:<_>,<_>] INFO Deleted producer state snapshot /bitnami/kafka/data/<_>/<_>.snapshot.deleted (kafka.log.SnapshotFile)`,
				`[2024-05-07 10:55:<_>,<_>] INFO Deleted time index /bitnami/kafka/data/<_>/<_>.timeindex.deleted. (kafka.log.LogSegment)`,
				`[2024-05-07 10:55:<_>,<_>] INFO [LocalLog partition=<_>, dir=/bitnami/kafka/data] Rolled new log segment at offset <_> in <_> ms. (kafka.log.LocalLog)`,
				`[2024-05-07 10:55:<_>,<_>] INFO [ProducerStateManager partition=<_>] Wrote producer snapshot at offset <_> with 0 producer ids in <_> ms. (kafka.log.ProducerStateManager)`,
				`[2024-05-07 10:55:<_>,<_>] INFO [UnifiedLog partition=<_>, dir=/bitnami/kafka/data] Deleting segment LogSegment(baseOffset=<_>, size=<_>, lastModifiedTime=<_>, largestRecordTimestamp=Some(<_>)) due to retention size <_> breach. Log size after deletion will be <_>. (kafka.log.UnifiedLog)`,
				`[2024-05-07 10:55:<_>,<_>] INFO [UnifiedLog partition=<_>, dir=/bitnami/kafka/data] Deleting segments due to log start offset <_> breach: LogSegment(baseOffset=<_>, size=948, lastModifiedTime=<_>, largestRecordTimestamp=Some(<_>)),LogSegment(baseOffset=<_>, size=948, lastModifiedTime=<_>, largestRecordTimestamp=Some(<_>)) (kafka.log.UnifiedLog)`,
				`[2024-05-07 10:55:<_>,<_>] INFO [UnifiedLog partition=<_>, dir=/bitnami/kafka/data] Deleting segments due to log start offset <_> breach: LogSegment(baseOffset=<_>, size=<_>, lastModifiedTime=<_>, largestRecordTimestamp=Some(<_>)) (kafka.log.UnifiedLog)`,
				`[2024-05-07 10:55:<_>,<_>] INFO [UnifiedLog partition=<_>, dir=/bitnami/kafka/data] Incremented log start offset to <_> due to leader offset increment (kafka.log.UnifiedLog)`,
				`[2024-05-07 10:55:<_>,<_>] INFO [UnifiedLog partition=<_>, dir=/bitnami/kafka/data] Incremented log start offset to <_> due to segment deletion (kafka.log.UnifiedLog)`,
			},
		},
		{
			drain:     New(DefaultConfig(), nil),
			inputFile: "testdata/kubernetes.txt",
			patterns: []string{
				`I0507 12:02:27.947830       1 nodeutilization.go:274] "Evicting pods based on priority, if they have same priority, they'll be evicted based on QoS tiers"`,
				`I0507 12:02:27.<_>       1 defaultevictor.go:163] "pod does not fit on any other node because of nodeSelector(s), Taint(s), or nodes marked as unschedulable" pod="<_>/<_>"`,
				`I0507 12:02:27.<_>       1 defaultevictor.go:202] "Pod fails the following checks" pod="<_>/<_>" checks="pod has local storage and descheduler is not configured with evictLocalStoragePods"`,
				`I0507 12:02:27.<_>       1 defaultevictor.go:202] "Pod fails the following checks" pod="ge-logs/<_>" checks="[pod is a DaemonSet pod, pod has local storage and descheduler is not configured with evictLocalStoragePods]"`,
				`I0507 12:02:27.<_>       1 defaultevictor.go:202] "Pod fails the following checks" pod="insight-logs/<_>" checks="[pod is a DaemonSet pod, pod has higher priority than specified priority class threshold, pod has local storage and descheduler is not configured with evictLocalStoragePods]"`,
				`I0507 12:02:27.<_>       1 defaultevictor.go:202] "Pod fails the following checks" pod="loki-dev-ssd/<_>" checks="[pod is a DaemonSet pod, pod has higher priority than specified priority class threshold, pod has local storage and descheduler is not configured with evictLocalStoragePods]"`,
				`I0507 12:02:27.<_>       1 defaultevictor.go:202] "Pod fails the following checks" pod="promtail-ops/<_>" checks="[pod is a DaemonSet pod, pod has local storage and descheduler is not configured with evictLocalStoragePods]"`,
				`I0507 12:02:27.<_>       1 defaultevictor.go:202] "Pod fails the following checks" pod="pyroscope-ebpf/<_>" checks="pod is a DaemonSet pod"`,
				`I0507 12:02:27.<_>       1 node.go:157] "Pod does not fit on any other node" pod:="<_>/<_>" node:="<_>" error:="[pod node selector does not match the node label, <_> <_><_> <_> <_><_> <_> <_>]"`,
				`I0507 12:02:27.<_>       1 node.go:157] "Pod does not fit on any other node" pod:="<_>/<_>" node:="<_>" error:="[pod node selector does not match the node label, insufficient <_>, insufficient <_>]"`,
				`I0507 12:02:27.<_>       1 node.go:157] "Pod does not fit on any other node" pod:="<_>/<_>" node:="<_>" error:="[pod node selector does not match the node label, insufficient <_>]"`,
				`I0507 12:02:27.<_>       1 node.go:157] "Pod does not fit on any other node" pod:="<_>/<_>" node:="<_>" error:="[pod node selector does not match the node label, pod does not tolerate taints on the node, insufficient <_>, insufficient <_>]"`,
				`I0507 12:02:27.<_>       1 node.go:157] "Pod does not fit on any other node" pod:="<_>/<_>" node:="<_>" error:="[pod node selector does not match the node label, pod does not tolerate taints on the node, insufficient <_>]"`,
				`I0507 12:02:27.<_>       1 node.go:157] "Pod does not fit on any other node" pod:="<_>/<_>" node:="<_>" error:="insufficient cpu"`,
				`I0507 12:02:27.<_>       1 node.go:157] "Pod does not fit on any other node" pod:="loki-dev-005/querier-burst-6b5f6db455-5zvkm" node:="<_>" error:="[insufficient <_>, insufficient <_>]"`,
				`I0507 12:02:27.<_>       1 node.go:157] "Pod does not fit on any other node" pod:="loki-dev-005/querier-burst-6b5f6db455-5zvkm" node:="<_>" error:="pod node selector does not match the node label"`,
				`I0507 12:02:27.<_>       1 node.go:339] "no Pod antiaffinity rule found" pod="<_>/<_>"`,
				`I0507 12:04:17.595169       1 descheduler.go:155] Building a pod evictor`,
				`I0507 12:04:17.596431       1 nodeutilization.go:204] "Node is underutilized" node="gke-dev-eu-west-3-main-n2s8-1-1dd39c-d1c92061-4z2l" usage={"cpu":"984m","memory":"611Mi","pods":"16"} usagePercentage={"cpu":12.44,"memory":2.15,"pods":25}`,
				`I0507 12:04:17.596484       1 highnodeutilization.go:107] "Criteria for a node below target utilization" CPU=50 Mem=50 Pods=100`,
				`I0507 12:04:17.596504       1 highnodeutilization.go:108] "Number of underutilized nodes" totalNumber=1`,
				`I0507 12:04:17.596528       1 nodeutilization.go:260] "Total capacity to be moved" CPU=5060 Mem=112216292800 Pods=163`,
				`I0507 12:04:17.596651       1 defaultevictor.go:202] "Pod fails the following checks" pod="kube-system/metrics-server-v0.6.3-68f5b7c4d5-t5mz8" checks="[pod has system critical priority, pod has higher priority than specified priority class threshold, pod has local storage and descheduler is not configured with evictLocalStoragePods]"`,
				`I0507 12:04:17.596803       1 defaultevictor.go:202] "Pod fails the following checks" pod="gadget/gadget-zjjts" checks="[pod is a DaemonSet pod, pod has local storage and descheduler is not configured with evictLocalStoragePods]"`,
				`I0507 12:04:17.<_>       1 nodeutilization.go:207] "Node is overutilized" node="<_>" usage={"cpu":"<_>","memory":"<_>","pods":"<_>"} usagePercentage={"cpu":<_>.<_>,"memory":<_>.<_>,"pods":<_>.<_>}`,
				`I0507 12:04:17.<_>       1 nodeutilization.go:207] "Node is overutilized" node="<_>" usage={"cpu":"<_>","memory":"<_>","pods":"<_>"} usagePercentage={"cpu":<_>.<_>,"memory":<_>.<_>,"pods":<_>}`,
				`I0507 12:<_>:<_>.<_>       1 defaultevictor.go:202] "Pod fails the following checks" pod="agent-logs/<_>" checks="[pod is a DaemonSet pod, pod has higher priority than specified priority class threshold, pod has local storage and descheduler is not configured with evictLocalStoragePods]"`,
				`I0507 12:<_>:<_>.<_>       1 defaultevictor.go:202] "Pod fails the following checks" pod="conntrack-exporter/<_>" checks="[pod is a DaemonSet pod, pod has higher priority than specified priority class threshold]"`,
				`I0507 12:<_>:<_>.<_>       1 defaultevictor.go:202] "Pod fails the following checks" pod="goldpinger/<_>" checks="[pod is a DaemonSet pod, pod has higher priority than specified priority class threshold]"`,
				`I0507 12:<_>:<_>.<_>       1 defaultevictor.go:202] "Pod fails the following checks" pod="kube-system/<_>" checks="[pod has system critical priority, pod has higher priority than specified priority class threshold]"`,
				`I0507 12:<_>:<_>.<_>       1 defaultevictor.go:202] "Pod fails the following checks" pod="kube-system/<_>" checks="[pod is a DaemonSet pod, pod has system critical priority, pod has higher priority than specified priority class threshold, pod has local storage and descheduler is not configured with evictLocalStoragePods]"`,
				`I0507 12:<_>:<_>.<_>       1 defaultevictor.go:202] "Pod fails the following checks" pod="kube-system/<_>" checks="[pod is a DaemonSet pod, pod has system critical priority, pod has higher priority than specified priority class threshold]"`,
				`I0507 12:<_>:<_>.<_>       1 defaultevictor.go:202] "Pod fails the following checks" pod="kube-system/<_>" checks="[pod is a mirror pod, pod is a static pod, pod has system critical priority, pod has higher priority than specified priority class threshold, pod has local storage and descheduler is not configured with evictLocalStoragePods]"`,
				`I0507 12:<_>:<_>.<_>       1 defaultevictor.go:202] "Pod fails the following checks" pod="netfilter-exporter/<_>" checks="[pod is a DaemonSet pod, pod has higher priority than specified priority class threshold, pod has local storage and descheduler is not configured with evictLocalStoragePods]"`,
				`I0507 12:<_>:<_>.<_>       1 defaultevictor.go:202] "Pod fails the following checks" pod="node-exporter/<_>" checks="[pod is a DaemonSet pod, pod has higher priority than specified priority class threshold, pod has local storage and descheduler is not configured with evictLocalStoragePods]"`,
				`I0507 12:<_>:<_>.<_>       1 defaultevictor.go:202] "Pod fails the following checks" pod="promtail-ops/<_>" checks="[pod is a DaemonSet pod, pod has higher priority than specified priority class threshold]"`,
				`I0507 12:<_>:<_>.<_>       1 defaultevictor.go:202] "Pod fails the following checks" pod="startup/<_>" checks="[pod is a DaemonSet pod, pod has higher priority than specified priority class threshold]"`,
				`I0507 12:<_>:<_>.<_>       1 descheduler.go:<_>] "Number of evicted pods" totalEvicted=<_>`,
				`I0507 12:<_>:<_>.<_>       1 nodeutilization.go:<_>] "Evicting pods from node" node="<_>" usage={"cpu":"<_>","memory":"<_>","pods":"<_>"}`,
				`I0507 12:<_>:<_>.<_>       1 nodeutilization.go:<_>] "No removable pods on node, try next node" node="<_>"`,
				`I0507 12:<_>:<_>.<_>       1 nodeutilization.go:<_>] "Pods on node" node="<_>" allPods=<_> nonRemovablePods=<_> removablePods=<_>`,
				`I0507 12:<_>:<_>.<_>       1 profile.go:<_>] "Total number of pods evicted" extension point="Balance" evictedPods=<_>`,
				`I0507 12:<_>:<_>.<_>       1 reflector.go:<_>] k8s.io/client-go/informers/factory.go:<_>: Watch close - *v1.<_> total <_> items received`,
			},
		},
		{
			drain:     New(DefaultConfig(), nil),
			inputFile: "testdata/vault.txt",
			patterns: []string{
				`2024-05-07T10:56:38.667Z [INFO]  expiration: revoked lease: lease_id=auth/gcp/login/h4c031a99aa555040a0dd99864d828e946c6d4e31f4f5178757183def61f9d104`,
				`2024-05-07T10:<_>:<_>.<_> [INFO]  expiration: revoked lease: lease_id=auth/kubernetes/<_>/login/<_>`,
			},
		},
		{
			drain:     New(DefaultConfig(), nil),
			inputFile: "testdata/calico.txt",
			patterns: []string{
				`2024-05-08 15:23:56.403 [DEBUG][615489] felix/table.go 699: Finished loading iptables state ipVersion=0x4 table="filter"`,
				`2024-05-08 15:23:56.403 [INFO][615489] felix/summary.go 100: Summarising 1 dataplane reconciliation loops over 600ms: avg=119ms longest=119ms (resync-filter-v4)`,
				`2024-05-08 15:23:56.614 [DEBUG][76] felix/int_dataplane.go 1777: Refreshing routes`,
				`2024-05-08 15:23:56.615 [DEBUG][76] felix/route_rule.go 179: Queueing a resync of routing rules. ipVersion=4`,
				`2024-05-08 15:23:56.615 [DEBUG][76] felix/route_table.go 480: Queueing a resync of routing table. ifaceRegex="<_>.<_>" ipVersion=0x4 tableIndex=<_>`,
				`2024-05-08 15:23:56.615 [DEBUG][76] felix/route_table.go 533: Check interfaces matching regex`,
				`2024-05-08 15:23:56.615 [DEBUG][76] felix/wireguard.go 605: Queueing a resync of wireguard configuration ipVersion=0x4`,
				`2024-05-08 15:23:56.615 [DEBUG][76] felix/wireguard.go 654: Wireguard is not in-sync - verifying wireguard configuration is removed ipVersion=0x4`,
				`2024-05-08 15:23:56.617 [DEBUG][76] felix/wireguard.go 1503: Wireguard is disabled and does not exist ifaceName="wireguard.cali" ipVersion=0x4`,
				`2024-05-08 15:23:56.619 [DEBUG][76] felix/route_table.go 584: Flag no OIF for full re-sync`,
				`2024-05-08 15:23:56.619 [DEBUG][76] felix/route_table.go 614: Synchronised routes on interface ifaceName="*NoOIF*" ifaceRegex="^wireguard.cali$" ipVersion=0x4 tableIndex=1`,
				`2024-05-08 15:23:56.619 [DEBUG][76] felix/route_table.go 661: Syncing interface routes ifaceName="*NoOIF*" ifaceRegex="^wireguard.cali$" ipVersion=0x4 tableIndex=1`,
				`2024-05-08 15:23:56.619 [DEBUG][76] felix/route_table.go 686: Reconcile against kernel programming ifaceName="*NoOIF*" ifaceRegex="^wireguard.cali$" ipVersion=0x4 tableIndex=1`,
				`2024-05-08 15:23:56.624 [INFO][76] felix/summary.go 100: Summarising 1 dataplane reconciliation loops over 200ms: avg=10ms longest=10ms (resync-routes-v4,resync-routes-v4,resync-rules-v4,resync-wg)`,
				`2024-05-08 15:23:56.<_> [DEBUG][615489] felix/table.go 677: Skipping expected chain chainName="<_>" ipVersion=0x4 table="filter"`,
				`2024-05-08 15:23:56.<_> [DEBUG][615489] felix/table.go 677: Skipping expected chain chainName="<_>.<_>" ipVersion=0x4 table="filter"`,
				`2024-05-08 15:23:56.<_> [DEBUG][615489] felix/table.go 677: Skipping expected chain chainName="cali-pro-ksa.<_>.<_>" ipVersion=0x4 table="filter"`,
				`2024-05-08 15:23:56.<_> [DEBUG][76] felix/route_table.go 557: Resync: found calico-owned interface ifaceName="<_>" ifaceRegex="^azv.*" ipVersion=0x4 tableIndex=0`,
				`2024-05-08 15:23:56.<_> [DEBUG][76] felix/route_table.go 614: Synchronised routes on interface ifaceName="<_>" ifaceRegex="^azv.*" ipVersion=0x4 tableIndex=0`,
				`2024-05-08 15:23:56.<_> [DEBUG][76] felix/route_table.go 661: Syncing interface routes ifaceName="<_>" ifaceRegex="^azv.*" ipVersion=0x4 tableIndex=0`,
				`2024-05-08 15:23:56.<_> [DEBUG][76] felix/route_table.go 686: Reconcile against kernel programming ifaceName="<_>" ifaceRegex="^azv.*" ipVersion=0x4 tableIndex=0`,
				`2024-05-08 15:23:56.<_> [DEBUG][76] felix/route_table.go 880: Processing route: 254 <_> 10.68.10.<_>/32 ifaceName="<_>" ifaceRegex="^azv.*" ipVersion=0x4 tableIndex=0`,
				`2024-05-08 15:23:56.<_> [DEBUG][76] felix/route_table.go 915: Route is correct dest=10.68.10.<_>/32 ifaceName="<_>" ifaceRegex="^azv.*" ipVersion=0x4 tableIndex=0`,
				`2024-05-08 15:23:57.942 [WARNING][56] felix/table.go 654: Detected out-of-sync inserts, marking for resync actualRuleIDs=[]string{"", "", "", "", "", "", "", "", "", "", "", "", "tVnHkvAo15HuiPy0", "", ""} chainName="OUTPUT" expectedRuleIDs=[]string{"tVnHkvAo15HuiPy0", "", "", "", "", "", "", "", "", "", "", "", "", "", ""} ipVersion=0x4 table="raw"`,
				`2024-05-08 15:23:57.942 [WARNING][56] felix/table.go 654: Detected out-of-sync inserts, marking for resync actualRuleIDs=[]string{"", "", "", "", "6gwbT8clXdHdC1b1"} chainName="PREROUTING" expectedRuleIDs=[]string{"6gwbT8clXdHdC1b1", "", "", "", ""} ipVersion=0x4 table="raw"`,
				`2024-05-08 15:23:57.969 [WARNING][56] felix/table.go 654: Detected out-of-sync inserts, marking for resync actualRuleIDs=[]string{"", "", "", "", "Cz_u1IQiXIMmKD4c", "", "", "", "", "", "", "", "", "", "", "", ""} chainName="INPUT" expectedRuleIDs=[]string{"Cz_u1IQiXIMmKD4c", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", ""} ipVersion=0x4 table="filter"`,
				`2024-05-08 15:23:57.969 [WARNING][56] felix/table.go 654: Detected out-of-sync inserts, marking for resync actualRuleIDs=[]string{"", "", "", "", "tVnHkvAo15HuiPy0", "", "", "", "", ""} chainName="OUTPUT" expectedRuleIDs=[]string{"tVnHkvAo15HuiPy0", "", "", "", "", "", "", "", "", ""} ipVersion=0x4 table="filter"`,
				`2024-05-08 15:23:58.169 [INFO][2333] felix/summary.go 100: Summarising 35 dataplane reconciliation loops over 1m2s: avg=12ms longest=46ms (resync-filter-v4,resync-filter-v6,resync-mangle-v4,resync-mangle-v6,update-filter-v4,update-filter-v6)`,
				`2024-05-08 15:23:58.566 [DEBUG][3576126] felix/int_dataplane.go 957: Examining link for MTU calculation mtu=1500 name="eth0"`,
				`2024-05-08 15:23:58.680 [DEBUG][216945] felix/int_dataplane.go 1785: Reschedule kick received`,
				`2024-05-08 15:23:58.681 [DEBUG][216945] felix/feature_detect.go 112: Refreshing detected iptables features`,
				`2024-05-08 15:23:58.681 [DEBUG][216945] felix/table.go 944: Invalidating dataplane cache ipVersion=0x4 reason="refresh timer" table="nat"`,
				`2024-05-08 15:23:58.684 [DEBUG][216945] felix/feature_detect.go 242: Ran iptables --version rawVersion="iptables v1.8.4 (legacy)\n"`,
				`2024-05-08 15:23:58.684 [DEBUG][216945] felix/feature_detect.go 255: Parsed iptables version version=1.8.4`,
				`2024-05-08 15:23:58.684 [DEBUG][216945] felix/table.go 604: Loading current iptables state and checking it is correct. ipVersion=0x4 table="nat"`,
				`2024-05-08 15:23:58.684 [DEBUG][216945] felix/versionparse.go 110: Raw kernel version rawVersion="Linux version 5.15.0-1057-azure (buildd@lcy02-amd64-033) (gcc (Ubuntu 11.4.0-1ubuntu1~22.04) 11.4.0, GNU ld (GNU Binutils for Ubuntu) 2.38) #65-Ubuntu SMP Fri Feb 9 18:39:24 UTC 2024\n"`,
				`2024-05-08 15:23:58.684 [DEBUG][216945] felix/versionparse.go 118: Parsed kernel version version=5.15.0-1057`,
				`2024-05-08 15:23:58.715 [DEBUG][216945] felix/table.go 851: Parsing line ipVersion=0x4 line="# Generated by iptables-nft-save v1.8.4 on Wed May  8 15:23:58 2024" table="nat"`,
				`2024-05-08 15:23:58.716 [DEBUG][216945] felix/table.go 851: Parsing line ipVersion=0x4 line="*nat" table="nat"`,
				`2024-05-08 15:23:58.716 [DEBUG][216945] felix/table.go 881: Not an append, skipping ipVersion=0x4 line="# Generated by iptables-nft-save v1.8.4 on Wed May  8 15:23:58 2024" table="nat"`,
				`2024-05-08 15:23:58.716 [DEBUG][216945] felix/table.go 881: Not an append, skipping ipVersion=0x4 line="*nat" table="nat"`,
				`2024-05-08 15:23:58.<_> [DEBUG][216945] felix/table.go 851: Parsing line ipVersion=0x4 line=":<_> <_> [0:0]" table="nat"`,
				`2024-05-08 15:23:58.<_> [DEBUG][216945] felix/table.go 870: Found forward-reference chainName="<_>" ipVersion=0x4 line=":<_> <_> [0:0]" table="nat"`,
				`2024-05-08 15:23:58.<_> [DEBUG][3576126] felix/int_dataplane.go 954: Skipping interface for MTU detection mtu=<_> name="<_>"`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/endpoint_mgr.go 443: Reporting endpoint status. dirtyEndpoints=set.Set{}`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/health.go 167: Health: <_>`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/health.go 196: Checking state of reporter reporter=&health.reporterState{name:"<_>", reports:health.HealthReport{Live:true, Ready:true, Detail:""}, timeout:<_>, latest:health.HealthReport{Live:true, Ready:true, Detail:""}, timestamp:time.Time{wall:<_>, ext:<_>, loc:(*time.Location)(0x4ce3aa0)}}`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/health.go 245: Calculated health summary healthResult=&health.HealthReport{Live:true, Ready:true, Detail:"+------------------+---------+----------------+-----------------+--------+\n|    COMPONENT     | TIMEOUT |    LIVENESS    |    READINESS    | DETAIL |\n+------------------+---------+----------------+-----------------+--------+\n| async_calc_graph | 20s     | reporting live | reporting ready |        |\n| felix-startup    | 0s      | reporting live | reporting ready |        |\n| int_dataplane    | 1m30s   | reporting live | reporting ready |        |\n+------------------+---------+----------------+-----------------+--------+"}`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/health.go <_>: GET /<_>`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/int_dataplane.go 1773: Refreshing IP sets state`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/int_dataplane.go 1807: Applying dataplane updates`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/int_dataplane.go 2080: Asked to reschedule. delay=<_>.<_>`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/ipsets.go 234: Asked to resync with the dataplane on next update. family="inet"`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/ipsets.go 314: Resyncing ipsets with dataplane. family="inet"`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/ipsets.go 366: Finished IPSets resync family="inet" numInconsistenciesFound=0 resyncDuration=<_>.<_>`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/ipsets.go 426: Parsing IP set. family="inet" setName="<_>"`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/ipsets.go 467: Found member in dataplane canon=<_>.<_>.<_>.<_> family="inet" member="<_>.<_>.<_>.<_>" setID="this-host"`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/ipsets.go 589: Whitelisting IP sets. ID="<_>" family="inet" mainName="<_>"`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/ipsets.go 607: Skipping expected Calico IP set. family="inet" setName="<_>"`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/ipsets.go 643: No dirty IP sets. family="inet"`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/sync_client.go 347: Ping received from Typha connID=0x0 connection=&discovery.Typha{Addr:"", IP:"", NodeName:(*string)(nil)} type=""`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/sync_client.go 356: Pong sent to Typha connID=0x0 connection=&discovery.Typha{Addr:"", IP:"", NodeName:(*string)(nil)} type=""`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/sync_client.go 434: New message from Typha. connID=0x0 connection=&discovery.Typha{Addr:"", IP:"", NodeName:(*string)(nil)} envelope=syncproto.Envelope{Message:syncproto.MsgPing{Timestamp:time.Date(2024, time.May, 8, 15, 23, <_>, <_>, time.Local)}} type=""`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/table.go 1233: In nftables mode, restarting transaction between updates and deletions. ipVersion=0x4 table="<_>"`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/table.go 1263: Update ended up being no-op, skipping call to ip(6)tables-restore. ipVersion=0x4 table="<_>"`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/wireguard.go 652: Wireguard is not enabled, skipping sync ipVersion=0x4`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/xdp_state.go 1004: Updating ipsetIDsToMembers cache. family=4`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/xdp_state.go 1043: Processing pending diff state. cs=&intdataplane.xdpSystemState{IfaceNameToData:map[string]intdataplane.xdpIfaceData{}, XDPEligiblePolicies:map[proto.PolicyID]intdataplane.xdpRules{}} family=4`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/xdp_state.go 1270: Finished processing pending diff state. bpfActions=intdataplane.xdpBPFActions{CreateMap:set.Typed[string]{}, RemoveMap:set.Typed[string]{}, AddToMap:map[string]map[string]uint32{}, RemoveFromMap:map[string]map[string]uint32{}, InstallXDP:set.Typed[string]{}, UninstallXDP:set.Typed[string]{}, MembersToDrop:map[string]map[string]uint32{}, MembersToAdd:map[string]map[string]uint32{}} family=4 newCS=&intdataplane.xdpSystemState{IfaceNameToData:map[string]intdataplane.xdpIfaceData{}, XDPEligiblePolicies:map[proto.PolicyID]intdataplane.xdpRules{}}`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/xdp_state.go 1605: Getting member changes. family=4 oldMembers=map[string]set.Set[string]{}`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/xdp_state.go 1798: Processing BPF actions. family="ipv4"`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/xdp_state.go 1932: Finished processing BPF actions. family="ipv4"`,
				`2024-05-08 15:23:<_>.<_> [DEBUG][<_>] felix/xdp_state.go 968: Processing member updates. family=4`,
				`2024-05-08 15:23:<_>.<_> [INFO][<_>] felix/summary.go 100: Summarising <_> dataplane reconciliation loops over <_>.<_>: avg=<_> longest=<_> (<_>)`,
				"bird: Netlink: No route to host",
			},
		},
		{
			drain:     New(DefaultConfig(), nil),
			inputFile: "testdata/grafana-ruler.txt",
			patterns: []string{
				`level=debug ts=2024-05-29T13:44:15.804597912Z caller=remote_instance_store.go:51 user=297794 slug=leanix msg="calling SaveAlertInstance"`,
				`level=debug ts=2024-05-29T13:44:15.<_> caller=remote_instance_store.go:51 user=396586 slug=opengov msg="calling SaveAlertInstance"`,
				`level=debug ts=2024-05-29T13:44:15.<_> caller=remote_instance_store.go:51 user=<_> slug=<_> msg="calling SaveAlertInstance"`,
				`logger=ngalert.scheduler user=102553 slug=flownative version=1 fingerprint=4ad9e35be0f80ca3 attempt=1 now=2024-05-29T13:44:10Z t=2024-05-29T13:44:15.79499903Z level=debug msg="Alert rule evaluated" results="[{Instance: State:Normal Error:<nil> Results:map[] Values:map[] EvaluatedAt:2024-05-29 13:44:10 +0000 UTC EvaluationDuration:5.794695854s EvaluationString:}]" duration=116.038803ms`,
				`logger=ngalert.scheduler user=473762 slug=intentiq version=35 fingerprint=0bc4b6f46a852420 attempt=1 now=2024-05-29T13:44:10Z t=2024-05-29T13:44:15.788200731Z level=debug msg="Alert rule evaluated" results="[{Instance:datasource_uid=grafanacloud-prom, ref_id=A State:NoData Error:<nil> Results:map[] Values:map[] EvaluatedAt:2024-05-29 13:44:10 +0000 UTC EvaluationDuration:5.787878355s EvaluationString:}]" duration=15.345212ms`,
				`logger=ngalert.scheduler user=70430 slug=dapperlabs version=1 fingerprint=65a68c433031b4e0 attempt=1 now=2024-05-29T13:44:10Z t=2024-05-29T13:44:15.790598463Z level=debug msg="Alert rule evaluated" results="[{Instance: State:Normal Error:<nil> Results:map[] Values:map[] EvaluatedAt:2024-05-29 13:44:10 +0000 UTC EvaluationDuration:5.78875161s EvaluationString:}]" duration=1.693079007s`,
				`logger=ngalert.state.manager user=102553 slug=flownative instance= t=2024-05-29T13:44:15.795103234Z level=debug msg="Setting next state" handler=resultNormal`,
				`logger=ngalert.state.manager user=15338 slug=rstsoftwarerc instance= t=2024-05-29T13:44:15.790951656Z level=debug msg="Keeping state" state=Alerting previous_ends_at=2024-05-29T13:47:00Z next_ends_at=2024-05-29T13:48:00Z`,
				`logger=ngalert.state.manager user=172772 slug=ppbtradingtribe instance="datasource_uid=p06gSxS7k, ref_id=A" t=2024-05-29T13:44:15.793080651Z level=debug msg="Keeping state" state=Normal`,
				`logger=ngalert.state.manager user=172772 slug=ppbtradingtribe t=2024-05-29T13:44:15.79304032Z level=debug msg="State manager processing evaluation results" resultCount=1`,
				`logger=ngalert.state.manager user=228733 slug=csmoney instance="datasource_uid=grafanacloud-logs, ref_id=A" t=2024-05-29T13:44:15.796750449Z level=debug msg="Setting next state" handler=resultNoData`,
				`logger=ngalert.state.manager user=371756 slug=asapp instance="company_marker=dish" t=2024-05-29T13:44:15.788780219Z level=debug msg="Keeping state" state=Normal`,
				`logger=ngalert.state.manager user=371756 slug=asapp instance="company_marker=optimumfixed" t=2024-05-29T13:44:15.788904162Z level=debug msg="Keeping state" state=Normal`,
				`logger=ngalert.state.manager user=371756 slug=asapp instance="company_marker=rcn" t=2024-05-29T13:44:15.789011178Z level=debug msg="Setting next state" handler=resultNormal`,
				`logger=ngalert.state.manager user=412141 slug=sharethrough instance="datasource_uid=pFBylkiVz, ref_id=Swap Usage for Alert" t=2024-05-29T13:44:15.792756002Z level=debug msg="Setting next state" handler=resultNoData`,
				`logger=ngalert.state.manager user=412141 slug=sharethrough instance="datasource_uid=pFBylkiVz, ref_id=Swap Usage for Alert" t=2024-05-29T13:44:15.792775073Z level=debug msg="Keeping state" state=Normal`,
				`logger=ngalert.state.manager user=430961 slug=solifi instance= t=2024-05-29T13:44:15.799932951Z level=debug msg="Setting next state" handler=resultNormal`,
				`logger=ngalert.state.manager user=430961 slug=solifi instance= t=2024-05-29T13:44:15.799945019Z level=debug msg="Keeping state" state=Normal`,
				`logger=ngalert.state.manager user=473762 slug=intentiq instance="datasource_uid=grafanacloud-prom, ref_id=A" t=2024-05-29T13:44:15.<_> level=debug msg="Execution no data state is Normal" handler=resultNormal previous_handler=resultNoData`,
				`logger=ngalert.state.manager user=473762 slug=intentiq instance="datasource_uid=grafanacloud-prom, ref_id=A" t=2024-05-29T13:44:15.<_> level=debug msg="Keeping state" state=Normal`,
				`logger=ngalert.state.manager user=473762 slug=intentiq instance="datasource_uid=grafanacloud-prom, ref_id=A" t=2024-05-29T13:44:15.<_> level=debug msg="Setting next state" handler=resultNoData`,
				`logger=ngalert.state.manager user=473762 slug=intentiq t=2024-05-29T13:44:15.788261794Z level=debug msg="State manager processing evaluation results" resultCount=1`,
				`logger=ngalert.state.manager user=630397 slug=tatin instance= t=2024-05-29T13:44:15.795542988Z level=debug msg="Keeping state" state=Normal`,
				`logger=ngalert.state.manager user=679029 slug=joveoprodaws instance="datasource_uid=grafanacloud-logs, ref_id=A" t=2024-05-29T13:44:15.800327814Z level=debug msg="Setting next state" handler=resultNoData`,
				`logger=ngalert.state.manager user=692010 slug=mercariusprod instance="datasource_uid=gfds-prometheus-wrapper, ref_id=B" t=2024-05-29T13:44:15.791100679Z level=debug msg="Execution no data state is Normal" handler=resultNormal previous_handler=resultNoData`,
				`logger=ngalert.state.manager user=692010 slug=mercariusprod instance="datasource_uid=gfds-prometheus-wrapper, ref_id=B" t=2024-05-29T13:44:15.791114955Z level=debug msg="Keeping state" state=Normal`,
				`logger=ngalert.state.manager user=692010 slug=mercariusprod instance="datasource_uid=gfds-prometheus-wrapper, ref_id=B" t=2024-05-29T13:44:15.791129917Z level=debug msg="Setting next state" handler=resultNoData`,
				`logger=ngalert.state.manager user=84535 slug=arweave instance= t=2024-05-29T13:44:15.796640981Z level=debug msg="Setting next state" handler=resultNormal`,
				`logger=ngalert.state.manager user=84535 slug=arweave t=2024-05-29T13:44:15.796542294Z level=debug msg="State manager processing evaluation results" resultCount=1`,
				`logger=ngalert.state.manager user=893151 slug=cmtdsnp instance="cluster=tds-np-cluster, container=<_>, instance=172.30.<_>.<_>:8080, job=integrations/kubernetes/kube-state-metrics, namespace=<_>, pod=<_>, uid=<_>" t=2024-05-29T13:44:15.<_> level=debug msg="Setting next state" handler=resultNormal`,
				`logger=ngalert.state.manager user=893151 slug=cmtdsnp instance="cluster=tds-np-cluster, container=consul, instance=172.30.43.160:8080, job=integrations/kubernetes/kube-state-metrics, namespace=tds, pod=wcs9-tds-devus-vault-con-74f6c575b8-6d879, uid=f5320297-1117-400f-9704-d4f43fa1127d" t=2024-05-29T13:44:15.78870732Z level=debug msg="Keeping state" state=Normal`,
				`logger=ngalert.state.manager user=893151 slug=cmtdsnp instance="cluster=tds-np-cluster, container=crs-app, instance=172.30.<_>.<_>:8080, job=integrations/kubernetes/kube-state-metrics, namespace=tds, pod=<_>, uid=<_>" t=2024-05-29T13:44:15.<_> level=debug msg="Keeping state" state=Normal`,
				`logger=ngalert.state.manager user=893151 slug=cmtdsnp instance="cluster=tds-np-cluster, container=frontend, instance=172.30.43.160:8080, job=integrations/kubernetes/kube-state-metrics, namespace=tds, pod=tdsdevcaauth-exo-frontend-5c569cbc88-fr7t4, uid=2b8456c8-297f-4763-8f00-f8076b542d7c" t=2024-05-29T13:44:15.790564871Z level=debug msg="Keeping state" state=Normal`,
				`logger=ngalert.state.manager user=893151 slug=cmtdsnp instance="cluster=tds-np-cluster, container=node, instance=172.30.43.160:8080, job=integrations/kubernetes/kube-state-metrics, namespace=tds-devops, pod=exo-devca-cicd-288-zcl2b-9ws4z-nzgt7, uid=ca99b6a7-f08f-475a-adf6-dcf8c8936eed" t=2024-05-29T13:44:15.791738618Z level=debug msg="Keeping state" state=Normal`,
				`logger=ngalert.state.manager user=893151 slug=cmtdsnp instance="cluster=tds-np-cluster, container=search-app-master, instance=172.30.43.160:8080, job=integrations/kubernetes/kube-state-metrics, namespace=tds, pod=tdsdevauthsearch-app-master-65969fb8d5-c7nl4, uid=c4f14b2b-581a-4543-a848-af6e25ada58a" t=2024-05-29T13:44:15.79227249Z level=debug msg="Keeping state" state=Normal`,
				`logger=ngalert.state.manager user=893151 slug=cmtdsnp instance="cluster=tds-np-cluster, container=search-app-repeater, instance=172.30.<_>.<_>:8080, job=integrations/kubernetes/kube-state-metrics, namespace=tds, pod=<_>, uid=<_>" t=2024-05-29T13:44:15.<_> level=debug msg="Keeping state" state=Normal`,
				`logger=ngalert.state.manager user=893151 slug=cmtdsnp instance="cluster=tds-np-cluster, container=tdsdevauthts-utils, instance=172.30.43.160:8080, job=integrations/kubernetes/kube-state-metrics, namespace=tds, pod=tdsdevauthts-utils-7f54f8d7b4-njddr, uid=352d7df2-7832-41f3-ad3e-cbe1a060c968" t=2024-05-29T13:44:15.793846886Z level=debug msg="Keeping state" state=Normal`,
				`logger=ngalert.state.manager user=893151 slug=cmtdsnp instance="cluster=tds-np-cluster, container=tdsqalivets-utils, instance=172.30.43.160:8080, job=integrations/kubernetes/kube-state-metrics, namespace=tds, pod=tdsqalivets-utils-75b748978f-r2vkj, uid=1d39d0d7-d483-427b-ba91-45d897674698" t=2024-05-29T13:44:15.794284465Z level=debug msg="Keeping state" state=Normal`,
				`logger=ngalert.state.manager user=893151 slug=cmtdsnp instance="cluster=tds-np-cluster, container=ts-app, instance=172.30.<_>.<_>:8080, job=integrations/kubernetes/kube-state-metrics, namespace=tds, pod=<_>, uid=<_>" t=2024-05-29T13:44:15.<_> level=debug msg="Keeping state" state=Normal`,
				`logger=ngalert.state.manager user=893151 slug=cmtdsnp instance="cluster=tds-np-cluster, container=ts-web, instance=172.30.43.160:8080, job=integrations/kubernetes/kube-state-metrics, namespace=tds, pod=tdsqaauthts-web-57f5b6f56b-bdmh9, uid=8f6b5224-94ce-4f5d-ba08-03f9fc2f572f" t=2024-05-29T13:44:15.795397351Z level=debug msg="Keeping state" state=Normal`,
				`logger=ngalert.state.manager.persist user=14927 slug=rstsoftware t=2024-05-29T13:44:15.798496844Z level=debug msg="Saving alert states done" count=1 max_state_save_concurrency=1 duration=26.340653ms`,
				`logger=ngalert.state.manager.persist user=20177 slug=paddledash t=2024-05-29T13:44:15.806655602Z level=debug msg="Saving alert states" count=1 max_state_save_concurrency=1`,
				`logger=ngalert.state.manager.persist user=<_> slug=<_> t=2024-05-29T13:44:15.<_> level=debug msg="Saving alert states" count=<_> max_state_save_concurrency=1`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.inputFile, func(t *testing.T) {
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
			slices.Sort(output)

			if outputPatternsForTestUpdate {
				for _, pattern := range output {
					fmt.Printf("`%s`,\n", pattern)
				}
			}

			require.Equal(t, tt.patterns, output)
			require.Falsef(t, outputPatternsForTestUpdate, `outputPatternsForTestUpdate should only be used locally to update test patterns.`)
		})
	}
}

func TestDrain_TrainGeneratesMatchablePatterns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		drain      *Drain
		inputLines []string
	}{
		{
			name:  "should match each line against a pattern",
			drain: New(DefaultConfig(), nil),
			inputLines: []string{
				"test test test test",
				"test test test test",
				"test test test test",
				"test test test test",
			},
		},
		{
			name:  "should also match newlines",
			drain: New(DefaultConfig(), nil),
			inputLines: []string{
				`test test test test
`,
				`test test test test
`,
				`test test test test
`,
				`test test test test
`,
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, line := range tt.inputLines {
				tt.drain.Train(line, 0)
			}

			for _, line := range tt.inputLines {
				match := tt.drain.Match(line)
				require.NotNil(t, match, `Line should match a cluster`)
			}
		})
	}
}

func TestDrain_TrainGeneratesPatternsMatchableByLokiPatternFilter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		drain      *Drain
		inputLines []string
	}{
		{
			name:  "should extract patterns that all lines match",
			drain: New(DefaultConfig(), nil),
			inputLines: []string{
				"test 1 test test",
				"test 2 test test",
				"test 3 test test",
				"test 4 test test",
			},
		},
		{
			name:  "should extract patterns that match if line ends with newlines",
			drain: New(DefaultConfig(), nil),
			inputLines: []string{
				`test 1 test test
`,
				`test 2 test test
`,
				`test 3 test test
`,
				`test 4 test test
`,
			},
		},
		{
			name:  "should extract patterns that match if line ends with empty space",
			drain: New(DefaultConfig(), nil),
			inputLines: []string{
				`test 1 test test			`,
				`test 2 test test			`,
				`test 3 test test			`,
				`test 4 test test			`,
			},
		},
		{
			name:  "should extract patterns that match if line starts with empty space",
			drain: New(DefaultConfig(), nil),
			inputLines: []string{
				`			test 1 test test`,
				`			test 2 test test`,
				`			test 3 test test`,
				`			test 4 test test`,
			},
		},
		{
			name:  "Scheduler patterns are matchable",
			drain: New(DefaultConfig(), nil),
			inputLines: []string{
				`ts=2024-05-30T12:50:36.648377186Z caller=scheduler_processor.go:143 level=warn msg="error contacting scheduler" err="rpc error: code = Unavailable desc = connection error: desc = \"error reading server preface: EOF\"" addr=10.0.151.101:9095`,
				`ts=2024-05-30T12:50:36.350575929Z caller=scheduler_processor.go:143 level=warn msg="error contacting scheduler" err="rpc error: code = Unavailable desc = connection error: desc = \"error reading server preface: EOF\"" addr=10.0.151.101:9095`,
				`ts=2024-05-30T12:50:36.335784477Z caller=scheduler_processor.go:143 level=warn msg="error contacting scheduler" err="rpc error: code = Unavailable desc = connection error: desc = \"error reading server preface: EOF\"" addr=10.0.151.101:9095`,
				`ts=2024-05-30T12:50:36.250406732Z caller=scheduler_processor.go:143 level=warn msg="error contacting scheduler" err="rpc error: code = Unavailable desc = connection error: desc = \"error reading server preface: EOF\"" addr=10.0.151.101:9095`,
				`ts=2024-05-30T12:50:36.248030329Z caller=scheduler_processor.go:143 level=warn msg="error contacting scheduler" err="rpc error: code = Unavailable desc = connection error: desc = \"error reading server preface: EOF\"" addr=10.0.45.239:9095`,
				`ts=2024-05-30T12:50:36.176344754Z caller=scheduler_processor.go:143 level=warn msg="error contacting scheduler" err="rpc error: code = Unavailable desc = connection error: desc = \"error reading server preface: EOF\"" addr=10.0.151.101:9095`,
				`ts=2024-05-30T12:50:36.174730772Z caller=scheduler_processor.go:143 level=warn msg="error contacting scheduler" err="rpc error: code = Unavailable desc = connection error: desc = \"error reading server preface: EOF\"" addr=10.0.151.101:9095`,
				`ts=2024-05-30T12:50:36.076517207Z caller=scheduler_processor.go:143 level=warn msg="error contacting scheduler" err="rpc error: code = Unavailable desc = connection error: desc = \"error reading server preface: EOF\"" addr=10.0.45.239:9095`,
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, line := range tt.inputLines {
				tt.drain.Train(line, 0)
			}
			require.Equal(t, 1, len(tt.drain.Clusters()))
			cluster := tt.drain.Clusters()[0]

			matcher, err := pattern.ParseLineFilter([]byte(cluster.String()))
			require.NoError(t, err)

			for _, line := range tt.inputLines {
				passes := matcher.Test([]byte(line))
				require.Truef(t, passes, "Line should match extracted pattern: \nPatt[%q] \nLine[%q]", cluster.String(), line)

			}
		})
	}
}

func TestDeduplicatePlaceholders(b *testing.T) {
	type dedupCase struct {
		line string
		want string
	}
	cases := []dedupCase{
		{
			line: "abcd",
			want: "abcd",
		},
		{
			line: "<_><_>abcd",
			want: "<_>abcd",
		},
		{
			line: strings.Repeat("<_>", 100),
			want: "<_>",
		},
		{
			line: "<_> <_>",
			want: "<_> <_>",
		},
		{
			line: strings.Repeat("<_> ", 100),
			want: strings.Repeat("<_> ", 100),
		},
		{
			line: "<_><<_>",
			want: "<_><<_>",
		},
		{
			line: "<_><->",
			want: "<_><->",
		},
		{
			line: strings.Repeat(strings.Repeat("<_>", 100)+" ", 100),
			want: strings.Repeat("<_> ", 100),
		},
		{
			line: "<<<<<<<_><_>>>>>>>>",
			want: "<<<<<<<_>>>>>>>>",
		},
		{
			line: strings.Repeat("A", 100) + "<_><_>",
			want: strings.Repeat("A", 100) + "<_>",
		},
	}

	for i, tc := range cases {
		b.Run(fmt.Sprintf("Dedup %d", i), func(t *testing.T) {
			got := deduplicatePlaceholders(tc.line, `<_>`)
			require.Equal(t, tc.want, got)
		})
	}
}
