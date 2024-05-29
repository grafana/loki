package drain

import (
	"bufio"
	"fmt"
	"os"
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
				`ts=2024-04-16T15:10:42.556278698Z caller=filetargetmanager.go:361 level=info component=logs logs_config=default msg="Adding target" key="/var/log/pods/*b92ee988-5c26-4c64-bba3-ff6a01723759/grafana/*.log:{app=\"grafana\", conprof=\"true\", container=\"grafana\", instanceId=\"i1111\", job=\"hosted-grafana/grafana\", name=\"grafana\", namespace=\"hosted-grafana\", org=\"orgnamehere\", plan=\"free\", pod=\"orgnamehere-grafana-7c65678f86-9zhlb\", pod_template_hash=\"7c65678f86\", resource_version=\"143638246\", slug=\"orgnamehere\", stackId=\"866772\"}"`,
				`ts=2024-04-16T15:10:42.556706613Z caller=filetargetmanager.go:361 level=info component=logs logs_config=default msg="Adding target" key="/var/log/pods/*b92ee988-5c26-4c64-bba3-ff6a01723759/hgrun/*.log:{app=\"grafana\", conprof=\"true\", container=\"hgrun\", instanceId=\"i1111\", job=\"hosted-grafana/grafana\", name=\"grafana\", namespace=\"hosted-grafana\", org=\"orgnamehere\", plan=\"free\", pod=\"orgnamehere-grafana-7c65678f86-9zhlb\", pod_template_hash=\"7c65678f86\", resource_version=\"143638246\", slug=\"orgnamehere\", stackId=\"866772\"}"`,
				`ts=2024-04-16T15:10:42.556930066Z caller=filetargetmanager.go:361 level=info component=logs logs_config=default msg="Adding target" key="/var/log/pods/*b92ee988-5c26-4c64-bba3-ff6a01723759/hg-plugins/*.log:{app=\"grafana\", conprof=\"true\", container=\"hg-plugins\", instanceId=\"i1111\", job=\"hosted-grafana/grafana\", name=\"grafana\", namespace=\"hosted-grafana\", org=\"orgnamehere\", plan=\"free\", pod=\"orgnamehere-grafana-7c65678f86-9zhlb\", pod_template_hash=\"7c65678f86\", resource_version=\"143638246\", slug=\"orgnamehere\", stackId=\"866772\"}"`,
				`ts=2024-04-16T15:10:42.557102408Z caller=filetargetmanager.go:361 level=info component=logs logs_config=default msg="Adding target" key="/var/log/pods/*b92ee988-5c26-4c64-bba3-ff6a01723759/hosted-grafana-security/*.log:{app=\"grafana\", conprof=\"true\", container=\"hosted-grafana-security\", instanceId=\"i1111\", job=\"hosted-grafana/grafana\", name=\"grafana\", namespace=\"hosted-grafana\", org=\"orgnamehere\", plan=\"free\", pod=\"orgnamehere-grafana-7c65678f86-9zhlb\", pod_template_hash=\"7c65678f86\", resource_version=\"143638246\", slug=\"orgnamehere\", stackId=\"866772\"}"`,
				`ts=2024-04-16T15:10:43.192290389Z caller=filetargetmanager.go:361 level=info component=logs logs_config=default msg="Adding target" key="/var/log/pods/*19a1cce8-5f04-46e0-a124-292b0dd9b343/testcoordinator/*.log:{batch_kubernetes_io_controller_uid=\"25ec5edf-f78e-468b-b6f3-3b9685f0cc8f\", batch_kubernetes_io_job_name=\"testcoordinator-job-2665838\", container=\"testcoordinator\", controller_uid=\"25ec5edf-f78e-468b-b6f3-3b9685f0cc8f\", job=\"k6-cloud/testcoordinator\", job_name=\"testcoordinator-job-2665838\", name=\"testcoordinator\", namespace=\"k6-cloud\", pod=\"testcoordinator-job-2665838-9g8ds\"}"`,
				`ts=2024-04-16T15:10:43.551543875Z caller=filetargetmanager.go:397 level=info component=logs logs_config=default msg="Removing target" key="/var/log/pods/*35649bfd-52ff-4281-9294-5f65fd5a89fc/marketplaces-api/*.log:{container=\"marketplaces-api\", job=\"grafana-com/marketplaces-api\", name=\"marketplaces-api\", namespace=\"grafana-com\", pod=\"marketplaces-api-f67ff7567-gqrvb\", pod_template_hash=\"f67ff7567\"}"`,
				`ts=<_> caller=filetarget.go:192 level=info component=logs logs_config=default msg="filetarget:watcher closed, tailer stopped, positions saved" path=<_>`,
				`ts=<_> caller=filetarget.go:313 level=info component=logs logs_config=default msg="watching new directory" directory=<_>`,
				`ts=<_> caller=filetarget.go:326 level=info component=logs logs_config=default msg="removing directory from watcher" directory=<_>`,
				`ts=<_> caller=filetargetmanager.go:181 level=info component=logs logs_config=default msg="received file watcher event" name=<_> op=CREATE`,
				`ts=<_> caller=filetargetmanager.go:361 level=info component=logs logs_config=default msg="Adding target" key=<_> \"kube-proxy\", container=\"kube-proxy\", job=<_> namespace=\"kube-system\", pod=\"kube-proxy-gke-ops-us-east-0-main-n2s32-1-1dd39c-32ae1dde-hmhw\", tier=\"node\"}"`,
				`ts=<_> caller=filetargetmanager.go:397 level=info component=logs logs_config=default msg="Removing target" key=<_> \"grafana\", conprof=\"true\", container=\"grafana\", instanceId=<_> job=\"hosted-grafana/grafana\", name=\"grafana\", namespace=\"hosted-grafana\", org=<_> plan=\"free\", pod=<_> pod_template_hash=<_> resource_version=<_> slug=<_> stackId=<_>`,
				`ts=<_> caller=filetargetmanager.go:397 level=info component=logs logs_config=default msg="Removing target" key=<_> \"grafana\", conprof=\"true\", container=\"hg-plugins\", instanceId=<_> job=\"hosted-grafana/grafana\", name=\"grafana\", namespace=\"hosted-grafana\", org=<_> plan=\"free\", pod=<_> pod_template_hash=<_> resource_version=<_> slug=<_> stackId=<_>`,
				`ts=<_> caller=filetargetmanager.go:397 level=info component=logs logs_config=default msg="Removing target" key=<_> \"grafana\", conprof=\"true\", container=\"hgrun\", instanceId=<_> job=\"hosted-grafana/grafana\", name=\"grafana\", namespace=\"hosted-grafana\", org=<_> plan=\"free\", pod=<_> pod_template_hash=<_> resource_version=<_> slug=<_> stackId=<_>`,
				`ts=<_> caller=filetargetmanager.go:397 level=info component=logs logs_config=default msg="Removing target" key=<_> \"grafana\", conprof=\"true\", container=\"hosted-grafana-security\", instanceId=<_> job=\"hosted-grafana/grafana\", name=\"grafana\", namespace=\"hosted-grafana\", org=<_> plan=\"free\", pod=<_> pod_template_hash=<_> resource_version=<_> slug=<_> stackId=<_>`,
				`ts=<_> caller=log.go:168 component=logs logs_config=default level=info msg="Re-opening moved/deleted file <_> ..."`,
				`ts=<_> caller=log.go:168 component=logs logs_config=default level=info msg="Seeked <_> - &{Offset:0 Whence:0}"`,
				`ts=<_> caller=log.go:168 component=logs logs_config=default level=info msg="Successfully reopened <_>`,
				`ts=<_> caller=log.go:168 component=logs logs_config=default level=info msg="Waiting for <_> to appear..."`,
				`ts=<_> caller=logfmt.go:139 level=error component=logs logs_config=default component=file_pipeline component=stage type=logfmt msg="failed to decode logfmt" err="bufio.Scanner:token too long"`,
				`ts=<_> caller=logfmt.go:139 level=error component=logs logs_config=default component=file_pipeline component=stage type=logfmt msg="failed to decode logfmt" err="logfmt syntax error at pos <_> on line 1:unexpected '\"'"`,
				`ts=<_> caller=tailer.go:118 level=info component=logs logs_config=default component=tailer msg="position timer:exited" path=<_>`,
				`ts=<_> caller=tailer.go:147 level=info component=logs logs_config=default component=tailer msg="tail routine:started" path=<_>`,
				`ts=<_> caller=tailer.go:155 level=info component=logs logs_config=default component=tailer msg="tail routine:exited" path=<_>`,
				`ts=<_> caller=tailer.go:164 level=info component=logs logs_config=default component=tailer msg="tail routine:tail channel closed, stopping tailer" path=<_> reason=null`,
				`ts=<_> caller=tailer.go:207 level=info component=logs logs_config=default component=tailer msg="skipping update of position for a file which does not currently exist" path=<_>`,
				`ts=<_> caller=tailer.go:245 level=info component=logs logs_config=default component=tailer msg="stopped tailing file" path=<_>`,
				`ts=<_> level=info msg="finished node evaluation" controller_id=module.http.cloudwatch_pipelines node_id=<_> duration=<_>`,
			},
		},
		{
			drain:     New(DefaultConfig(), nil),
			inputFile: `testdata/ingester-logfmt.txt`,
			patterns: []string{
				`ts=2024-04-17T09:52:46.363974185Z caller=http.go:194 level=debug traceID=1b48f5156a61ca69 msg="GET /debug/pprof/delta_mutex (200) 1.161082ms"`,
				`ts=<_> caller=head.go:216 level=debug tenant=987678 msg="profile is empty after delta computation" metricName=memory`,
				`ts=<_> caller=http.go:194 level=debug traceID=<_> orgID=<_> msg="POST /ingester.v1.IngesterService/Push (200) <_>`,
			},
		},
		{
			drain:     New(DefaultConfig(), nil),
			inputFile: `testdata/drone-json.txt`,
			patterns: []string{
				`{"duration":<_> "debug","method":"GET","msg":"request completed","referer":"","remote":"10.136.105.40:52702","request":"/metrics","status":200,"time":<_> <_> <_> "GrafanaAgent/v0.40.3 (flow; linux; helm)"}`,
				`{"id":<_> "debug","max-pool":4,"min-pool":0,"msg":"check capacity","pending-builds":0,"running-builds":0,"server-buffer":0,"server-capacity":0,"server-count":0,"time":<_> <_> <_>`,
				`{"id":<_> "debug","msg":"calculate server capacity","time":<_> <_> <_>`,
				`{"id":<_> "debug","msg":"calculate unfinished jobs","time":<_> <_> <_>`,
				`{"id":<_> "debug","msg":"check capacity complete","time":<_> <_> <_>`,
				`{"id":<_> "debug","msg":"no capacity changes required","time":<_> <_> <_>`,
			},
		},
		{
			drain:     New(DefaultConfig(), nil),
			inputFile: "testdata/distributor-logfmt.txt",
			patterns: []string{
				`ts=2024-05-02T12:17:22.115385619Z caller=http.go:194 level=debug traceID=7836a12bb7f1964e orgID=75 msg="POST /ingest?aggregationType=sum&from=1714652227107641016&name=checkoutservice%7B__session_id__%3D294b9729f5a7de95%2Cnamespace%3Dotel-demo%7D&sampleRate=100&spyName=gospy&units=samples&until=1714652242109516917 (200) 1.562143ms"`,
				`ts=2024-05-02T12:17:22.242343806Z caller=http.go:194 level=debug traceID=404c6a83a18e66a4 orgID=75 msg="POST /ingest?aggregationType=average&from=1714652227232613927&name=checkoutservice%7B__session_id__%3D294b9729f5a7de95%2Cnamespace%3Dotel-demo%7D&sampleRate=0&spyName=gospy&units=goroutines&until=1714652242232506798 (200) 2.902485ms"`,
				`ts=<_> caller=http.go:194 level=debug traceID=<_> orgID=1819 msg="POST /pyroscope/ingest?aggregationType=sum&from=1714652230&name=<_> 0&spyName=scrape&units=samples&until=1714652240 (200) <_>`,
				`ts=<_> caller=http.go:194 level=debug traceID=<_> orgID=75 msg="POST /ingest?aggregationType=&from=1714652227232613927&name=checkoutservice%7B__session_id__%3D294b9729f5a7de95%2Cnamespace%3Dotel-demo%7D&sampleRate=<_> gospy&units=&until=1714652242232506798 (200) <_>`,
				`ts=<_> caller=http.go:194 level=debug traceID=<_> orgID=<_> msg="POST /push.v1.PusherService/Push <_> <_>`,
			},
		},
		{
			drain:     New(DefaultConfig(), nil),
			inputFile: "testdata/journald.txt",
			patterns: []string{
				`						exec /bin/hgrun -log.level=debug launch -bundledPluginsManifest /proc/$(pidof plugins-pause)/root/manifest.json -bundledPluginsDir /proc/$(pidof plugins-pause)/root/plugins],WorkingDir:,Ports:[]ContainerPort{ContainerPort{Name:http-metrics,HostPort:0,ContainerPort:80,Protocol:TCP,HostIP:,},ContainerPort{Name:grpc,HostPort:0,ContainerPort:10000,Protocol:TCP,HostIP:,},ContainerPort{Name:profiling,HostPort:0,ContainerPort:6060,Protocol:TCP,HostIP:,},},Env:[]EnvVar{EnvVar{Name:HG_API,Value:http://hosted-grafana-api,ValueFrom:nil,},EnvVar{Name:HG_INSTANCE_SLUG,Value:<_> nil,},EnvVar{Name:HG_INSTANCE_SECRET,Value:<_> nil,},EnvVar{Name:EXTRA_OPTIONS,Value:-profile -profile-port=6060 -profile-addr=0.0.0.0,ValueFrom:nil,},EnvVar{Name:HG_CREATE_TIME_MS,Value:<_> nil,},EnvVar{Name:HG_PULL_POLICY,Value:Always,ValueFrom:nil,},EnvVar{Name:HG_START_REASON,Value:active,ValueFrom:nil,},EnvVar{Name:HGRUN_SECURE_PLUGINS,Value:false,ValueFrom:nil,},EnvVar{Name:HGRUN_PLUGIN_RUNNER_ROOT_CA,Value:false,ValueFrom:nil,},EnvVar{Name:OTEL_EXPORTER_OTLP_TRACES_ENDPOINT,Value:http://jaeger-agent.jaeger.svc.cluster.local:4317,ValueFrom:nil,},EnvVar{Name:JAEGER_SAMPLER_PARAM,Value:1,ValueFrom:nil,},EnvVar{Name:OTEL_RESOURCE_ATTRIBUTES,Value:cluster=dev-us-central-0,namespace=hosted-grafana,ValueFrom:nil,},EnvVar{Name:HG_PROBE_PATH,Value:/api/health,ValueFrom:nil,},EnvVar{Name:HGRUN_EXIT_ON_PLUGIN_FAIL,Value:true,ValueFrom:nil,},EnvVar{Name:HGRUN_PLUGIN_INSTALL_RETRIES,Value:2,ValueFrom:nil,},EnvVar{Name:HGRUN_PLUGIN_INSTALL_CONCURRENCY,Value:1,ValueFrom:nil,},EnvVar{Name:HGRUN_LAUNCH_TIMEOUT,Value:3m0s,ValueFrom:nil,},EnvVar{Name:GOMEMLIMIT,Value:429496730,ValueFrom:nil,},},Resources:ResourceRequirements{Limits:ResourceList{memory: {{536870912 0} {<nil>}  BinarySI},},Requests:ResourceList{cpu: {{26 -3} {<nil>} 26m DecimalSI},memory: {{293601280 0} {<nil>}  BinarySI},},Claims:[]ResourceClaim{},},VolumeMounts:[]VolumeMount{},LivenessProbe:&Probe{ProbeHandler:ProbeHandler{Exec:nil,HTTPGet:&HTTPGetAction{Path:/api/health,Port:{0 80 },Host:,Scheme:HTTP,HTTPHeaders:[]HTTPHeader{},},TCPSocket:nil,GRPC:nil,},InitialDelaySeconds:300,TimeoutSeconds:10,PeriodSeconds:30,SuccessThreshold:1,FailureThreshold:3,TerminationGracePeriodSeconds:nil,},ReadinessProbe:&Probe{ProbeHandler:ProbeHandler{Exec:&ExecAction{Command:[/bin/hgrun check],},HTTPGet:nil,TCPSocket:nil,GRPC:nil,},InitialDelaySeconds:0,TimeoutSeconds:30,PeriodSeconds:30,SuccessThreshold:1,FailureThreshold:3,TerminationGracePeriodSeconds:nil,},Lifecycle:&Lifecycle{PostStart:nil,PreStop:&LifecycleHandler{Exec:&ExecAction{Command:[/bin/hgrun drain -timeout 1m0s -waitTime 55s],},HTTPGet:nil,TCPSocket:nil,},},TerminationMessagePath:/dev/termination-log,ImagePullPolicy:Always,SecurityContext:&SecurityContext{Capabilities:&Capabilities{Add:[SYS_PTRACE],Drop:[],},Privileged:nil,SELinuxOptions:nil,RunAsUser:nil,RunAsNonRoot:nil,ReadOnlyRootFilesystem:nil,AllowPrivilegeEscalation:nil,RunAsGroup:nil,ProcMount:nil,WindowsOptions:nil,SeccompProfile:nil,},Stdin:false,StdinOnce:false,TTY:false,EnvFrom:[]EnvFromSource{},TerminationMessagePolicy:File,VolumeDevices:[]VolumeDevice{},StartupProbe:nil,ResizePolicy:[]ContainerResizePolicy{},RestartPolicy:nil,} start failed in pod <_>  ErrImagePull: [rpc error: code =NotFound desc =failed to pull and unpack image "us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>  failed to resolve reference "us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>  us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>  not found, failed to pull and unpack image "us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>  failed to resolve reference "us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>  unexpected status from HEAD request to https:<_>  403 Forbidden]`,
				`						ln --force -s /proc/$(pidof hgrun-pause)/root/bin/hgrun /bin/hgrun;`,
				`						while [ "$(pidof plugins-pause)" = "" ]; do sleep 0.5; done;`,
				`	ts=2024-05-07T11:59:32.025687537Z level=error caller=http_client.go:56 app=hgrun hgrun_version=0.1.453-59-gf3f63162a msg="request`,
				`	ts=2024-05-07T11:59:<_> level=error caller=http_client.go:56 app=hgrun <_> msg="request failed" error="Get \"http://127.0.0.1:3000/api/health\": dial tcp 127.0.0.1:3000: connect: connection refused" method=GET url=http://127.0.0.1:3000/api/health`,
				`2024-05-07T11:59:43.484606Z INFO ExtHandler ExtHandler Downloading agent manifest`,
				`2024-05-07T11:59:<_> INFO TelemetryEventsCollector ExtHandler Collected 2 events for extension: Microsoft.Azure.Extensions.CustomScript`,
				`<_>  Consumed <_> CPU time.`,
				`<_>  Deactivated successfully.`,
				`AVC apparmor="DENIED" operation="ptrace" profile="cri-containerd.apparmor.d" pid=<_> comm="pidof" requested_mask="read" denied_mask="read" peer="unconfined"`,
				`E0507 11:59:29.725681    3089 pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"azure-resourcemanager-exporter\" with CrashLoopBackOff:\"back-off 5m0s restarting failed container=azure-resourcemanager-exporter pod=azure-resourcemanager-exporter-6b5b58c666-rsttd_infra-exporters(5a95f801-309c-4f33-864a-406262c6ece6)\"" pod="infra-exporters/azure-resourcemanager-exporter-6b5b58c666-rsttd" podUID="5a95f801-309c-4f33-864a-406262c6ece6"`,
				`E0507 11:59:31.554203    4531 pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"frontend\" with CrashLoopBackOff:\"back-off 5m0s restarting failed container=frontend pod=otel-demo-alt-dev-frontend-79ccf98858-mbj4x_otel-demo-alt(d08e620e-00d0-49f1-a195-820a62e8de8f)\"" pod="otel-demo-alt/otel-demo-alt-dev-frontend-79ccf98858-mbj4x" podUID="d08e620e-00d0-49f1-a195-820a62e8de8f"`,
				`E0507 11:59:31.928148    4734 pod_workers.go:1300] "Error syncing pod, skipping" err="unmounted volumes=[terraform-drift-detector-data], unattached volumes=[terraform-drift-detector-data], failed to process volumes=[]:context deadline exceeded" pod="terraform-drift-detector/terraform-drift-detector-d68b4c545-jg2vj" podUID="6c607496-ef26-454e-b2f2-4cb75b233fa3"`,
				`E0507 11:59:34.856101    4727 pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"grafana-render-security\" with ImagePullBackOff: \"Back-off pulling image \\\"us.gcr.io/hosted-grafana/hosted-grafana-security:0.1.181\\\"\"" pod="integration/grafana-render-service-cbff479fc-cj9tp" podUID="0e3114d1-2f3a-49d6-a71d-dbc75050d8e0"`,
				`E0507 11:59:34.923938    3027 kuberuntime_manager.go:1261] container &Container{Name:mysqld-exporter,Image:prom/mysqld-exporter:v0.13.0,Command:[],Args:[--collect.info_schema.innodb_metrics],WorkingDir:,Ports:[]ContainerPort{ContainerPort{Name:http-metrics,HostPort:0,ContainerPort:9104,Protocol:TCP,HostIP:,},},Env:[]EnvVar{EnvVar{Name:MYSQL_USER,Value:,ValueFrom:&EnvVarSource{FieldRef:nil,ResourceFieldRef:nil,ConfigMapKeyRef:nil,SecretKeyRef:&SecretKeySelector{LocalObjectReference:LocalObjectReference{Name:testcrossplane-user-exporter,},Key:username,Optional:nil,},},},EnvVar{Name:MYSQL_PASSWORD,Value:,ValueFrom:&EnvVarSource{FieldRef:nil,ResourceFieldRef:nil,ConfigMapKeyRef:nil,SecretKeyRef:&SecretKeySelector{LocalObjectReference:LocalObjectReference{Name:testcrossplane-user-exporter,},Key:password,Optional:nil,},},},EnvVar{Name:MYSQL_HOST,Value:,ValueFrom:&EnvVarSource{FieldRef:nil,ResourceFieldRef:nil,ConfigMapKeyRef:nil,SecretKeyRef:&SecretKeySelector{LocalObjectReference:LocalObjectReference{Name:testcrossplane-user-exporter,},Key:endpoint,Optional:nil,},},},EnvVar{Name:MYSQL_PORT,Value:,ValueFrom:&EnvVarSource{FieldRef:nil,ResourceFieldRef:nil,ConfigMapKeyRef:nil,SecretKeyRef:&SecretKeySelector{LocalObjectReference:LocalObjectReference{Name:testcrossplane-user-exporter,},Key:port,Optional:nil,},},},EnvVar{Name:MYSQL_TLS_MODE,Value:preferred,ValueFrom:nil,},EnvVar{Name:DATA_SOURCE_NAME,Value:$(MYSQL_USER):$(MYSQL_PASSWORD)@tcp($(MYSQL_HOST):$(MYSQL_PORT))/?tls=$(MYSQL_TLS_MODE),ValueFrom:nil,},},Resources:ResourceRequirements{Limits:ResourceList{},Requests:ResourceList{},Claims:[]ResourceClaim{},},VolumeMounts:[]VolumeMount{VolumeMount{Name:kube-api-access-dzx7d,ReadOnly:true,MountPath:/var/run/secrets/kubernetes.io/serviceaccount,SubPath:,MountPropagation:nil,SubPathExpr:,},},LivenessProbe:nil,ReadinessProbe:nil,Lifecycle:nil,TerminationMessagePath:/dev/termination-log,ImagePullPolicy:IfNotPresent,SecurityContext:nil,Stdin:false,StdinOnce:false,TTY:false,EnvFrom:[]EnvFromSource{},TerminationMessagePolicy:File,VolumeDevices:[]VolumeDevice{},StartupProbe:nil,ResizePolicy:[]ContainerResizePolicy{},RestartPolicy:nil,} start failed in pod testcrossplane-exporter-c67cfc58f-vbzl4_crossplane-playground(3d49134d-3378-4ec3-824c-5ff4ea2590a5): CreateContainerConfigError: secret "testcrossplane-user-exporter" not found`,
				`E0507 11:59:34.923984    3027 pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"mysqld-exporter\" with CreateContainerConfigError: \"secret \\\"testcrossplane-user-exporter\\\" not found\"" pod="crossplane-playground/testcrossplane-exporter-c67cfc58f-vbzl4" podUID="3d49134d-3378-4ec3-824c-5ff4ea2590a5"`,
				`E0507 11:59:35.928465    4734 pod_workers.go:1300] "Error syncing pod, skipping" err="unmounted volumes=[custom-grafana-agent], unattached volumes=[], failed to process volumes=[]:context deadline exceeded" pod="loki-dev-010/custom-grafana-agent-856948968f-6jfks" podUID="17b244cc-ecb9-4fbc-beaa-8fa47fafe013"`,
				`E0507 11:59:37.252214    4736 pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"ksm\" with CrashLoopBackOff:\"back-off 5m0s restarting failed container=ksm pod=new-relic-nri-bundle-nrk8s-ksm-6c785668f5-jcxh2_integration(f7cc3cca-2ffb-4fde-a73e-a4ba8b0f6b3c)\"" pod="integration/new-relic-nri-bundle-nrk8s-ksm-6c785668f5-jcxh2" podUID="f7cc3cca-2ffb-4fde-a73e-a4ba8b0f6b3c"`,
				`E0507 11:59:39.149450    4729 pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"cluster-agent\" with CrashLoopBackOff:\"back-off 5m0s restarting failed container=cluster-agent pod=appdynamics-cluster-agent-appdynamics-cluster-agent-56667dmbnkv_integration(69bc5e6c-0451-443e-af8a-c831871afbb8)\"" pod="integration/appdynamics-cluster-agent-appdynamics-cluster-agent-56667dmbnkv" podUID="69bc5e6c-0451-443e-af8a-c831871afbb8"`,
				`E0507 11:59:41.375655    4736 kuberuntime_manager.go:1256] container &Container{Name:ruler,Image:grafana/enterprise-metrics:v2.12.0,Command:[],Args:[-target=ruler -config.expand-env=true -config.file=/etc/mimir/mimir.yaml -distributor.remote-timeout=10s],WorkingDir:,Ports:[]ContainerPort{ContainerPort{Name:http-metrics,HostPort:0,ContainerPort:8080,Protocol:TCP,HostIP:,},ContainerPort{Name:grpc,HostPort:0,ContainerPort:9095,Protocol:TCP,HostIP:,},ContainerPort{Name:memberlist,HostPort:0,ContainerPort:7946,Protocol:TCP,HostIP:,},},Env:[]EnvVar{EnvVar{Name:JAEGER_AGENT_HOST,Value:alloy-otlp.alloy-otlp.svc.cluster.local.,ValueFrom:nil,},EnvVar{Name:JAEGER_TAGS,Value:namespace=ge-metrics-federation,cluster=dev-us-central-0,ValueFrom:nil,},EnvVar{Name:JAEGER_SAMPLER_MANAGER_HOST_PORT,Value:http://alloy-otlp.alloy-otlp.svc.cluster.local.:5778/sampling,ValueFrom:nil,},EnvVar{Name:GOOGLE_APPLICATION_CREDENTIALS,Value:/var/secrets/google/credentials.json,ValueFrom:nil,},EnvVar{Name:AM_TOKEN,Value:,ValueFrom:&EnvVarSource{FieldRef:nil,ResourceFieldRef:nil,ConfigMapKeyRef:nil,SecretKeyRef:&SecretKeySelector{LocalObjectReference:LocalObjectReference{Name:ruler-alertmanager-token,},Key:token,Optional:nil,},},},EnvVar{Name:JAEGER_REPORTER_MAX_QUEUE_SIZE,Value:1000,ValueFrom:nil,},},Resources:ResourceRequirements{Limits:ResourceList{},Requests:ResourceList{cpu: {{100 -3} {<nil>} 100m DecimalSI},memory: {{134217728 0} {<nil>}  BinarySI},},Claims:[]ResourceClaim{},},VolumeMounts:[]VolumeMount{VolumeMount{Name:gcs-credentials,ReadOnly:false,MountPath:/var/secrets/google/,SubPath:,MountPropagation:nil,SubPathExpr:,},VolumeMount{Name:config,ReadOnly:false,MountPath:/etc/mimir,SubPath:,MountPropagation:nil,SubPathExpr:,},VolumeMount{Name:license,ReadOnly:false,MountPath:/license,SubPath:,MountPropagation:nil,SubPathExpr:,},VolumeMount{Name:runtime-config,ReadOnly:false,MountPath:/var/mimir,SubPath:,MountPropagation:nil,SubPathExpr:,},VolumeMount{Name:storage,ReadOnly:false,MountPath:/data,SubPath:,MountPropagation:nil,SubPathExpr:,},VolumeMount{Name:active-queries,ReadOnly:false,MountPath:/active-query-tracker,SubPath:,MountPropagation:nil,SubPathExpr:,},VolumeMount{Name:kube-api-access-jtnbs,ReadOnly:true,MountPath:/var/run/secrets/kubernetes.io/serviceaccount,SubPath:,MountPropagation:nil,SubPathExpr:,},},LivenessProbe:nil,ReadinessProbe:&Probe{ProbeHandler:ProbeHandler{Exec:nil,HTTPGet:&HTTPGetAction{Path:/ready,Port:{1 0 http-metrics},Host:,Scheme:HTTP,HTTPHeaders:[]HTTPHeader{},},TCPSocket:nil,GRPC:nil,},InitialDelaySeconds:45,TimeoutSeconds:1,PeriodSeconds:10,SuccessThreshold:1,FailureThreshold:3,TerminationGracePeriodSeconds:nil,},Lifecycle:nil,TerminationMessagePath:/dev/termination-log,ImagePullPolicy:IfNotPresent,SecurityContext:&SecurityContext{Capabilities:&Capabilities{Add:[],Drop:[ALL],},Privileged:nil,SELinuxOptions:nil,RunAsUser:nil,RunAsNonRoot:nil,ReadOnlyRootFilesystem:*true,AllowPrivilegeEscalation:*false,RunAsGroup:nil,ProcMount:nil,WindowsOptions:nil,SeccompProfile:nil,},Stdin:false,StdinOnce:false,TTY:false,EnvFrom:[]EnvFromSource{},TerminationMessagePolicy:File,VolumeDevices:[]VolumeDevice{},StartupProbe:nil,ResizePolicy:[]ContainerResizePolicy{},RestartPolicy:nil,} start failed in pod gem-mimir-ruler-5f56f7846b-fgxdm_ge-metrics-federation(07c06e21-137b-4fdd-b7d3-703f0a567720): CreateContainerConfigError: secret "ruler-alertmanager-token" not found`,
				`E0507 11:59:<_>    4731 pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"overrides-exporter\" with ImagePullBackOff: \"Back-off pulling image \\\"us.gcr.io/kubernetes-dev/enterprise-logs:callum-shard-firstlast-08\\\"\"" pod="loki-dev-010/overrides-exporter-98c77fd66-6zj6m" podUID="1ff5bf3e-5856-4f6f-ae04-273f2dee170b"`,
				`E0507 11:59:<_>    <_> kuberuntime_manager.go:1256] container &Container{Name:grafana,Image:us.gcr.io/hosted-grafana/hosted-grafana-pro:<_> [/bin/sh],Args:[-c set -e; while [ "$(pidof hgrun-pause)" ="" ]; do sleep 0.5; done;`,
				`E0507 11:59:<_>    <_> kuberuntime_manager.go:1256] container &Container{Name:pdc,Image:us.gcr.io/hosted-grafana/pdc:0.1.415,Command:[],Args:[-proxy.auth.ca-keys-dir=/var/run/secrets/pdc-certs -proxy.socks-server.addr=:10443 -proxy.ssh-server.addr=:2222 -proxy.use-socks-username-for-routing -proxy.api.http-address=:9182 -proxy.check-connpool-address-in-ring -memberlist.join=dns+gossip-ring.pdc.svc.cluster.local:7946 -api.http-address=:11443 -distributor.enabled=true -distributor.addr=:10444 -distributor.use-socks-username-for-routing -gateway.enabled=true -gateway.addr=:2244 -log.level=debug -certs.ca-private-key-file=/var/run/secrets/pdc-certs/ca.key -certs.ca-cert-file=/var/run/secrets/pdc-certs/ca.crt -certs.ca-pub-file=/var/run/secrets/pdc-certs/ca.pub -certs.cluster=local-k8s -shard-size=3 -graceful-shutdown-period=30s -enable-multiple-networks],WorkingDir:,Ports:[]ContainerPort{ContainerPort{Name:socks,HostPort:0,ContainerPort:10443,Protocol:TCP,HostIP:,},ContainerPort{Name:ssh,HostPort:0,ContainerPort:2222,Protocol:TCP,HostIP:,},ContainerPort{Name:distributor,HostPort:0,ContainerPort:10444,Protocol:TCP,HostIP:,},ContainerPort{Name:gateway,HostPort:0,ContainerPort:2244,Protocol:TCP,HostIP:,},ContainerPort{Name:api,HostPort:0,ContainerPort:11443,Protocol:TCP,HostIP:,},},Env:[]EnvVar{EnvVar{Name:POD_NAME,Value:,ValueFrom:&EnvVarSource{FieldRef:&ObjectFieldSelector{APIVersion:v1,FieldPath:metadata.name,},ResourceFieldRef:nil,ConfigMapKeyRef:nil,SecretKeyRef:nil,},},},Resources:ResourceRequirements{Limits:ResourceList{cpu: {{500 -3} {<nil>} 500m DecimalSI},memory: {{134217728 0} {<nil>}  BinarySI},},Requests:ResourceList{cpu: {{250 -3} {<nil>} 250m DecimalSI},memory: {{67108864 0} {<nil>}  BinarySI},},Claims:[]ResourceClaim{},},VolumeMounts:[]VolumeMount{VolumeMount{Name:pdc-certs,ReadOnly:true,MountPath:/var/run/secrets/pdc-certs,SubPath:,MountPropagation:nil,SubPathExpr:,},VolumeMount{Name:<_> true,MountPath:/var/run/secrets/kubernetes.io/serviceaccount,SubPath:,MountPropagation:nil,SubPathExpr:,},},LivenessProbe:nil,ReadinessProbe:&Probe{ProbeHandler:ProbeHandler{Exec:nil,HTTPGet:&HTTPGetAction{Path:/ready,Port:{0 11443 },Host:,Scheme:HTTP,HTTPHeaders:[]HTTPHeader{},},TCPSocket:nil,GRPC:nil,},InitialDelaySeconds:40,TimeoutSeconds:1,PeriodSeconds:5,SuccessThreshold:1,FailureThreshold:3,TerminationGracePeriodSeconds:nil,},Lifecycle:&Lifecycle{PostStart:nil,PreStop:&LifecycleHandler{Exec:&ExecAction{Command:[/bin/sleep 5],},HTTPGet:nil,TCPSocket:nil,},},TerminationMessagePath:/dev/termination-log,ImagePullPolicy:Never,SecurityContext:nil,Stdin:false,StdinOnce:false,TTY:false,EnvFrom:[]EnvFromSource{},TerminationMessagePolicy:File,VolumeDevices:[]VolumeDevice{},StartupProbe:nil,ResizePolicy:[]ContainerResizePolicy{},RestartPolicy:nil,} start failed in pod <_>  ErrImageNeverPull: Container image "us.gcr.io/hosted-grafana/pdc:0.1.415" is not present with pull policy of Never`,
				`E0507 11:59:<_>    <_> kuberuntime_manager.go:1256] container &Container{Name:ruler,Image:grafana/enterprise-metrics:v2.11.1,Command:[],Args:[-target=ruler -config.expand-env=true -config.file=/etc/mimir/mimir.yaml],WorkingDir:,Ports:[]ContainerPort{ContainerPort{Name:http-metrics,HostPort:0,ContainerPort:8080,Protocol:TCP,HostIP:,},ContainerPort{Name:grpc,HostPort:0,ContainerPort:9095,Protocol:TCP,HostIP:,},ContainerPort{Name:memberlist,HostPort:0,ContainerPort:7946,Protocol:TCP,HostIP:,},},Env:[]EnvVar{EnvVar{Name:JAEGER_AGENT_HOST,Value:<_> nil,},EnvVar{Name:JAEGER_TAGS,Value:namespace=ge-metrics-federation,cluster=dev-us-central-0,ValueFrom:nil,},EnvVar{Name:JAEGER_SAMPLER_MANAGER_HOST_PORT,Value:http:<_> 5778/sampling,ValueFrom:nil,},EnvVar{Name:GOOGLE_APPLICATION_CREDENTIALS,Value:/var/secrets/google/credentials.json,ValueFrom:nil,},EnvVar{Name:AM_TOKEN,Value:,ValueFrom:&EnvVarSource{FieldRef:nil,ResourceFieldRef:nil,ConfigMapKeyRef:nil,SecretKeyRef:&SecretKeySelector{LocalObjectReference:LocalObjectReference{Name:ruler-alertmanager-token,},Key:token,Optional:nil,},},},},Resources:ResourceRequirements{Limits:ResourceList{},Requests:ResourceList{cpu: {{100 -3} {<nil>} 100m DecimalSI},memory: {{134217728 0} {<nil>}  BinarySI},},Claims:[]ResourceClaim{},},VolumeMounts:[]VolumeMount{VolumeMount{Name:gcs-credentials,ReadOnly:false,MountPath:/var/secrets/google/,SubPath:,MountPropagation:nil,SubPathExpr:,},VolumeMount{Name:config,ReadOnly:false,MountPath:/etc/mimir,SubPath:,MountPropagation:nil,SubPathExpr:,},VolumeMount{Name:license,ReadOnly:false,MountPath:/license,SubPath:,MountPropagation:nil,SubPathExpr:,},VolumeMount{Name:runtime-config,ReadOnly:false,MountPath:/var/mimir,SubPath:,MountPropagation:nil,SubPathExpr:,},VolumeMount{Name:storage,ReadOnly:false,MountPath:/data,SubPath:,MountPropagation:nil,SubPathExpr:,},VolumeMount{Name:active-queries,ReadOnly:false,MountPath:/active-query-tracker,SubPath:,MountPropagation:nil,SubPathExpr:,},VolumeMount{Name:<_> true,MountPath:/var/run/secrets/kubernetes.io/serviceaccount,SubPath:,MountPropagation:nil,SubPathExpr:,},},LivenessProbe:nil,ReadinessProbe:&Probe{ProbeHandler:ProbeHandler{Exec:nil,HTTPGet:&HTTPGetAction{Path:/ready,Port:{1 0 http-metrics},Host:,Scheme:HTTP,HTTPHeaders:[]HTTPHeader{},},TCPSocket:nil,GRPC:nil,},InitialDelaySeconds:45,TimeoutSeconds:1,PeriodSeconds:10,SuccessThreshold:1,FailureThreshold:3,TerminationGracePeriodSeconds:nil,},Lifecycle:nil,TerminationMessagePath:/dev/termination-log,ImagePullPolicy:IfNotPresent,SecurityContext:&SecurityContext{Capabilities:&Capabilities{Add:[],Drop:[ALL],},Privileged:nil,SELinuxOptions:nil,RunAsUser:nil,RunAsNonRoot:nil,ReadOnlyRootFilesystem:*true,AllowPrivilegeEscalation:*false,RunAsGroup:nil,ProcMount:nil,WindowsOptions:nil,SeccompProfile:nil,},Stdin:false,StdinOnce:false,TTY:false,EnvFrom:[]EnvFromSource{},TerminationMessagePolicy:File,VolumeDevices:[]VolumeDevice{},StartupProbe:nil,ResizePolicy:[]ContainerResizePolicy{},RestartPolicy:nil,} start failed in pod <_>  CreateContainerConfigError: secret "ruler-alertmanager-token" not found`,
				`E0507 11:59:<_>    <_> pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"gcom-sync\" with ImagePullBackOff: \"Back-off pulling image \\\"us.gcr.io/kubernetes-dev/frontend-monitoring:6a8eb5a\\\"\"" <_> <_>`,
				`E0507 11:59:<_>    <_> pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"grafana\" with ErrImagePull: \"[rpc error: code =NotFound desc =failed to pull and unpack image \\\"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>  failed to resolve reference \\\"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>  us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>  not found, failed to pull and unpack image \\\"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>  failed to resolve reference \\\"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>  unexpected status from HEAD request to https:<_>  403 Forbidden]\"" <_> <_>`,
				`E0507 11:59:<_>    <_> pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"grafana\" with ImagePullBackOff: \"Back-off pulling image \\\"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_> <_> <_>`,
				`E0507 11:59:<_>    <_> pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"pdc\" with ErrImageNeverPull: \"Container image \\\"us.gcr.io/hosted-grafana/pdc:0.1.415\\\" is not present with pull policy of Never\"" <_> <_>`,
				`E0507 11:59:<_>    <_> pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"ruler\" with CreateContainerConfigError: \"secret \\\"ruler-alertmanager-token\\\" not found\"" <_> <_>`,
				`E0507 11:59:<_>    <_> prober.go:104] "Probe errored" err="rpc error: code =NotFound desc =failed to exec in container: failed to load task: no running task found: task <_> not found: not found" probeType="Readiness" <_> <_> containerName="grafana"`,
				`E0507 11:59:<_>    <_> remote_image.go:180] "PullImage from image service failed" err="rpc error: code =NotFound desc =failed to pull and unpack image \"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>  failed to resolve reference \"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>  us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>  not found" image="us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>`,
				`E0507 11:59:<_>    <_> remote_image.go:180] "PullImage from image service failed" err="rpc error: code =Unknown desc =failed to pull and unpack image \"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>  failed to resolve reference \"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>  unexpected status from HEAD request to https:<_>  403 Forbidden" image="us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>`,
				`E0507 11:59:<_>    <_> remote_runtime.go:432] "ContainerStatus from runtime service failed" err="rpc error: code =NotFound desc =an error occurred when try to find container <_>  not found" <_>`,
				`E0507 11:59:<_>    <_> remote_runtime.go:496] "ExecSync cmd from runtime service failed" err="rpc error: code =NotFound desc =failed to exec in container: failed to load task: no running task found: task <_> not found: not found" <_> cmd=["/bin/hgrun","check"]`,
				`E0507 <_>    4733 pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"prometheus\" with CrashLoopBackOff:\"back-off 5m0s restarting failed container=prometheus pod=bryan-prometheus-0_bryan-prometheus(6dadfe71-eb19-4231-a96e-c64bb5499a1e)\"" pod="bryan-prometheus/bryan-prometheus-0" podUID="6dadfe71-eb19-4231-a96e-c64bb5499a1e"`,
				`E0507 <_>    <_> pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"agent\" with CrashLoopBackOff:\"back-off 5m0s restarting failed container=agent pod=<_> pod=<_> podUID=<_>`,
				`E0507 <_>    <_> pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"cortex-gw\" with CrashLoopBackOff:\"back-off 5m0s restarting failed container=cortex-gw pod=<_> pod=<_> podUID=<_>`,
				`E0507 <_>    <_> pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"goldpinger\" with CrashLoopBackOff:\"back-off 5m0s restarting failed container=goldpinger pod=<_> pod=<_> podUID=<_>`,
				`E0507 <_>    <_> pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"grafana\" with CrashLoopBackOff:\"back-off <_> restarting failed container=grafana pod=<_> pod=<_> podUID=<_>`,
				`E0507 <_>    <_> pod_workers.go:1300] "Error syncing pod, skipping" err="failed to \"StartContainer\" for \"support-agent\" with CrashLoopBackOff:\"back-off 5m0s restarting failed container=support-agent pod=<_> pod=<_> podUID=<_>`,
				`E0507 <_>    <_> prober.go:239] "Unable to write all bytes from execInContainer" err="short write" expectedBytes=<_> actualBytes=10240`,
				`I0507 11:59:29.320184 1537502 kubelet_pods.go:906] "Unable to retrieve pull secret, the image pull may not succeed." pod="logs-endpoint-dev-005/kafka-controller-0" secret="" err="secret \"not-needed\" not found"`,
				`I0507 11:59:31.815514    2791 azure_credentials.go:220] image(us.gcr.io/hosted-grafana/hosted-grafana-pro) is not from ACR, return empty authentication`,
				`I0507 11:59:32.409568  581823 cache.go:40] re-using cached key and certificate`,
				`I0507 11:59:33.422254 1537502 kubelet_getters.go:187] "Pod status updated" pod="kube-system/kube-proxy-gke-dev-us-central-0-main-n2s16-3-1dd-9b502d96-x28r" status="Running"`,
				`I0507 11:59:34.518822    3224 kuberuntime_container.go:745] "Killing container with a grace period" pod="hosted-grafana/hosted-grafana-api-7b6bd9b949-9csb4" podUID="25cb986c-3d6c-4ed0-abf3-ee59ed6175f9" containerName="hgapi" containerID="containerd://c91436db00920ec961b9d5d6b4859d80a912e862e34fb5c45d8a85684fe6a97e" gracePeriod=30`,
				`I0507 11:59:34.834734    3224 reconciler_common.go:172] "operationExecutor.UnmountVolume started for volume \"kube-api-access-95j2t\" (UniqueName: \"kubernetes.io/projected/25cb986c-3d6c-4ed0-abf3-ee59ed6175f9-kube-api-access-95j2t\") pod \"25cb986c-3d6c-4ed0-abf3-ee59ed6175f9\" (UID: \"25cb986c-3d6c-4ed0-abf3-ee59ed6175f9\") "`,
				`I0507 11:59:34.834794    3224 reconciler_common.go:172] "operationExecutor.UnmountVolume started for volume \"pdc-certs\" (UniqueName: \"kubernetes.io/secret/25cb986c-3d6c-4ed0-abf3-ee59ed6175f9-pdc-certs\") pod \"25cb986c-3d6c-4ed0-abf3-ee59ed6175f9\" (UID: \"25cb986c-3d6c-4ed0-abf3-ee59ed6175f9\") "`,
				`I0507 11:59:34.834835    3224 reconciler_common.go:172] "operationExecutor.UnmountVolume started for volume \"gcs-serviceaccount\" (UniqueName: \"kubernetes.io/secret/25cb986c-3d6c-4ed0-abf3-ee59ed6175f9-gcs-serviceaccount\") pod \"25cb986c-3d6c-4ed0-abf3-ee59ed6175f9\" (UID: \"25cb986c-3d6c-4ed0-abf3-ee59ed6175f9\") "`,
				`I0507 11:59:34.836955    3224 operation_generator.go:888] UnmountVolume.TearDown succeeded for volume "kubernetes.io/secret/25cb986c-3d6c-4ed0-abf3-ee59ed6175f9-pdc-certs" (OuterVolumeSpecName: "pdc-certs") pod "25cb986c-3d6c-4ed0-abf3-ee59ed6175f9" (UID: "25cb986c-3d6c-4ed0-abf3-ee59ed6175f9"). InnerVolumeSpecName "pdc-certs". PluginName "kubernetes.io/secret", VolumeGidValue ""`,
				`I0507 11:59:34.841404    3224 operation_generator.go:888] UnmountVolume.TearDown succeeded for volume "kubernetes.io/projected/25cb986c-3d6c-4ed0-abf3-ee59ed6175f9-kube-api-access-95j2t" (OuterVolumeSpecName: "kube-api-access-95j2t") pod "25cb986c-3d6c-4ed0-abf3-ee59ed6175f9" (UID: "25cb986c-3d6c-4ed0-abf3-ee59ed6175f9"). InnerVolumeSpecName "kube-api-access-95j2t". PluginName "kubernetes.io/projected", VolumeGidValue ""`,
				`I0507 11:59:34.841447    3224 operation_generator.go:888] UnmountVolume.TearDown succeeded for volume "kubernetes.io/secret/25cb986c-3d6c-4ed0-abf3-ee59ed6175f9-gcs-serviceaccount" (OuterVolumeSpecName: "gcs-serviceaccount") pod "25cb986c-3d6c-4ed0-abf3-ee59ed6175f9" (UID: "25cb986c-3d6c-4ed0-abf3-ee59ed6175f9"). InnerVolumeSpecName "gcs-serviceaccount". PluginName "kubernetes.io/secret", VolumeGidValue ""`,
				`I0507 11:59:34.854084    4727 kubelet_pods.go:906] "Unable to retrieve pull secret, the image pull may not succeed." pod="integration/grafana-render-service-cbff479fc-cj9tp" secret="" err="secret \"us-gcr-io-hosted-grafana\" not found"`,
				`I0507 11:59:34.936025    3224 reconciler_common.go:300] "Volume detached for volume \"pdc-certs\" (UniqueName: \"kubernetes.io/secret/25cb986c-3d6c-4ed0-abf3-ee59ed6175f9-pdc-certs\") on node \"ip-10-60-2-58.us-east-2.compute.internal\" DevicePath \"\""`,
				`I0507 11:59:37.133005    3782 prober.go:107] "Probe failed" probeType="Readiness" pod="loki-dev-014/loki-dev-014-rollout-operator-58fc68b876-2qhmp" podUID="e6504036-2514-4ecc-b78c-c47061f60c9f" containerName="rollout-operator" probeResult="failure" output="HTTP probe failed with statuscode:500"`,
				`I0507 11:59:37.915108    4726 prober.go:107] "Probe failed" probeType="Readiness" pod="agent-management-dev-002/agent-management-api-7ff7b9b9-k9nft" podUID="9893f9ac-f3e4-41fb-8da7-592061d2386c" containerName="agent-management-api" probeResult="failure" output="HTTP probe failed with statuscode:400"`,
				`I0507 11:59:38.116658    2791 azure_credentials.go:220] image(us.gcr.io/hosted-grafana/hg-plugins) is not from ACR, return empty authentication`,
				`I0507 11:59:39.168633    2776 kubelet.go:2493] "SyncLoop (probe)" probe="readiness" status="" pod="hosted-grafana/dafdeveuwest2-grafana-7845d969b5-f8h5q"`,
				`I0507 11:59:39.560605    4739 kubelet_pods.go:906] "Unable to retrieve pull secret, the image pull may not succeed." pod="logs-endpoint-dev-005/kafka-exporter-766c6757b5-bggf6" secret="" err="secret \"not-needed\" not found"`,
				`I0507 11:59:<_>    2791 azure_credentials.go:220] image(us.gcr.io/hosted-grafana/hgrun) is not from ACR, return empty authentication`,
				`I0507 11:59:<_>    3224 reconciler_common.go:300] "Volume detached for volume <_> (UniqueName: <_> on node \"ip-10-60-2-58.us-east-2.compute.internal\" DevicePath \"\""`,
				`I0507 11:59:<_>    6247 prober.go:107] "Probe failed" probeType="Readiness" pod="grafana-agent/grafana-agent-helm-4" podUID="c36c5200-1cd6-4093-893c-c022f91af996" containerName="grafana-agent" probeResult="failure" output="Get \"http://10.0.99.125:3090/-/ready\": dial tcp 10.0.99.125:3090: connect: connection refused"`,
				`I0507 11:59:<_>    <_> generic.go:334] "Generic (PLEG): container finished" <_> <_> exitCode=1`,
				`I0507 11:59:<_>    <_> kubelet.go:<_> "SyncLoop (PLEG): event for pod" <_> event={"ID":<_> "ContainerDied","Data":<_>`,
				`I0507 11:59:<_>    <_> kubelet.go:<_> "SyncLoop (PLEG): event for pod" <_> event={"ID":<_> "ContainerStarted","Data":<_>`,
				`I0507 11:59:<_>    <_> kubelet.go:<_> "SyncLoop DELETE" source="api" <_>`,
				`I0507 11:59:<_>    <_> kubelet.go:<_> "SyncLoop REMOVE" source="api" <_>`,
				`I0507 11:59:<_>    <_> kubelet_getters.go:187] "Pod status updated" <_> status="Running"`,
				`I0507 11:59:<_>    <_> kubelet_volumes.go:<_> "Cleaned up orphaned pod volumes dir" <_> <_>`,
				`I0507 11:59:<_>    <_> pod_container_deletor.go:53] "DeleteContainer returned error" containerID={"Type":"containerd","ID":<_> err="failed to get container status <_>  rpc error: code =NotFound desc =an error occurred when try to find container <_>  not found"`,
				`I0507 11:59:<_>    <_> scope.go:117] "RemoveContainer" <_>`,
				`I0507 11:59:<_> <_> cache.go:40] re-using cached key and certificate`,
				`I0507 <_>    <_> kubelet.go:2498] "SyncLoop (probe)" probe="liveness" status="unhealthy" pod=<_>`,
				`I0507 <_>    <_> kubelet.go:2498] "SyncLoop (probe)" probe="readiness" status="ready" pod=<_>`,
				`I0507 <_>    <_> kubelet_pods.go:906] "Unable to retrieve pull secret, the image pull may not succeed." pod=<_> secret="" err="secret \"dockerhub\" not found"`,
				`I0507 <_>    <_> kubelet_pods.go:906] "Unable to retrieve pull secret, the image pull may not succeed." pod=<_> secret="" err="secret \"gcr\" not found"`,
				`I0507 <_>    <_> prober.go:107] "Probe failed" probeType="Readiness" pod=<_> podUID=<_> containerName="grafana" probeResult="failure" output=<`,
				`IPv4: martian source <_> from <_> on dev eth0`,
				`PRC: Renewing lease on eth0.`,
				`RCV: Reply message on eth0 from fe80::e9:7eff:fedf:3d37.`,
				`Removed slice libcontainer container kubepods-burstable-pod25cb986c_3d6c_4ed0_abf3_ee59ed6175f9.slice.`,
				`Started libcontainer container <_>`,
				`XMT: Renew on eth0, interval 9700ms.`,
				`XMT: Solicit on eth0, interval <_>`,
				`audit:type=1400 <_> apparmor="DENIED" operation="ptrace" profile="cri-containerd.apparmor.d" pid=<_> comm="pidof" requested_mask="read" denied_mask="read" peer="unconfined"`,
				`kauditd_printk_skb: <_> callbacks suppressed`,
				`ll header: 00000000: 42 01 0a 80 00 <_> 42 01 0a 80 00 01 08 00`,
				`net_ratelimit: 2 callbacks suppressed`,
				`time="2024-05-07T11:59:32.755926053Z" level=info msg="CreateContainer within sandbox \"81e019a0248a0300a328fd59f9939c3eaa1b98aa7f325a7f6e00592633275ef6\" for container &ContainerMetadata{Name:checkoutservice,Attempt:3417,}"`,
				`time="2024-05-07T11:59:34.519591759Z" level=info msg="StopContainer for \"c91436db00920ec961b9d5d6b4859d80a912e862e34fb5c45d8a85684fe6a97e\" with timeout 30 (s)"`,
				`time="2024-05-07T11:59:34.520032214Z" level=info msg="Stop container \"c91436db00920ec961b9d5d6b4859d80a912e862e34fb5c45d8a85684fe6a97e\" with signal terminated"`,
				`time="2024-05-07T11:59:34.591282703Z" level=info msg="StopContainer for \"c91436db00920ec961b9d5d6b4859d80a912e862e34fb5c45d8a85684fe6a97e\" returns successfully"`,
				`time="2024-05-07T11:59:34.592005066Z" level=info msg="StopPodSandbox for \"c605ad2cdc74c6b5288f2532ad71cce81a28ef6965f97a89ff6609deb825553a\""`,
				`time="2024-05-07T11:59:34.592084495Z" level=info msg="Container to stop \"c91436db00920ec961b9d5d6b4859d80a912e862e34fb5c45d8a85684fe6a97e\" must be in running or unknown state, current state \"CONTAINER_EXITED\""`,
				`time="2024-05-07T11:59:34.706960850Z" level=info msg="TearDown network for sandbox \"c605ad2cdc74c6b5288f2532ad71cce81a28ef6965f97a89ff6609deb825553a\" successfully"`,
				`time="2024-05-07T11:59:34.707025668Z" level=info msg="StopPodSandbox for \"c605ad2cdc74c6b5288f2532ad71cce81a28ef6965f97a89ff6609deb825553a\" returns successfully"`,
				`time="2024-05-07T11:59:36.177858616Z" level=info msg="CreateContainer within sandbox \"81e019a0248a0300a328fd59f9939c3eaa1b98aa7f325a7f6e00592633275ef6\" for &ContainerMetadata{Name:checkoutservice,Attempt:3417,} returns container id \"95bf586cd79d43120ff44582d4dbd2476de61744411f8515b9b2c527a41fd5d9\""`,
				`time="2024-05-07T11:59:38.484586527Z" level=error msg="Failed to delete exec process \"d9e0a1867ce73695ad859f2b0a76fe8f5053db8a5e49142d747e53a445729bd4\" for container \"6ad3e55547f2192f865518e75009243418b177091c1c781236e2ac8f0324b408\"" error="ttrpc:closed:unknown"`,
				`time="2024-05-07T11:59:43.941729092Z" level=info msg="CreateContainer within sandbox \"ee9dc07bca79ef7dffe2a6eb326e27236e9e97c35913c7aae16ee0a62632fc25\" for container &ContainerMetadata{Name:cortex-gw,Attempt:1660,}"`,
				`time="2024-05-07T11:59:43.954289531Z" level=info msg="CreateContainer within sandbox \"ee9dc07bca79ef7dffe2a6eb326e27236e9e97c35913c7aae16ee0a62632fc25\" for &ContainerMetadata{Name:cortex-gw,Attempt:1660,} returns container id \"93fa5decd62691912f90c9b27526f5e00183239bfa4d3f4ea8578a7873b9c2b4\""`,
				`time="2024-05-07T11:59:<_> level=error msg="ExecSync for <_> failed" error="rpc error: code =NotFound desc =failed to exec in container: failed to load task: no running task found: task <_> not found: not found"`,
				`time="2024-05-07T11:59:<_> level=error msg="PullImage \"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_> failed" error="failed to pull and unpack image \"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>  failed to resolve reference \"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>  unexpected status from HEAD request to https:<_>  403 Forbidden"`,
				`time="2024-05-07T11:59:<_> level=error msg="PullImage \"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_> failed" error="rpc error: code =NotFound desc =failed to pull and unpack image \"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>  failed to resolve reference \"us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>  us.gcr.io/hosted-grafana/hosted-grafana-pro:<_>  not found"`,
				`time="2024-05-07T11:59:<_> level=info msg="CreateContainer within sandbox <_> for &ContainerMetadata{Name:grafana,Attempt:<_> returns container id <_>`,
				`time="2024-05-07T11:59:<_> level=info msg="CreateContainer within sandbox <_> for &ContainerMetadata{Name:hgrun,Attempt:0,} returns container id <_>`,
				`time="2024-05-07T11:59:<_> level=info msg="CreateContainer within sandbox <_> for container &ContainerMetadata{Name:grafana,Attempt:<_>`,
				`time="2024-05-07T11:59:<_> level=info msg="CreateContainer within sandbox <_> for container &ContainerMetadata{Name:hgrun,Attempt:0,}"`,
				`time="2024-05-07T11:59:<_> level=info msg="ImageCreate event name:<_> <_> labels:{key:\"io.cri-containerd.image\" value:\"managed\"}"`,
				`time="2024-05-07T11:59:<_> level=info msg="ImageUpdate event name:<_> <_> labels:{key:\"io.cri-containerd.image\" value:\"managed\"}"`,
				`time="2024-05-07T11:59:<_> level=info msg="PullImage \"us.gcr.io/hosted-grafana/hgrun:0.1.452\" returns image reference \"sha256:9fb1bce3e4a228f50768d21842cd7d7fafc1d586eaa0326c9d3c86d79a36868a\""`,
				`time="2024-05-07T11:59:<_> level=info msg="PullImage \"us.gcr.io/hosted-grafana/hosted-grafana-pro:11.1.0-70397\" returns image reference \"sha256:0036b00b52fc547c944c1c820817d91fba6e20775cbf4e6c3e09ad2e682dbd73\""`,
				`time="2024-05-07T11:59:<_> level=info msg="Pulled image \"us.gcr.io/hosted-grafana/hgrun:0.1.452\" with image id \"sha256:9fb1bce3e4a228f50768d21842cd7d7fafc1d586eaa0326c9d3c86d79a36868a\", repo tag \"us.gcr.io/hosted-grafana/hgrun:0.1.452\", repo digest \"us.gcr.io/hosted-grafana/hgrun@sha256:b492dbbbee9faf9dba63c9fd89e6f9e148239765454c6a54c4284a2828dec153\", size \"19109699\" in <_>`,
				`time="2024-05-07T11:59:<_> level=info msg="Pulled image \"us.gcr.io/hosted-grafana/hosted-grafana-pro:11.1.0-70397\" with image id \"sha256:0036b00b52fc547c944c1c820817d91fba6e20775cbf4e6c3e09ad2e682dbd73\", repo tag \"us.gcr.io/hosted-grafana/hosted-grafana-pro:11.1.0-70397\", repo digest \"us.gcr.io/hosted-grafana/hosted-grafana-pro@sha256:0853965a142fb95648de3281a7c71de0d05fb51616bc32b523dc2f1da6ca06dc\", size \"173405048\" in <_>`,
				`time=<_> level=error msg="ContainerStatus for <_> failed" error="rpc error:code = NotFound desc = an error occurred when try to find container <_> not found"`,
				`time=<_> level=info msg="PullImage <_>`,
				`time=<_> level=info msg="RemoveContainer for <_>`,
				`time=<_> level=info msg="RemoveContainer for <_> returns successfully"`,
				`time=<_> level=info msg="StartContainer for <_>`,
				`time=<_> level=info msg="StartContainer for <_> returns successfully"`,
				`time=<_> level=info msg="cleaning up dead shim" namespace=k8s.io`,
				`time=<_> level=info msg="shim disconnected" id=<_> namespace=k8s.io`,
				`time=<_> level=info msg="stop pulling image <_> active requests=0, bytes read=<_>`,
				`time=<_> level=info msg="trying next host - response was http.StatusNotFound" host=us.gcr.io`,
				`time=<_> level=warning msg="cleaning up after shim disconnected" id=<_> namespace=k8s.io`,
			},
		},
		{
			drain:     New(DefaultConfig(), nil),
			inputFile: "testdata/kafka.txt",
			patterns: []string{
				`[2024-05-07 10:55:40,626] INFO [LocalLog partition=ingest-6, dir=/bitnami/kafka/data] Deleting segment files LogSegment(baseOffset=180391157, size=16991045, lastModifiedTime=1715075754780, largestRecordTimestamp=Some(1715075754774)),LogSegment(baseOffset=180393429, size=16997692, lastModifiedTime=1715075760206, largestRecordTimestamp=Some(1715075760186)),LogSegment(baseOffset=180395889, size=16998200, lastModifiedTime=1715075765542, largestRecordTimestamp=Some(1715075765526)),LogSegment(baseOffset=180398373, size=16977347, lastModifiedTime=1715075770515, largestRecordTimestamp=Some(1715075770504)) (kafka.log.LocalLog$)`,
				`[2024-05-07 10:55:40,638] INFO [LocalLog partition=ingest-6, dir=/bitnami/kafka/data] Deleting segment files LogSegment(baseOffset=180400817, size=16997594, lastModifiedTime=1715075775780, largestRecordTimestamp=Some(1715075775771)),LogSegment(baseOffset=180403261, size=16992344, lastModifiedTime=1715075781053, largestRecordTimestamp=Some(1715075781021)),LogSegment(baseOffset=180405723, size=16989895, lastModifiedTime=1715075786205, largestRecordTimestamp=Some(1715075786174)),LogSegment(baseOffset=180408118, size=16998698, lastModifiedTime=1715075791681, largestRecordTimestamp=Some(1715075791673)),LogSegment(baseOffset=180410608, size=16995676, lastModifiedTime=1715075796438, largestRecordTimestamp=Some(1715075796430)),LogSegment(baseOffset=180412733, size=16963278, lastModifiedTime=1715075800534, largestRecordTimestamp=Some(1715075800511)),LogSegment(baseOffset=180414883, size=16984328, lastModifiedTime=1715075805272, largestRecordTimestamp=Some(1715075805230)),LogSegment(baseOffset=180417063, size=16989109, lastModifiedTime=1715075810381, largestRecordTimestamp=Some(1715075810372)),LogSegment(baseOffset=180419267, size=16996871, lastModifiedTime=1715075815153, largestRecordTimestamp=Some(1715075815125)),LogSegment(baseOffset=180421560, size=16988558, lastModifiedTime=1715075819785, largestRecordTimestamp=Some(1715075819763)),LogSegment(baseOffset=180424008, size=16999292, lastModifiedTime=1715075825336, largestRecordTimestamp=Some(1715075825303)),LogSegment(baseOffset=180426459, size=16990595, lastModifiedTime=1715075830839, largestRecordTimestamp=Some(1715075830827)),LogSegment(baseOffset=180428944, size=16995859, lastModifiedTime=1715075835942, largestRecordTimestamp=Some(1715075835904)),LogSegment(baseOffset=180431327, size=16992294, lastModifiedTime=1715075841219, largestRecordTimestamp=Some(1715075841214)),LogSegment(baseOffset=180433867, size=16966736, lastModifiedTime=1715075846443, largestRecordTimestamp=Some(1715075846401)),LogSegment(baseOffset=180436204, size=16894731, lastModifiedTime=1715075853273, largestRecordTimestamp=Some(1715075853244)),LogSegment(baseOffset=180438984, size=16983529, lastModifiedTime=1715075858911, largestRecordTimestamp=Some(1715075858891)),LogSegment(baseOffset=180441466, size=16996933, lastModifiedTime=1715075863566, largestRecordTimestamp=Some(1715075863554)),LogSegment(baseOffset=180443778, size=16999841, lastModifiedTime=1715075866199, largestRecordTimestamp=Some(1715075866185)),LogSegment(baseOffset=180445367, size=16992471, lastModifiedTime=1715075870385, largestRecordTimestamp=Some(1715075870347)),LogSegment(baseOffset=180447366, size=16999996, lastModifiedTime=1715075875102, largestRecordTimestamp=Some(1715075875091)),LogSegment(baseOffset=180449601, size=16994426, lastModifiedTime=1715075879927, largestRecordTimestamp=Some(1715075879926)),LogSegment(baseOffset=180452079, size=16998020, lastModifiedTime=1715075885293, largestRecordTimestamp=Some(1715075885263)),LogSegment(baseOffset=180454546, size=16992231, lastModifiedTime=1715075890424, largestRecordTimestamp=Some(1715075890409)),LogSegment(baseOffset=180456986, size=16970315, lastModifiedTime=1715075895719, largestRecordTimestamp=Some(1715075895690)),LogSegment(baseOffset=180459366, size=16990785, lastModifiedTime=1715075900996, largestRecordTimestamp=Some(1715075900985)),LogSegment(baseOffset=180461885, size=16996655, lastModifiedTime=1715075905847, largestRecordTimestamp=Some(1715075905841)),LogSegment(baseOffset=180464299, size=16982181, lastModifiedTime=1715075911052, largestRecordTimestamp=Some(1715075911028)),LogSegment(baseOffset=180466821, size=16997630, lastModifiedTime=1715075915962, largestRecordTimestamp=Some(1715075915953)),LogSegment(baseOffset=180468968, size=16995723, lastModifiedTime=1715075920325, largestRecordTimestamp=Some(1715075920308)),LogSegment(baseOffset=180471046, size=16979316, lastModifiedTime=1715075924724, largestRecordTimestamp=Some(1715075924697)),LogSegment(baseOffset=180473259, size=16995238, lastModifiedTime=1715075929645, largestRecordTimestamp=Some(1715075929624)),LogSegment(baseOffset=180475486, size=16988461, lastModifiedTime=1715075934288, largestRecordTimestamp=Some(1715075934283)),LogSegment(baseOffset=180477735, size=16993767, lastModifiedTime=1715075939277, largestRecordTimestamp=Some(1715075939270)),LogSegment(baseOffset=180480095, size=16995409, lastModifiedTime=1715075944639, largestRecordTimestamp=Some(1715075944635)),LogSegment(baseOffset=180482560, size=16992784, lastModifiedTime=1715075949760, largestRecordTimestamp=Some(1715075949760)),LogSegment(baseOffset=180484967, size=16990838, lastModifiedTime=1715075954937, largestRecordTimestamp=Some(1715075954929)),LogSegment(baseOffset=180487377, size=16976794, lastModifiedTime=1715075960151, largestRecordTimestamp=Some(1715075960119)),LogSegment(baseOffset=180489919, size=16997379, lastModifiedTime=1715075965116, largestRecordTimestamp=Some(1715075965085)),LogSegment(baseOffset=180492304, size=16956613, lastModifiedTime=1715075970448, largestRecordTimestamp=Some(1715075970424)),LogSegment(baseOffset=180494832, size=16895640, lastModifiedTime=1715075975354, largestRecordTimestamp=Some(1715075975341)),LogSegment(baseOffset=180496930, size=16998328, lastModifiedTime=1715075979813, largestRecordTimestamp=Some(1715075979796)),LogSegment(baseOffset=180499079, size=16995699, lastModifiedTime=1715075984309, largestRecordTimestamp=Some(1715075984285)),LogSegment(baseOffset=180501183, size=16993785, lastModifiedTime=1715075989086, largestRecordTimestamp=Some(1715075989064)),LogSegment(baseOffset=180503431, size=16989600, lastModifiedTime=1715075993713, largestRecordTimestamp=Some(1715075993683)),LogSegment(baseOffset=180505674, size=16984790, lastModifiedTime=1715075998337, largestRecordTimestamp=Some(1715075998318)),LogSegment(baseOffset=180508022, size=16982630, lastModifiedTime=1715076003671, largestRecordTimestamp=Some(1715076003660)),LogSegment(baseOffset=180510439, size=16999488, lastModifiedTime=1715076009000, largestRecordTimestamp=Some(1715076008996)),LogSegment(baseOffset=180512848, size=16997845, lastModifiedTime=1715076014033, largestRecordTimestamp=Some(1715076014032)),LogSegment(baseOffset=180515281, size=16990661, lastModifiedTime=1715076019245, largestRecordTimestamp=Some(1715076019216)),LogSegment(baseOffset=180517815, size=16996244, lastModifiedTime=1715076023989, largestRecordTimestamp=Some(1715076023963)),LogSegment(baseOffset=180520112, size=16992012, lastModifiedTime=1715076029243, largestRecordTimestamp=Some(1715076029231)) (kafka.log.LocalLog$)`,
				`[2024-05-07 10:55:53,038] INFO [LocalLog partition=mimir-dev-09-aggregations-offsets-1, dir=/bitnami/kafka/data] Deleting segment files LogSegment(baseOffset=447957, size=948, lastModifiedTime=1715059232052, largestRecordTimestamp=Some(1715059232002)),LogSegment(baseOffset=447969, size=948, lastModifiedTime=1715059424352, largestRecordTimestamp=Some(1715059424301)) (kafka.log.LocalLog$)`,
				`[2024-05-07 10:55:<_> INFO Deleted log <_> (kafka.log.LogSegment)`,
				`[2024-05-07 10:55:<_> INFO Deleted offset index <_> (kafka.log.LogSegment)`,
				`[2024-05-07 10:55:<_> INFO Deleted producer state snapshot <_> (kafka.log.SnapshotFile)`,
				`[2024-05-07 10:55:<_> INFO Deleted time index <_> (kafka.log.LogSegment)`,
				`[2024-05-07 10:55:<_> INFO [ProducerStateManager <_> Wrote producer snapshot at offset <_> with 0 producer ids in <_> ms. (kafka.log.ProducerStateManager)`,
				`[2024-05-07 <_> INFO [LocalLog partition=<_> dir=/bitnami/kafka/data] Deleting segment files LogSegment(baseOffset=<_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> (kafka.log.LocalLog$)`,
				`[2024-05-07 <_> INFO [LocalLog partition=<_> dir=/bitnami/kafka/data] Rolled new log segment at offset <_> in <_> ms. (kafka.log.LocalLog)`,
				`[2024-05-07 <_> INFO [LocalLog partition=mimir-dev-09-aggregations-offsets-0, dir=/bitnami/kafka/data] Deleting segment files LogSegment(baseOffset=<_> size=948, lastModifiedTime=<_> largestRecordTimestamp=<_> (kafka.log.LocalLog$)`,
				`[2024-05-07 <_> INFO [UnifiedLog partition=<_> dir=/bitnami/kafka/data] Deleting segment LogSegment(baseOffset=<_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> due to retention size <_> breach. Log size after deletion will be <_> (kafka.log.UnifiedLog)`,
				`[2024-05-07 <_> INFO [UnifiedLog partition=<_> dir=/bitnami/kafka/data] Deleting segments due to log start offset <_> breach:LogSegment(baseOffset=<_> size=948, lastModifiedTime=<_> largestRecordTimestamp=<_> <_> size=948, lastModifiedTime=<_> largestRecordTimestamp=<_> (kafka.log.UnifiedLog)`,
				`[2024-05-07 <_> INFO [UnifiedLog partition=<_> dir=/bitnami/kafka/data] Deleting segments due to log start offset <_> breach:LogSegment(baseOffset=<_> size=<_> lastModifiedTime=<_> largestRecordTimestamp=<_> (kafka.log.UnifiedLog)`,
				`[2024-05-07 <_> INFO [UnifiedLog partition=<_> dir=/bitnami/kafka/data] Incremented log start offset to <_> due to leader offset increment (kafka.log.UnifiedLog)`,
				`[2024-05-07 <_> INFO [UnifiedLog partition=<_> dir=/bitnami/kafka/data] Incremented log start offset to <_> due to segment deletion (kafka.log.UnifiedLog)`,
			},
		},
		{
			drain:     New(DefaultConfig(), nil),
			inputFile: "testdata/kubernetes.txt",
			patterns: []string{
				`I0507 12:02:27.947830       1 nodeutilization.go:274] "Evicting pods based on priority, if they have same priority, they'll be evicted based on QoS tiers"`,
				`I0507 12:02:<_>       1 defaultevictor.go:163] "pod does not fit on any other node because of nodeSelector(s), Taint(s), or nodes marked as unschedulable" <_>`,
				`I0507 12:02:<_>       1 defaultevictor.go:202] "Pod fails the following checks" <_> checks="[pod is a DaemonSet pod, pod has local storage and descheduler is not configured with evictLocalStoragePods]"`,
				`I0507 12:02:<_>       1 defaultevictor.go:202] "Pod fails the following checks" <_> checks="pod has local storage and descheduler is not configured with evictLocalStoragePods"`,
				`I0507 12:02:<_>       1 defaultevictor.go:202] "Pod fails the following checks" <_> checks="pod is a DaemonSet pod"`,
				`I0507 12:02:<_>       1 node.go:157] "Pod does not fit on any other node" pod:<_> node:<_> error:="[pod node selector does not match the node label, insufficient <_>`,
				`I0507 12:02:<_>       1 node.go:157] "Pod does not fit on any other node" pod:<_> node:<_> error:="[pod node selector does not match the node label, insufficient <_> insufficient <_>`,
				`I0507 12:02:<_>       1 node.go:157] "Pod does not fit on any other node" pod:<_> node:<_> error:="[pod node selector does not match the node label, insufficient <_> insufficient <_> insufficient pods]"`,
				`I0507 12:02:<_>       1 node.go:157] "Pod does not fit on any other node" pod:<_> node:<_> error:="[pod node selector does not match the node label, pod does not tolerate taints on the node, insufficient <_>`,
				`I0507 12:02:<_>       1 node.go:157] "Pod does not fit on any other node" pod:<_> node:<_> error:="[pod node selector does not match the node label, pod does not tolerate taints on the node, insufficient <_> insufficient <_>`,
				`I0507 12:02:<_>       1 node.go:157] "Pod does not fit on any other node" pod:<_> node:<_> error:="[pod node selector does not match the node label, pod does not tolerate taints on the node]"`,
				`I0507 12:02:<_>       1 node.go:157] "Pod does not fit on any other node" pod:<_> node:<_> error:="insufficient cpu"`,
				`I0507 12:02:<_>       1 node.go:157] "Pod does not fit on any other node" pod:="loki-dev-005/querier-burst-6b5f6db455-5zvkm" node:<_> error:="[insufficient <_> insufficient <_>`,
				`I0507 12:02:<_>       1 node.go:157] "Pod does not fit on any other node" pod:="loki-dev-005/querier-burst-6b5f6db455-5zvkm" node:<_> error:="pod node selector does not match the node label"`,
				`I0507 12:02:<_>       1 node.go:339] "no Pod antiaffinity rule found" <_>`,
				`I0507 12:04:17.595169       1 descheduler.go:155] Building a pod evictor`,
				`I0507 12:04:17.596431       1 nodeutilization.go:204] "Node is underutilized" node="gke-dev-eu-west-3-main-n2s8-1-1dd39c-d1c92061-4z2l" usage={"cpu":"984m","memory":"611Mi","pods":"16"} usagePercentage={"cpu":12.44,"memory":2.15,"pods":25}`,
				`I0507 12:04:17.596484       1 highnodeutilization.go:107] "Criteria for a node below target utilization" CPU=50 Mem=50 Pods=100`,
				`I0507 12:04:17.596504       1 highnodeutilization.go:108] "Number of underutilized nodes" totalNumber=1`,
				`I0507 12:04:17.596528       1 nodeutilization.go:260] "Total capacity to be moved" CPU=5060 Mem=112216292800 Pods=163`,
				`I0507 12:04:17.596651       1 defaultevictor.go:202] "Pod fails the following checks" pod="kube-system/metrics-server-v0.6.3-68f5b7c4d5-t5mz8" checks="[pod has system critical priority, pod has higher priority than specified priority class threshold, pod has local storage and descheduler is not configured with evictLocalStoragePods]"`,
				`I0507 12:04:17.596803       1 defaultevictor.go:202] "Pod fails the following checks" pod="gadget/gadget-zjjts" checks="[pod is a DaemonSet pod, pod has local storage and descheduler is not configured with evictLocalStoragePods]"`,
				`I0507 12:04:<_>       1 nodeutilization.go:207] "Node is overutilized" <_> usage={"cpu":<_> <_> <_> usagePercentage={"cpu":<_> <_> <_>`,
				`I0507 12:<_> <_>       1 defaultevictor.go:202] "Pod fails the following checks" <_> checks="[pod has system critical priority, pod has higher priority than specified priority class threshold]"`,
				`I0507 12:<_> <_>       1 defaultevictor.go:202] "Pod fails the following checks" <_> checks="[pod is a DaemonSet pod, pod has higher priority than specified priority class threshold, pod has local storage and descheduler is not configured with evictLocalStoragePods]"`,
				`I0507 12:<_> <_>       1 defaultevictor.go:202] "Pod fails the following checks" <_> checks="[pod is a DaemonSet pod, pod has higher priority than specified priority class threshold]"`,
				`I0507 12:<_> <_>       1 defaultevictor.go:202] "Pod fails the following checks" <_> checks="[pod is a DaemonSet pod, pod has system critical priority, pod has higher priority than specified priority class threshold, pod has local storage and descheduler is not configured with evictLocalStoragePods]"`,
				`I0507 12:<_> <_>       1 defaultevictor.go:202] "Pod fails the following checks" <_> checks="[pod is a DaemonSet pod, pod has system critical priority, pod has higher priority than specified priority class threshold]"`,
				`I0507 12:<_> <_>       1 defaultevictor.go:202] "Pod fails the following checks" <_> checks="[pod is a mirror pod, pod is a static pod, pod has system critical priority, pod has higher priority than specified priority class threshold, pod has local storage and descheduler is not configured with evictLocalStoragePods]"`,
				`I0507 12:<_> <_>       1 descheduler.go:<_> "Number of evicted pods" <_>`,
				`I0507 12:<_> <_>       1 nodeutilization.go:<_> "Evicting pods from node" <_> usage={"cpu":<_> <_> <_>`,
				`I0507 12:<_> <_>       1 nodeutilization.go:<_> "No removable pods on node, try next node" <_>`,
				`I0507 12:<_> <_>       1 profile.go:<_> "Total number of pods evicted" extension point="Balance" <_>`,
				`I0507 12:<_> <_>       1 reflector.go:<_> k8s.io/client-go/informers/factory.go:<_>  Watch close - <_> total <_> items received`,
				`I0507 <_>       1 <_> "Pods on node" node=<_> allPods=<_> nonRemovablePods=<_> removablePods=<_>`,
			},
		},
		{
			drain:     New(DefaultConfig(), nil),
			inputFile: "testdata/vault.txt",
			patterns: []string{
				`2024-05-07T10:<_> <_> [INFO]  expiration: revoked lease: <_>`,
			},
		},
		{
			drain:     New(DefaultConfig(), nil),
			inputFile: "testdata/calico.txt",
			patterns: []string{
				`2024-05-08 15:23:56.403 [DEBUG][615489] felix/table.go 699: Finished loading iptables state ipVersion=0x4 table="filter"`,
				`2024-05-08 15:23:56.614 [DEBUG][76] felix/int_dataplane.go 1777: Refreshing routes`,
				`2024-05-08 15:23:56.615 [DEBUG][76] felix/route_rule.go 179: Queueing a resync of routing rules. ipVersion=4`,
				`2024-05-08 15:23:56.615 [DEBUG][76] felix/route_table.go 480:Queueing a resync of routing table. ifaceRegex="^azv.*" ipVersion=0x4 tableIndex=0`,
				`2024-05-08 15:23:56.615 [DEBUG][76] felix/route_table.go 480:Queueing a resync of routing table. ifaceRegex="^wireguard.cali$" ipVersion=0x4 tableIndex=1`,
				`2024-05-08 15:23:56.615 [DEBUG][76] felix/route_table.go 533: Check interfaces matching regex`,
				`2024-05-08 15:23:56.615 [DEBUG][76] felix/wireguard.go 605: Queueing a resync of wireguard configuration ipVersion=0x4`,
				`2024-05-08 15:23:56.615 [DEBUG][76] felix/wireguard.go 654: Wireguard is not in-sync - verifying wireguard configuration is removed ipVersion=0x4`,
				`2024-05-08 15:23:56.617 [DEBUG][76] felix/wireguard.go 1503: Wireguard is disabled and does not exist ifaceName="wireguard.cali" ipVersion=0x4`,
				`2024-05-08 15:23:56.619 [DEBUG][76] felix/route_table.go 584: Flag no OIF for full re-sync`,
				`2024-05-08 15:23:56.619 [DEBUG][76] felix/route_table.go 614:Synchronised routes on interface ifaceName="*NoOIF*" ifaceRegex="^wireguard.cali$" ipVersion=0x4 tableIndex=1`,
				`2024-05-08 15:23:56.619 [DEBUG][76] felix/route_table.go 661:Syncing interface routes ifaceName="*NoOIF*" ifaceRegex="^wireguard.cali$" ipVersion=0x4 tableIndex=1`,
				`2024-05-08 15:23:56.619 [DEBUG][76] felix/route_table.go 686:Reconcile against kernel programming ifaceName="*NoOIF*" ifaceRegex="^wireguard.cali$" ipVersion=0x4 tableIndex=1`,
				`2024-05-08 15:23:57.942 [WARNING][56] felix/table.go 654:Detected out-of-sync inserts, marking for resync actualRuleIDs=[]string{"", "", "", "", "", "", "", "", "", "", "", "", "tVnHkvAo15HuiPy0", "", ""} chainName="OUTPUT" expectedRuleIDs=[]string{"tVnHkvAo15HuiPy0", "", "", "", "", "", "", "", "", "", "", "", "", "", ""} ipVersion=0x4 table="raw"`,
				`2024-05-08 15:23:57.942 [WARNING][56] felix/table.go 654:Detected out-of-sync inserts, marking for resync actualRuleIDs=[]string{"", "", "", "", "6gwbT8clXdHdC1b1"} chainName="PREROUTING" expectedRuleIDs=[]string{"6gwbT8clXdHdC1b1", "", "", "", ""} ipVersion=0x4 table="raw"`,
				`2024-05-08 15:23:57.969 [WARNING][56] felix/table.go 654:Detected out-of-sync inserts, marking for resync actualRuleIDs=[]string{"", "", "", "", "Cz_u1IQiXIMmKD4c", "", "", "", "", "", "", "", "", "", "", "", ""} chainName="INPUT" expectedRuleIDs=[]string{"Cz_u1IQiXIMmKD4c", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", ""} ipVersion=0x4 table="filter"`,
				`2024-05-08 15:23:57.969 [WARNING][56] felix/table.go 654:Detected out-of-sync inserts, marking for resync actualRuleIDs=[]string{"", "", "", "", "tVnHkvAo15HuiPy0", "", "", "", "", ""} chainName="OUTPUT" expectedRuleIDs=[]string{"tVnHkvAo15HuiPy0", "", "", "", "", "", "", "", "", ""} ipVersion=0x4 table="filter"`,
				`2024-05-08 15:23:58.566 [DEBUG][3576126] felix/int_dataplane.go 957: Examining link for MTU calculation mtu=1500 name="eth0"`,
				`2024-05-08 15:23:58.680 [DEBUG][216945] felix/int_dataplane.go 1785: Reschedule kick received`,
				`2024-05-08 15:23:58.681 [DEBUG][216945] felix/feature_detect.go 112: Refreshing detected iptables features`,
				`2024-05-08 15:23:58.681 [DEBUG][216945] felix/table.go 944:Invalidating dataplane cache ipVersion=0x4 reason="refresh timer" table="nat"`,
				`2024-05-08 15:23:58.684 [DEBUG][216945] felix/feature_detect.go 242: Ran iptables --version rawVersion="iptables v1.8.4 (legacy)\n"`,
				`2024-05-08 15:23:58.684 [DEBUG][216945] felix/feature_detect.go 255: Parsed iptables version version=1.8.4`,
				`2024-05-08 15:23:58.684 [DEBUG][216945] felix/table.go 604: Loading current iptables state and checking it is correct. ipVersion=0x4 table="nat"`,
				`2024-05-08 15:23:58.684 [DEBUG][216945] felix/versionparse.go 110: Raw kernel version rawVersion="Linux version 5.15.0-1057-azure (buildd@lcy02-amd64-033) (gcc (Ubuntu 11.4.0-1ubuntu1~22.04) 11.4.0, GNU ld (GNU Binutils for Ubuntu) 2.38) #65-Ubuntu SMP Fri Feb 9 18:39:24 UTC 2024\n"`,
				`2024-05-08 15:23:58.684 [DEBUG][216945] felix/versionparse.go 118: Parsed kernel version version=5.15.0-1057`,
				`2024-05-08 15:23:58.715 [DEBUG][216945] felix/table.go 851: Parsing line ipVersion=0x4 line="# Generated by iptables-nft-save v1.8.4 on Wed May  8 15:23:58 2024" table="nat"`,
				`2024-05-08 15:23:58.716 [DEBUG][216945] felix/table.go 851:Parsing line ipVersion=0x4 line="*nat" table="nat"`,
				`2024-05-08 15:23:58.716 [DEBUG][216945] felix/table.go 881: Not an append, skipping ipVersion=0x4 line="# Generated by iptables-nft-save v1.8.4 on Wed May  8 15:23:58 2024" table="nat"`,
				`2024-05-08 15:23:58.716 [DEBUG][216945] felix/table.go 881:Not an append, skipping ipVersion=0x4 line="*nat" table="nat"`,
				`2024-05-08 15:23:58.717 [DEBUG][216945] felix/table.go 851: Parsing line ipVersion=0x4 line=":POSTROUTING ACCEPT [0:0]" table="nat"`,
				`2024-05-08 15:23:58.717 [DEBUG][216945] felix/table.go 870: Found forward-reference chainName="POSTROUTING" ipVersion=0x4 line=":POSTROUTING ACCEPT [0:0]" table="nat"`,
				`2024-05-08 15:23:58.718 [DEBUG][216945] felix/table.go 851: Parsing line ipVersion=0x4 line=":OUTPUT ACCEPT [0:0]" table="nat"`,
				`2024-05-08 15:23:58.718 [DEBUG][216945] felix/table.go 851: Parsing line ipVersion=0x4 line=":PREROUTING ACCEPT [0:0]" table="nat"`,
				`2024-05-08 15:23:58.718 [DEBUG][216945] felix/table.go 870: Found forward-reference chainName="OUTPUT" ipVersion=0x4 line=":OUTPUT ACCEPT [0:0]" table="nat"`,
				`2024-05-08 15:23:58.718 [DEBUG][216945] felix/table.go 870: Found forward-reference chainName="PREROUTING" ipVersion=0x4 line=":PREROUTING ACCEPT [0:0]" table="nat"`,
				`2024-05-08 15:23:<_> <_> felix/endpoint_mgr.go 443: Reporting endpoint status. dirtyEndpoints=set.Set{}`,
				`2024-05-08 15:23:<_> <_> felix/health.go 167: Health: <_>`,
				`2024-05-08 15:23:<_> <_> felix/health.go 196: Checking state of reporter reporter=&health.reporterState{name:"async_calc_graph", reports:health.HealthReport{Live:true, Ready:true, Detail:""}, timeout:20000000000, latest:health.HealthReport{Live:true, Ready:true, Detail:""}, timestamp:time.Time{wall:<_> ext:<_> loc:(*time.Location)(0x4ce3aa0)}}`,
				`2024-05-08 15:23:<_> <_> felix/health.go 196: Checking state of reporter reporter=&health.reporterState{name:"felix-startup", reports:health.HealthReport{Live:true, Ready:true, Detail:""}, timeout:0, latest:health.HealthReport{Live:true, Ready:true, Detail:""}, timestamp:time.Time{wall:<_> ext:<_> loc:(*time.Location)(0x4ce3aa0)}}`,
				`2024-05-08 15:23:<_> <_> felix/health.go 196: Checking state of reporter reporter=&health.reporterState{name:"int_dataplane", reports:health.HealthReport{Live:true, Ready:true, Detail:""}, timeout:90000000000, latest:health.HealthReport{Live:true, Ready:true, Detail:""}, timestamp:time.Time{wall:<_> ext:<_> loc:(*time.Location)(0x4ce3aa0)}}`,
				`2024-05-08 15:23:<_> <_> felix/health.go 245: Calculated health summary healthResult=&health.HealthReport{Live:true, Ready:true, Detail:"+------------------+---------+----------------+-----------------+--------+\n|    COMPONENT     | TIMEOUT |    LIVENESS    |    READINESS    | DETAIL |\n+------------------+---------+----------------+-----------------+--------+\n| async_calc_graph | 20s     | reporting live | reporting ready |        |\n| felix-startup    | 0s      | reporting live | reporting ready |        |\n| int_dataplane    | 1m30s   | reporting live | reporting ready |        |\n+------------------+---------+----------------+-----------------+--------+"}`,
				`2024-05-08 15:23:<_> <_> felix/health.go <_>  GET <_>`,
				`2024-05-08 15:23:<_> <_> felix/int_dataplane.go 1773: Refreshing IP sets state`,
				`2024-05-08 15:23:<_> <_> felix/int_dataplane.go 1807: Applying dataplane updates`,
				`2024-05-08 15:23:<_> <_> felix/int_dataplane.go 2080: Asked to reschedule. <_>`,
				`2024-05-08 15:23:<_> <_> felix/ipsets.go 234: Asked to resync with the dataplane on next update. family="inet"`,
				`2024-05-08 15:23:<_> <_> felix/ipsets.go 314: Resyncing ipsets with dataplane. family="inet"`,
				`2024-05-08 15:23:<_> <_> felix/ipsets.go 426: Parsing IP set. family="inet" <_>`,
				`2024-05-08 15:23:<_> <_> felix/ipsets.go 607: Skipping expected Calico IP set. family="inet" <_>`,
				`2024-05-08 15:23:<_> <_> felix/ipsets.go 643: No dirty IP sets. family="inet"`,
				`2024-05-08 15:23:<_> <_> felix/summary.go 100: Summarising <_> dataplane reconciliation loops over <_>  <_> <_> <_>`,
				`2024-05-08 15:23:<_> <_> felix/sync_client.go 347: Ping received from Typha connID=0x0 connection=&discovery.Typha{Addr:"", IP:"", NodeName:(*string)(nil)} type=""`,
				`2024-05-08 15:23:<_> <_> felix/sync_client.go 356: Pong sent to Typha connID=0x0 connection=&discovery.Typha{Addr:"", IP:"", NodeName:(*string)(nil)} type=""`,
				`2024-05-08 15:23:<_> <_> felix/sync_client.go 434: New message from Typha. connID=0x0 connection=&discovery.Typha{Addr:"", IP:"", NodeName:(*string)(nil)} envelope=syncproto.Envelope{Message:syncproto.MsgPing{Timestamp:time.Date(2024, time.May, 8, 15, 23, <_> <_> time.Local)}} type=""`,
				`2024-05-08 15:23:<_> <_> felix/table.go 1233: In nftables mode, restarting transaction between updates and deletions. ipVersion=0x4 <_>`,
				`2024-05-08 15:23:<_> <_> felix/table.go 1263: Update ended up being no-op, skipping call to ip(6)tables-restore. ipVersion=0x4 <_>`,
				`2024-05-08 15:23:<_> <_> felix/wireguard.go 652: Wireguard is not enabled, skipping sync ipVersion=0x4`,
				`2024-05-08 15:23:<_> <_> felix/xdp_state.go 1004: Updating ipsetIDsToMembers cache. family=4`,
				`2024-05-08 15:23:<_> <_> felix/xdp_state.go 1043: Processing pending diff state. cs=&intdataplane.xdpSystemState{IfaceNameToData:map[string]intdataplane.xdpIfaceData{}, XDPEligiblePolicies:map[proto.PolicyID]intdataplane.xdpRules{}} family=4`,
				`2024-05-08 15:23:<_> <_> felix/xdp_state.go 1270: Finished processing pending diff state. bpfActions=intdataplane.xdpBPFActions{CreateMap:set.Typed[string]{}, RemoveMap:set.Typed[string]{}, AddToMap:map[string]map[string]uint32{}, RemoveFromMap:map[string]map[string]uint32{}, InstallXDP:set.Typed[string]{}, UninstallXDP:set.Typed[string]{}, MembersToDrop:map[string]map[string]uint32{}, MembersToAdd:map[string]map[string]uint32{}} family=4 newCS=&intdataplane.xdpSystemState{IfaceNameToData:map[string]intdataplane.xdpIfaceData{}, XDPEligiblePolicies:map[proto.PolicyID]intdataplane.xdpRules{}}`,
				`2024-05-08 15:23:<_> <_> felix/xdp_state.go 1605: Getting member changes. family=4 oldMembers=map[string]set.Set[string]{}`,
				`2024-05-08 15:23:<_> <_> felix/xdp_state.go 1798: Processing BPF actions. family="ipv4"`,
				`2024-05-08 15:23:<_> <_> felix/xdp_state.go 1932: Finished processing BPF actions. family="ipv4"`,
				`2024-05-08 15:23:<_> <_> felix/xdp_state.go 968: Processing member updates. family=4`,
				`2024-05-08 15:23:<_> [DEBUG][216945] felix/table.go 851: Parsing line ipVersion=0x4 line=":<_> - [0:0]" table="nat"`,
				`2024-05-08 15:23:<_> [DEBUG][216945] felix/table.go 870: Found forward-reference <_> ipVersion=0x4 line=":<_> - [0:0]" table="nat"`,
				`2024-05-08 15:23:<_> [DEBUG][3576126] felix/int_dataplane.go 954: Skipping interface for MTU detection <_> <_>`,
				`2024-05-08 <_> <_> felix/ipsets.go 366:Finished IPSets resync family="inet" numInconsistenciesFound=0 resyncDuration=<_>`,
				`2024-05-08 <_> <_> felix/ipsets.go 467:Found member in dataplane canon=<_> family="inet" member=<_> setID="this-host"`,
				`2024-05-08 <_> <_> felix/ipsets.go 589:Whitelisting IP sets. ID="all-ipam-pools" family="inet" mainName="cali40all-ipam-pools"`,
				`2024-05-08 <_> <_> felix/ipsets.go 589:Whitelisting IP sets. ID="masq-ipam-pools" family="inet" mainName="cali40masq-ipam-pools"`,
				`2024-05-08 <_> <_> felix/ipsets.go 589:Whitelisting IP sets. ID="this-host" family="inet" mainName="cali40this-host"`,
				`2024-05-08 <_> [DEBUG][615489] felix/table.go 677:Skipping expected chain chainName=<_> ipVersion=0x4 table="filter"`,
				`2024-05-08 <_> [DEBUG][76] felix/route_table.go 557:Resync:found calico-owned interface ifaceName=<_> ifaceRegex="^azv.*" ipVersion=0x4 tableIndex=0`,
				`2024-05-08 <_> [DEBUG][76] felix/route_table.go 614:Synchronised routes on interface ifaceName=<_> ifaceRegex="^azv.*" ipVersion=0x4 tableIndex=0`,
				`2024-05-08 <_> [DEBUG][76] felix/route_table.go 661:Syncing interface routes ifaceName=<_> ifaceRegex="^azv.*" ipVersion=0x4 tableIndex=0`,
				`2024-05-08 <_> [DEBUG][76] felix/route_table.go 686:Reconcile against kernel programming ifaceName=<_> ifaceRegex="^azv.*" ipVersion=0x4 tableIndex=0`,
				`2024-05-08 <_> [DEBUG][76] felix/route_table.go 880:Processing route:254 <_> <_> ifaceName=<_> ifaceRegex="^azv.*" ipVersion=0x4 tableIndex=0`,
				`2024-05-08 <_> [DEBUG][76] felix/route_table.go 915:Route is correct dest=<_> ifaceName=<_> ifaceRegex="^azv.*" ipVersion=0x4 tableIndex=0`,
				`bird: Netlink: No route to host`,
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
				require.Truef(t, passes, `Line %q should match extracted pattern`, line)
			}
		})
	}

}
