package storageschema

import (
	"context"

	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/grafana/loki/operator/internal/manifests/storage"

	"github.com/ViaQ/logerr/v2/kverrors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/json"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

type lokiConfig struct {
	Schema lokiSchemaConfig `json:"schema_config"`
}

type lokiSchemaConfig struct {
	Configs []lokiStorageSchema `json:"configs"`
}

type lokiStorageSchema struct {
	From   string `json:"from"`
	Schema string `json:"schema"`
}

const (
	// LokiConfigFileName is the name of the config file in the configmap
	LokiConfigFileName = "config.yaml"
)

// GetLokiStorageSchemaData returns the previous and current storage schemas that
// need to be applied to the lokistack.
func GetLokiStorageSchemaData(ctx context.Context, k k8s.Client, req ctrl.Request) ([]storage.Schema, error) {
	var lokiConfigMap corev1.ConfigMap
	key := client.ObjectKey{Name: manifests.LokiConfigMapName(req.Name), Namespace: req.Namespace}

	if err := k.Get(ctx, key, &lokiConfigMap); err != nil {
		if apierrors.IsNotFound(err) {
			return []storage.Schema{}, nil
		}

		return []storage.Schema{}, err
	}

	scm, err := extractStorageSchemaConfigMap(&lokiConfigMap)
	if err != nil {
		return []storage.Schema{}, err
	}

	schemas := make([]storage.Schema, len(scm))
	for i, schema := range scm {
		schemas[i] = storage.Schema{
			From:    schema.From,
			Version: lokiv1beta1.ObjectStorageSchemaVersion(schema.Schema),
		}
	}

	return schemas, nil
}

// extractStorageSchemaConfigMap extracts storage_schema data if valid.
func extractStorageSchemaConfigMap(cm *corev1.ConfigMap) ([]lokiStorageSchema, error) {
	// Extract required fields from config.yaml
	lokiConfigYAML, ok := cm.BinaryData[LokiConfigFileName]
	if !ok {
		return nil, kverrors.New("missing config.yaml file in configMap.")
	}

	lokiConfigJSON, err := yaml.YAMLToJSON(lokiConfigYAML)
	if err != nil {
		return nil, kverrors.Wrap(err, "error in converting loki config yaml to json.")
	}

	var lokiStorageConfig lokiConfig
	err = json.Unmarshal(lokiConfigJSON, &lokiStorageConfig)
	if err != nil {
		return nil, kverrors.Wrap(err, "error in unmarshalling loki config to struct.")
	}

	return lokiStorageConfig.Schema.Configs, nil
}
