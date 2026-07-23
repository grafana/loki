package notification

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cloud.google.com/go/pubsub/v2"
	"github.com/fsouza/fake-gcs-server/internal/backend"
)

const publishTimeout = 10 * time.Second

// NotificationConfig mirrors the GCS Pub/Sub notification resource.
// See https://cloud.google.com/storage/docs/json_api/v1/notifications#resource.
type NotificationConfig struct {
	Kind             string            `json:"kind"`
	ID               string            `json:"id"`
	Topic            string            `json:"topic"`
	EventTypes       []EventType       `json:"event_types,omitempty"`
	ObjectNamePrefix string            `json:"object_name_prefix,omitempty"`
	PayloadFormat    string            `json:"payload_format"`
	CustomAttributes map[string]string `json:"custom_attributes,omitempty"`
}

type NotificationRegistry struct {
	mu      sync.RWMutex
	configs map[string][]NotificationConfig

	clientMu sync.Mutex
	clients  map[string]*pubsub.Client

	nextID atomic.Int64
	writer io.Writer
}

func NewNotificationRegistry(w io.Writer) *NotificationRegistry {
	return &NotificationRegistry{
		configs: make(map[string][]NotificationConfig),
		clients: make(map[string]*pubsub.Client),
		writer:  w,
	}
}

func (r *NotificationRegistry) Insert(bucket string, cfg NotificationConfig) NotificationConfig {
	cfg.Kind = "storage#notification"
	cfg.ID = fmt.Sprintf("%d", r.nextID.Add(1))
	if cfg.PayloadFormat == "" {
		cfg.PayloadFormat = "JSON_API_V1"
	}
	r.mu.Lock()
	r.configs[bucket] = append(r.configs[bucket], cfg)
	r.mu.Unlock()
	return cfg
}

func (r *NotificationRegistry) Get(bucket, id string) (NotificationConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, cfg := range r.configs[bucket] {
		if cfg.ID == id {
			return cfg, true
		}
	}
	return NotificationConfig{}, false
}

func (r *NotificationRegistry) List(bucket string) []NotificationConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cfgs := r.configs[bucket]
	if len(cfgs) == 0 {
		return nil
	}
	out := make([]NotificationConfig, len(cfgs))
	copy(out, cfgs)
	return out
}

func (r *NotificationRegistry) Delete(bucket, id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	cfgs := r.configs[bucket]
	for i, cfg := range cfgs {
		if cfg.ID == id {
			r.configs[bucket] = append(cfgs[:i], cfgs[i+1:]...)
			return true
		}
	}
	return false
}

func (r *NotificationRegistry) DeleteBucket(bucket string) {
	r.mu.Lock()
	delete(r.configs, bucket)
	r.mu.Unlock()
}

func (r *NotificationRegistry) Close() error {
	r.clientMu.Lock()
	defer r.clientMu.Unlock()
	var firstErr error
	for projectID, c := range r.clients {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("notification registry: closing pubsub client for project %q: %w", projectID, err)
		}
		delete(r.clients, projectID)
	}
	return firstErr
}

func (r *NotificationRegistry) Trigger(ctx context.Context, o *backend.StreamingObject, eventType EventType, extraEventAttr map[string]string) {
	r.mu.RLock()
	local := make([]NotificationConfig, len(r.configs[o.BucketName]))
	copy(local, r.configs[o.BucketName])
	r.mu.RUnlock()

	for _, cfg := range local {
		if !matchesNotificationConfig(cfg, o, eventType) {
			continue
		}
		go func() {
			pubCtx, cancel := context.WithTimeout(ctx, publishTimeout)
			defer cancel()
			if err := r.publish(pubCtx, cfg, o, eventType, extraEventAttr); err != nil {
				if r.writer != nil {
					fmt.Fprintf(r.writer, "error publishing notification (id=%s): %v\n", cfg.ID, err)
				}
			}
		}()
	}
}

func matchesNotificationConfig(cfg NotificationConfig, o *backend.StreamingObject, eventType EventType) bool {
	if cfg.ObjectNamePrefix != "" && !strings.HasPrefix(o.Name, cfg.ObjectNamePrefix) {
		return false
	}
	if len(cfg.EventTypes) > 0 {
		for _, et := range cfg.EventTypes {
			if et == eventType {
				return true
			}
		}
		return false
	}
	return true
}

func (r *NotificationRegistry) publish(ctx context.Context, cfg NotificationConfig, o *backend.StreamingObject, eventType EventType, extraEventAttr map[string]string) error {
	projectID, topicName, err := splitTopic(cfg.Topic)
	if err != nil {
		return err
	}

	client, err := r.getOrCreateClient(ctx, projectID)
	if err != nil {
		return err
	}

	attrs := make(map[string]string, len(cfg.CustomAttributes))
	for k, v := range cfg.CustomAttributes {
		attrs[k] = v
	}

	eventTime := time.Now().Format(time.RFC3339)
	var data []byte
	data, attrs, err = generateEventWithAttrs(o, eventType, cfg.PayloadFormat, eventTime, extraEventAttr, attrs)
	if err != nil {
		return err
	}

	res := client.Publisher(topicName).Publish(ctx, &pubsub.Message{Data: data, Attributes: attrs})
	_, err = res.Get(ctx)
	return err
}

func (r *NotificationRegistry) getOrCreateClient(ctx context.Context, projectID string) (*pubsub.Client, error) {
	r.clientMu.Lock()
	defer r.clientMu.Unlock()
	if c, ok := r.clients[projectID]; ok {
		return c, nil
	}
	c, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("notification registry: creating pubsub client for project %q: %w", projectID, err)
	}
	r.clients[projectID] = c
	return c, nil
}

func splitTopic(topic string) (string, string, error) {
	parts := strings.Split(topic, "/")
	if len(parts) != 4 || parts[0] != "projects" || parts[2] != "topics" {
		return "", "", fmt.Errorf("invalid topic format %q: expected projects/{project}/topics/{name}", topic)
	}
	return parts[1], parts[3], nil
}
