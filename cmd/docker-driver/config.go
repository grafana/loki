package main

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/docker/docker/daemon/logger"
	"github.com/docker/docker/daemon/logger/templates"
	"github.com/grafana/loki/pkg/helpers"
	"github.com/grafana/loki/pkg/logentry/stages"
	"github.com/grafana/loki/pkg/promtail/client"
	"github.com/grafana/loki/pkg/promtail/targets"
	"github.com/prometheus/common/model"
)

const (
	driverName = "loki"

	cfgExternalLabelsKey = "loki-external-labels"
	cfgURLKey            = "loki-url"
	cfgTLSCAFileKey      = "loki-tls-ca-file"
	cfgTLSCertFileKey    = "loki-tls-cert-file"
	cfgTLSKeyFileKey     = "loki-tls-key-file"
	cfgTLSServerNameKey  = "loki-tls-server-name"
	cfgTLSInsecure       = "loki-tls-insecure-skip-verify"
	cfgProxyURLKey       = "loki-proxy-url"
	cfgTimeoutKey        = "loki-timeout"
	cfgBatchWaitKey      = "loki-batch-wait"
	cfgBatchSizeKey      = "loki-batch-size"
	cfgMinBackoffKey     = "loki-min-backoff"
	cfgMaxBackoffKey     = "loki-max-backoff"
	cfgMaxRetriesKey     = "loki-retries"
	cfgPipelineStagesKey = "loki-pipeline-stage-file"

	swarmServiceLabelKey = "com.docker.swarm.service.name"
	swarmStackLabelKey   = "com.docker.stack.namespace"

	swarmServiceLabelName = "swarm_service"
	swarmStackLabelName   = "swarm_stack"

	composeServiceLabelKey = "com.docker.compose.service"
	composeProjectLabelKey = "com.docker.compose.project"

	composeServiceLabelName = "compose_service"
	composeProjectLabelName = "compose_project"

	defaultExternalLabels = "container_name={{.Name}}"
	defaultHostLabelName  = model.LabelName("host")
)

var (
	defaultClientConfig = client.Config{
		BatchWait: 1 * time.Second,
		BatchSize: 100 * 1024,
		BackoffConfig: util.BackoffConfig{
			MinBackoff: 100 * time.Millisecond,
			MaxBackoff: 10 * time.Second,
			MaxRetries: 10,
		},
		Timeout: 10 * time.Second,
	}
)

type config struct {
	labels       model.LabelSet
	clientConfig client.Config
	pipeline     PipelineConfig
}

type PipelineConfig struct {
	PipelineStages stages.PipelineStages `yaml:"pipeline_stages,omitempty"`
}

func validateDriverOpt(loggerInfo logger.Info) error {
	config := loggerInfo.Config

	for opt := range config {
		switch opt {
		case cfgURLKey:
		case cfgExternalLabelsKey:
		case cfgTLSCAFileKey:
		case cfgTLSCertFileKey:
		case cfgTLSKeyFileKey:
		case cfgTLSServerNameKey:
		case cfgTLSInsecure:
		case cfgTimeoutKey:
		case cfgProxyURLKey:
		case cfgBatchWaitKey:
		case cfgBatchSizeKey:
		case cfgMinBackoffKey:
		case cfgMaxBackoffKey:
		case cfgMaxRetriesKey:
		case cfgPipelineStagesKey:
		case "labels":
		case "env":
		case "env-regex":
		case "max-size":
		case "max-file":
		default:
			return fmt.Errorf("%s: wrong log-opt: '%s' - %s", driverName, opt, loggerInfo.ContainerID)
		}
	}
	_, ok := config[cfgURLKey]
	if !ok {
		return fmt.Errorf("%s: %s is required in the config", driverName, cfgURLKey)
	}

	return nil
}

func parseConfig(logCtx logger.Info) (*config, error) {
	if err := validateDriverOpt(logCtx); err != nil {
		return nil, err
	}

	clientConfig := defaultClientConfig
	labels := model.LabelSet{}

	// parse URL
	rawURL, ok := logCtx.Config[cfgURLKey]
	if !ok {
		return nil, fmt.Errorf("%s: option %s is required", driverName, cfgURLKey)
	}
	url, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("%s: option %s is invalid %s", driverName, cfgURLKey, err)
	}
	clientConfig.URL = flagext.URLValue{URL: url}

	// parse timeout
	if err := parseDuration(cfgTimeoutKey, logCtx, func(d time.Duration) { clientConfig.Timeout = d }); err != nil {
		return nil, err
	}

	// parse batch wait and batch size
	if err := parseDuration(cfgBatchWaitKey, logCtx, func(d time.Duration) { clientConfig.BatchWait = d }); err != nil {
		return nil, err
	}
	if err := parseInt(cfgBatchSizeKey, logCtx, func(i int) { clientConfig.BatchSize = i }); err != nil {
		return nil, err
	}

	// parse backoff
	if err := parseDuration(cfgMinBackoffKey, logCtx, func(d time.Duration) { clientConfig.BackoffConfig.MinBackoff = d }); err != nil {
		return nil, err
	}
	if err := parseDuration(cfgMaxBackoffKey, logCtx, func(d time.Duration) { clientConfig.BackoffConfig.MaxBackoff = d }); err != nil {
		return nil, err
	}
	if err := parseInt(cfgMaxRetriesKey, logCtx, func(i int) { clientConfig.BackoffConfig.MaxRetries = i }); err != nil {
		return nil, err
	}

	// parse http & tls config
	if tlsCAFile, ok := logCtx.Config[cfgTLSCAFileKey]; ok {
		clientConfig.Client.TLSConfig.CAFile = tlsCAFile
	}
	if tlsCertFile, ok := logCtx.Config[cfgTLSCertFileKey]; ok {
		clientConfig.Client.TLSConfig.CertFile = tlsCertFile
	}
	if tlsCertFile, ok := logCtx.Config[cfgTLSCertFileKey]; ok {
		clientConfig.Client.TLSConfig.CertFile = tlsCertFile
	}
	if tlsKeyFile, ok := logCtx.Config[cfgTLSKeyFileKey]; ok {
		clientConfig.Client.TLSConfig.KeyFile = tlsKeyFile
	}
	if tlsServerName, ok := logCtx.Config[cfgTLSServerNameKey]; ok {
		clientConfig.Client.TLSConfig.ServerName = tlsServerName
	}
	if tlsInsecureSkipRaw, ok := logCtx.Config[cfgTLSInsecure]; ok {
		tlsInsecureSkip, err := strconv.ParseBool(tlsInsecureSkipRaw)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid external labels: %s", driverName, tlsInsecureSkipRaw)
		}
		clientConfig.Client.TLSConfig.InsecureSkipVerify = tlsInsecureSkip
	}
	if tlsProxyURL, ok := logCtx.Config[cfgProxyURLKey]; ok {
		proxyURL, err := url.Parse(tlsProxyURL)
		if err != nil {
			return nil, fmt.Errorf("%s: option %s is invalid %s", driverName, cfgProxyURLKey, err)
		}
		clientConfig.Client.ProxyURL.URL = proxyURL
	}

	// parse external labels
	extlbs, ok := logCtx.Config[cfgExternalLabelsKey]
	if !ok {
		extlbs = defaultExternalLabels
	}
	lvs := strings.Split(extlbs, ",")
	for _, lv := range lvs {
		lvparts := strings.Split(lv, "=")
		if len(lvparts) != 2 {
			return nil, fmt.Errorf("%s: invalid external labels: %s", driverName, extlbs)
		}
		labelName := model.LabelName(lvparts[0])
		if !labelName.IsValid() {
			return nil, fmt.Errorf("%s: invalid external label name: %s", driverName, labelName)
		}

		// expand the value using docker template {{.Name}}.{{.ImageName}}
		value, err := expandLabelValue(logCtx, lvparts[1])
		if err != nil {
			return nil, fmt.Errorf("%s: could not expand label value: %s err : %s", driverName, lvparts[1], err)
		}
		labelValue := model.LabelValue(value)
		if !labelValue.IsValid() {
			return nil, fmt.Errorf("%s: invalid external label value: %s", driverName, value)
		}
		labels[labelName] = labelValue
	}

	// other labels coming from docker labels or env selected by user labels, labels-regex, env, env-regex config.
	attrs, err := logCtx.ExtraAttributes(nil)
	if err != nil {
		return nil, err
	}

	// parse docker swarms labels and adds them automatically to attrs
	swarmService := logCtx.ContainerLabels[swarmServiceLabelKey]
	if swarmService != "" {
		attrs[swarmServiceLabelName] = swarmService
	}
	swarmStack := logCtx.ContainerLabels[swarmStackLabelKey]
	if swarmStack != "" {
		attrs[swarmStackLabelName] = swarmStack
	}

	// parse docker compose labels and adds them automatically to attrs
	composeService := logCtx.ContainerLabels[composeServiceLabelKey]
	if composeService != "" {
		attrs[composeServiceLabelName] = composeService
	}
	composeProject := logCtx.ContainerLabels[composeProjectLabelKey]
	if composeProject != "" {
		attrs[composeProjectLabelName] = composeProject
	}

	for key, value := range attrs {
		labelName := model.LabelName(key)
		if !labelName.IsValid() {
			return nil, fmt.Errorf("%s: invalid label name from attribute: %s", driverName, key)
		}
		labelValue := model.LabelValue(value)
		if !labelValue.IsValid() {
			return nil, fmt.Errorf("%s: invalid label value from attribute: %s", driverName, value)
		}
		labels[labelName] = labelValue
	}

	// adds host label and filename
	host, err := os.Hostname()
	if err == nil {
		labels[defaultHostLabelName] = model.LabelValue(host)
	}
	labels[targets.FilenameLabel] = model.LabelValue(logCtx.LogPath)

	// parse pipeline stages
	var pipeline PipelineConfig
	pipelineFile, ok := logCtx.Config[cfgPipelineStagesKey]
	if ok {
		if err := helpers.LoadConfig(pipelineFile, &pipeline); err != nil {
			return nil, fmt.Errorf("%s: error loading config file %s: %s", driverName, pipelineFile, err)
		}
	}

	return &config{
		labels:       labels,
		clientConfig: clientConfig,
		pipeline:     pipeline,
	}, nil
}

func expandLabelValue(info logger.Info, defaultTemplate string) (string, error) {
	tmpl, err := templates.NewParse("label_value", defaultTemplate)
	if err != nil {
		return "", err
	}
	buf := new(bytes.Buffer)
	if err := tmpl.Execute(buf, &info); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func parseDuration(key string, logCtx logger.Info, set func(d time.Duration)) error {
	if raw, ok := logCtx.Config[key]; ok {
		val, err := time.ParseDuration(raw)
		if err != nil {
			return fmt.Errorf("%s: invalid option %s format: %s", driverName, key, raw)
		}
		set(val)
	}
	return nil
}

func parseInt(key string, logCtx logger.Info, set func(i int)) error {
	if raw, ok := logCtx.Config[key]; ok {
		val, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("%s: invalid option %s format: %s", driverName, key, raw)
		}
		set(val)
	}
	return nil
}
