/*
Copyright 2025 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package bigtable

import (
	"context"
	"fmt"
	"strings"
	"time"

	mexporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric"
	"go.opentelemetry.io/contrib/detectors/gcp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/stats/opentelemetry"
)

const (
	bigtableClientMonitoredResourceName = "bigtable_client"
	bigtableClientMetricPrefix          = "bigtable.googleapis.com/internal/client/"
	unKnownAtttr                        = "unknown"
)

var latenciesBoundaries = []float64{0.0, 0.001, 0.002, 0.003, 0.004, 0.005, 0.006, 0.008, 0.01, 0.013, 0.016, 0.02, 0.025, 0.03, 0.04, 0.05, 0.065, 0.08, 0.1, 0.13, 0.16, 0.2, 0.25, 0.3, 0.4, 0.5, 0.65, 0.8, 1.0, 2.0, 5.0, 10.0, 20.0, 50.0, 100.0, 200.0, 400.0, 800.0, 1600.0, 3200.0} // max is 53.3 minutes

// bigtable_client monitored resource labels
type bigtableClientMonitoredResource struct {
	project       string // project
	instance      string // instance
	appProfile    string // app_profile
	clientProject string // client_project
	cloudPlatform string // cloud_platform
	region        string // client_region
	hostID        string // host_id
	hostName      string // host_name
	clientName    string // client_name
	clientUID     string // uuid
	resource      *resource.Resource
}

func (bmr *bigtableClientMonitoredResource) exporter() (metric.Exporter, error) {
	exporter, err := mexporter.New(
		mexporter.WithProjectID(bmr.project),
		mexporter.WithMetricDescriptorTypeFormatter(metricFormatter),
		mexporter.WithCreateServiceTimeSeries(),
		mexporter.WithMonitoredResourceDescription(bigtableClientMonitoredResourceName, []string{"project_id", "instance", "app_profile", "client_project", "cloud_platform", "host_id", "host_name", "client_name", "uuid", "region"}),
	)
	if err != nil {
		return nil, fmt.Errorf("bigtable: creating metrics exporter: %w", err)
	}
	return exporter, nil
}

func metricFormatter(m metricdata.Metrics) string {
	// converts grpc.lb.rls.target_picks to `bigtable.googleapis.com/internal/client/grpc/lb/rls/target_picks`
	return bigtableClientMetricPrefix + strings.ReplaceAll(string(m.Name), ".", "/")
}

func getAttribute(s *attribute.Set, key attribute.Key, defaultValue string) string {
	if val, ok := s.Value(key); ok {
		return val.AsString()
	}
	return defaultValue
}

func newBigtableClientMonitoredResource(ctx context.Context, project, appProfile, instance, clientName, clientUID string, opts ...resource.Option) (*bigtableClientMonitoredResource, error) {
	detectedAttrs, err := resource.New(ctx, opts...)
	if err != nil {
		return nil, err
	}
	smr := &bigtableClientMonitoredResource{
		project:    project,
		instance:   instance,
		appProfile: appProfile,
		clientName: clientName,
		clientUID:  clientUID,
	}
	s := detectedAttrs.Set()
	// Attempt to use resource detector project id if project id wasn't
	// identified using ADC as a last resort. Otherwise metrics cannot be started.

	smr.clientProject = getAttribute(s, "cloud.account.id", unKnownAtttr)
	smr.cloudPlatform = getAttribute(s, "cloud.platform", unKnownAtttr)

	smr.hostName = getAttribute(s, "host.name", "")
	// See https://opentelemetry.io/docs/specs/semconv/resource/k8s/#pod
	if smr.hostName == "" {
		smr.hostName = getAttribute(s, "k8s.pod.name", "")
	}
	// Fallback https://opentelemetry.io/docs/specs/semconv/resource/k8s/#pod
	if smr.hostName == "" {
		// Fallback to Node name if pod name is missing, add unknown
		smr.hostName = getAttribute(s, "k8s.node.name", unKnownAtttr)
	}

	smr.hostID = getAttribute(s, "host.id", unKnownAtttr)
	if smr.hostID == "unknown" {
		// cloud run / cloud functions have faas.id instead of host.id

		smr.hostID = getAttribute(s, "faas.id", unKnownAtttr)
	}

	// See https://opentelemetry.io/docs/specs/semconv/resource/cloud/
	smr.region = getAttribute(s, "cloud.region", "")
	if smr.region == "" {
		zone := getAttribute(s, "cloud.availability_zone", "")
		if zone != "" && zone != unKnownAtttr {
			// handle for only gce zones.
			if lastDash := strings.LastIndex(zone, "-"); lastDash > 0 {
				smr.region = zone[:lastDash]
			}
		}
	}

	if smr.region == "" {
		smr.region = "global"
	}

	smr.resource, err = resource.New(ctx, resource.WithAttributes([]attribute.KeyValue{
		{Key: "gcp.resource_type", Value: attribute.StringValue(bigtableClientMonitoredResourceName)},
		{Key: "project_id", Value: attribute.StringValue(project)},
		{Key: "app_profile", Value: attribute.StringValue(smr.appProfile)},
		{Key: "region", Value: attribute.StringValue(smr.region)},
		{Key: "instance", Value: attribute.StringValue(smr.instance)},
		{Key: "cloud_platform", Value: attribute.StringValue(smr.cloudPlatform)},
		{Key: "host_id", Value: attribute.StringValue(smr.hostID)},
		{Key: "host_name", Value: attribute.StringValue(smr.hostName)},
		{Key: "client_name", Value: attribute.StringValue(smr.clientName)},
		{Key: "uuid", Value: attribute.StringValue(smr.clientUID)},
		{Key: "client_project", Value: attribute.StringValue(smr.clientProject)},
	}...))
	if err != nil {
		return nil, err
	}
	return smr, nil
}

type otelMetricsContext struct {
	// client options passed to gRPC channels
	clientOpts []option.ClientOption
	// instance of metric reader used by gRPC client-side metrics
	otelMeterProvider *metric.MeterProvider
	// clean func to call when closing gRPC client
	close func()
}

type metricsConfig struct {
	project         string // project_id
	instance        string // instance
	appProfile      string // app_profile
	clientName      string // client_name
	clientUID       string // uuid
	interval        time.Duration
	customExporter  *metric.Exporter
	manualReader    *metric.ManualReader // used by tests
	disableExporter bool                 // used by tests disables exports
	resourceOpts    []resource.Option    // used by tests
}

func newOtelMetricsContext(ctx context.Context, cfg metricsConfig) (*otelMetricsContext, error) {
	var exporter metric.Exporter
	meterOpts := []metric.Option{}
	if cfg.customExporter == nil {
		var ropts []resource.Option
		if cfg.resourceOpts != nil {
			ropts = cfg.resourceOpts
		} else {
			ropts = []resource.Option{resource.WithDetectors(gcp.NewDetector())}
		}
		smr, err := newBigtableClientMonitoredResource(ctx, cfg.project, cfg.appProfile, cfg.instance, cfg.clientName, cfg.clientUID, ropts...)
		if err != nil {
			return nil, err
		}
		exporter, err = smr.exporter()
		if err != nil {
			return nil, err
		}
		meterOpts = append(meterOpts, metric.WithResource(smr.resource))
	} else {
		exporter = *cfg.customExporter
	}
	interval := time.Minute
	if cfg.interval > 0 {
		interval = cfg.interval
	}
	meterOpts = append(meterOpts,
		// customer histogram boundaries
		metric.WithView(
			createHistogramView("grpc.client.attempt.duration", latenciesBoundaries),
		))
	if cfg.manualReader != nil {
		meterOpts = append(meterOpts, metric.WithReader(cfg.manualReader))
	}
	if !cfg.disableExporter {
		meterOpts = append(meterOpts, metric.WithReader(
			metric.NewPeriodicReader(&exporterLogSuppressor{Exporter: exporter}, metric.WithInterval(interval))))
	}
	otelMeterProvider := metric.NewMeterProvider(meterOpts...)
	mo := opentelemetry.MetricsOptions{
		MeterProvider: otelMeterProvider,
		Metrics: stats.NewMetricSet(
			"grpc.client.attempt.duration",
			"grpc.lb.rls.default_target_picks",
			"grpc.lb.rls.target_picks",
			"grpc.lb.rls.failed_picks",
			"grpc.xds_client.server_failure",
			"grpc.xds_client.resource_updates_invalid",
			"grpc.subchannel.open_connections",
			"grpc.subchannel.disconnections",
			"grpc.subchannel.connection_attempts_succeeded",
			"grpc.subchannel.connection_attempts_failed",
		),
		OptionalLabels: []string{"grpc.lb.locality"},
	}
	opts := []option.ClientOption{
		option.WithGRPCDialOption(
			opentelemetry.DialOption(opentelemetry.Options{MetricsOptions: mo})),
		option.WithGRPCDialOption(
			grpc.WithDefaultCallOptions(grpc.StaticMethodCallOption{})),
	}
	return &otelMetricsContext{
		clientOpts:        opts,
		otelMeterProvider: otelMeterProvider,
		close: func() {
			otelMeterProvider.Shutdown(ctx)
		},
	}, nil
}

// Silences permission errors after initial error is emitted to prevent
// chatty logs.
type exporterLogSuppressor struct {
	metric.Exporter
	emittedFailure bool
}

// Implements OTel SDK metric.Exporter interface to prevent noisy logs from
// lack of credentials after initial failure.
func (e *exporterLogSuppressor) Export(ctx context.Context, rm *metricdata.ResourceMetrics) error {
	if err := e.Exporter.Export(ctx, rm); err != nil && !e.emittedFailure {
		if strings.Contains(err.Error(), "PermissionDenied") {
			e.emittedFailure = true
			return fmt.Errorf("gRPC metrics failed due permission issue: %w", err)
		}
		return err
	}
	return nil
}

func createHistogramView(name string, boundaries []float64) metric.View {
	return metric.NewView(metric.Instrument{
		Name: name,
		Kind: metric.InstrumentKindHistogram,
	}, metric.Stream{
		Name:        name,
		Aggregation: metric.AggregationExplicitBucketHistogram{Boundaries: boundaries},
	})
}
