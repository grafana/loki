package handlers

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/grafana/loki/operator/internal/external/k8s"
)

const (
	LokiInstanceAZ = "loki.grafana.com/availability-zone"
)

// isPodScheduled returns if the Pod is scheduled. If true it also returns the node name.
func isPodScheduled(pod *corev1.Pod) (bool, string) {
	status := pod.Status
	for i := range status.Conditions {
		if !(status.Conditions[i].Type == corev1.PodScheduled) {
			continue
		}
		return status.Conditions[i].Status == corev1.ConditionTrue && pod.Spec.NodeName != "", pod.Spec.NodeName
	}
	return false, ""
}

func AnnotatePodsWithNodeLabels(ctx context.Context, log logr.Logger, c k8s.Client, pod corev1.Pod, lokiLabelValue string) error {
	var err error

	ll := log.WithValues("lokistack-pod-zone-annotation event", "createOrUpdatePred", "pod", pod.Name)

	scheduled, nodeName := isPodScheduled(&pod)
	if !scheduled {
		return nil
	}

	node := &corev1.Node{}
	key := client.ObjectKey{Name: nodeName}
	if err = c.Get(ctx, key, node); err != nil {
		return kverrors.Wrap(err, "failed to lookup node", "name", nodeName)
	}

	annotations := pod.GetAnnotations()
	var topologykeys []string

	for key := range annotations {
		if key == "loki.grafana.com/zoneawareannotation" {
			topologykeys = strings.Split(annotations["loki.grafana.com/zoneawareannotation"], ",")
		}
	}
	// topologykeys := strings.Split(lokiLabelValue, "-")

	podAnnotations, err := getPodAnnotations(&pod, topologykeys, node.Labels)
	if err != nil {
		ll.Error(err, "failed to set the pod annotations", "name", pod.Name)
		return kverrors.Wrap(err, "failed to set the pod annotations", "name", pod.Name)
	}

	// Stop early if there is no annotation to set.
	if len(podAnnotations) == 0 {
		return nil
	}

	mergePatch, err := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": podAnnotations,
		},
	})
	if err != nil {
		return kverrors.Wrap(err, "could not format the pod annotations", "name", podAnnotations)
	}

	if err = c.Patch(ctx, &pod, client.RawPatch(types.StrategicMergePatchType, mergePatch)); err != nil && !errors.IsNotFound(err) {
		ll.Error(err, "could not patch the pod annotations", "name", pod.Name)
		return kverrors.Wrap(err, "Error patching the pod annotations", "name", pod.Name)
	}

	return nil
}

// getPodAnnotations returns missing annotations, and their values, expected on a given Pod.
// It also ensures that labels exist on the K8S node, if not the case an error is returned.
func getPodAnnotations(pod *corev1.Pod, expectedAnnotations []string, nodeLabels map[string]string) (map[string]string, error) {
	podAnnotations := make(map[string]string)
	var missingLabels []string
	var region, zone, hostname, other bool
	for _, expectedAnnotation := range expectedAnnotations {
		_, ok := nodeLabels[expectedAnnotation]
		if !ok {
			missingLabels = append(missingLabels, expectedAnnotation)
			continue
		}
		// Check if the annotations is already set
		if _, alreadyExists := pod.Annotations[expectedAnnotation]; alreadyExists {
			continue
		}
		// podAnnotations[expectedAnnotation] = value
		switch expectedAnnotation {
		case corev1.LabelHostname:
			hostname = true
		case corev1.LabelTopologyZone:
			zone = true
		case corev1.LabelTopologyRegion:
			region = true
		default:
			other = true

		}
	}

	if len(missingLabels) > 0 {
		return nil, kverrors.Wrap(nil, "missing node labels", "topology_label", missingLabels)
	}

	// Concatenate the labels as region_zone_hostname when using the common topology labels in kubernetes
	if region {
		podAnnotations[LokiInstanceAZ] = nodeLabels[corev1.LabelTopologyRegion]
	}
	if zone {
		podAnnotations[LokiInstanceAZ] = concatenatePodAnnotation(corev1.LabelTopologyZone, podAnnotations[LokiInstanceAZ], nodeLabels)
	}
	if hostname {
		podAnnotations[LokiInstanceAZ] = concatenatePodAnnotation(corev1.LabelHostname, podAnnotations[LokiInstanceAZ], nodeLabels)
	}

	if other {
		for _, expectedAnnotation := range expectedAnnotations {
			switch expectedAnnotation {
			case corev1.LabelHostname:
			case corev1.LabelTopologyZone:
			case corev1.LabelTopologyRegion:
				continue
			default:
				podAnnotations[LokiInstanceAZ] = concatenatePodAnnotation(expectedAnnotation, podAnnotations[LokiInstanceAZ], nodeLabels)
			}
		}
	}

	return podAnnotations, nil
}

func concatenatePodAnnotation(label, podAnnotation string, nodeLabels map[string]string) string {
	var podannotation_value string
	if podAnnotation != "" {
		podannotation_value = podAnnotation + "_" + nodeLabels[label]
	} else {
		podannotation_value = nodeLabels[label]
	}
	return podannotation_value
}
