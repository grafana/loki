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

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
)

func AnnotatePodWithAvailabilityZone(ctx context.Context, log logr.Logger, c k8s.Client, pod *corev1.Pod) error {
	var err error

	ll := log.WithValues("lokistack-pod-zone-annotation event", "createOrUpdatePodWithLabelPred", "pod", pod.Name)

	nodeName := pod.Spec.NodeName
	if nodeName == "" {
		return nil
	}

	node := &corev1.Node{}
	key := client.ObjectKey{Name: nodeName}
	if err = c.Get(ctx, key, node); err != nil {
		return kverrors.Wrap(err, "failed to lookup node", "name", nodeName)
	}

	labelsAnnotation, ok := pod.Annotations[lokiv1.AnnotationAvailabilityZoneLabels]
	if !ok {
		return kverrors.New("zone-aware pod is missing node-labels annotation", "annotation", lokiv1.AnnotationAvailabilityZoneLabels)
	}
	labelKeys := strings.Split(labelsAnnotation, ",")

	availabilityZone, err := getAvailabilityZone(labelKeys, node.Labels)
	if err != nil {
		ll.Error(err, "failed to get pod availability zone", "name", pod.Name)
		return kverrors.Wrap(err, "failed to get pod availability zone", "name", pod.Name)
	}

	// Stop early if there is no annotation to set.
	if len(availabilityZone) == 0 {
		return nil
	}

	mergePatch, err := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": map[string]string{
				lokiv1.AnnotationAvailabilityZone: availabilityZone,
			},
		},
	})
	if err != nil {
		return kverrors.Wrap(err, "could not format the annotations patch", "name", availabilityZone)
	}

	if err = c.Patch(ctx, pod, client.RawPatch(types.StrategicMergePatchType, mergePatch)); err != nil && !errors.IsNotFound(err) {
		ll.Error(err, "could not patch the pod annotations", "name", pod.Name)
		return kverrors.Wrap(err, "Error patching the pod annotations", "name", pod.Name)
	}

	return nil
}

func getAvailabilityZone(labelKeys []string, nodeLabels map[string]string) (string, error) {
	labelValues := []string{}
	for _, key := range labelKeys {
		value, ok := nodeLabels[key]
		if !ok {
			return "", kverrors.New("scheduled node is missing label", "label", key)
		}

		labelValues = append(labelValues, value)
	}

	return strings.Join(labelValues, "_"), nil
}
