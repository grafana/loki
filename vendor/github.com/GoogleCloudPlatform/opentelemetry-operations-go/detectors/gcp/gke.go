// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/oauth2/google"
)

const (
	// If the kubernetes.default.svc service exists in the cluster,
	// then the KUBERNETES_SERVICE_HOST env var will be populated.
	// Use this as an indication that we are running on kubernetes.
	k8sServiceHostEnv = "KUBERNETES_SERVICE_HOST"
	// See the available GKE metadata:
	// https://cloud.google.com/kubernetes-engine/docs/concepts/workload-identity#instance_metadata
	clusterNameMetadataAttr     = "cluster-name"
	clusterLocationMetadataAttr = "cluster-location"
	computeInstancesGetScope    = "https://www.googleapis.com/auth/compute.readonly"
	defaultComputeBaseURL       = "https://compute.googleapis.com/compute/v1"
)

func (d *Detector) onGKE() bool {
	// Check if we are on k8s first
	_, found := d.os.LookupEnv(k8sServiceHostEnv)
	if !found {
		return false
	}
	// If we are on k8s, make sure that we are actually on GKE, and not a
	// different managed k8s platform.
	_, err := d.metadata.InstanceAttributeValueWithContext(context.TODO(), clusterLocationMetadataAttr)
	return err == nil
}

// GKEHostID returns the instance ID of the instance on which this program is running.
func (d *Detector) GKEHostID() (string, error) {
	return d.GCEHostID()
}

// GKEClusterName returns the name if the GKE cluster in which this program is running.
func (d *Detector) GKEClusterName() (string, error) {
	return d.metadata.InstanceAttributeValueWithContext(context.TODO(), clusterNameMetadataAttr)
}

// GKEHostType returns the machine type of the instance on which this program is running.
func (d *Detector) GKEHostType() (string, error) {
	ctx := context.TODO()
	projectID, err := d.ProjectID()
	if err != nil {
		return "", err
	}
	zone, err := d.metadata.ZoneWithContext(ctx)
	if err != nil {
		return "", err
	}
	instanceName, err := d.metadata.InstanceNameWithContext(ctx)
	if err != nil {
		return "", err
	}

	machineType, err := d.computeInstanceMachineType(ctx, projectID, lastPathSegment(zone), instanceName)
	if err != nil {
		return "", err
	}
	return lastPathSegment(machineType), nil
}

func (d *Detector) computeInstanceMachineType(ctx context.Context, projectID, zone, instanceName string) (string, error) {
	client, err := d.computeHTTPClient(ctx)
	if err != nil {
		return "", err
	}
	baseURL := d.computeBaseURL
	if baseURL == "" {
		baseURL = defaultComputeBaseURL
	}
	reqURL := fmt.Sprintf("%s/projects/%s/zones/%s/instances/%s",
		strings.TrimRight(baseURL, "/"),
		url.PathEscape(projectID),
		url.PathEscape(zone),
		url.PathEscape(instanceName),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", err
	}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("compute instances.get returned %s", res.Status)
	}
	var instance struct {
		MachineType string `json:"machineType"`
	}
	if err := json.NewDecoder(res.Body).Decode(&instance); err != nil {
		return "", err
	}
	if instance.MachineType == "" {
		return "", fmt.Errorf("compute instances.get response missing machineType")
	}
	return instance.MachineType, nil
}

func (d *Detector) computeHTTPClient(ctx context.Context) (*http.Client, error) {
	if d.httpClient != nil {
		return d.httpClient, nil
	}
	return google.DefaultClient(ctx, computeInstancesGetScope)
}

func lastPathSegment(s string) string {
	s = strings.TrimRight(s, "/")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

type LocationType int64

const (
	UndefinedLocation LocationType = iota
	Zone
	Region
)

// GKEAvailabilityZoneOrRegion returns the location of the cluster and whether the cluster is zonal or regional.
func (d *Detector) GKEAvailabilityZoneOrRegion() (string, LocationType, error) {
	clusterLocation, err := d.metadata.InstanceAttributeValueWithContext(context.TODO(), clusterLocationMetadataAttr)
	if err != nil {
		return "", UndefinedLocation, err
	}
	switch strings.Count(clusterLocation, "-") {
	case 1:
		return clusterLocation, Region, nil
	case 2:
		return clusterLocation, Zone, nil
	default:
		return "", UndefinedLocation, fmt.Errorf("unrecognized format for cluster location: %v", clusterLocation)
	}
}
