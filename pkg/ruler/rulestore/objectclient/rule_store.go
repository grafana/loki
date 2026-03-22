package objectclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/ruler/rulespb"
	"github.com/grafana/loki/v3/pkg/ruler/rulestore"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
)

// Object Rule Storage Schema
// =======================
// Object Name: "rules/<user_id>/<base64 URL Encoded: namespace>/<base64 URL Encoded: group_name>"
// Storage Format: Encoded RuleGroupDesc
//
// Prometheus Rule Groups can include a large number of characters that are not valid object names
// in common object storage systems. A URL Base64 encoding allows for generic consistent naming
// across all backends

const (
	delim      = "/"
	rulePrefix = "rules" + delim
)

// RuleStore allows cortex rules to be stored using an object store backend.
type RuleStore struct {
	client          client.ObjectClient
	loadConcurrency int

	logger log.Logger
}

// NewRuleStore returns a new RuleStore
func NewRuleStore(client client.ObjectClient, loadConcurrency int, logger log.Logger) *RuleStore {
	return &RuleStore{
		client:          client,
		loadConcurrency: loadConcurrency,
		logger:          logger,
	}
}

// If existing rule group is supplied, it is Reset and reused. If nil, new RuleGroupDesc is allocated.
func (o *RuleStore) getRuleGroup(ctx context.Context, objectKey string, rg *rulespb.RuleGroupDesc) (*rulespb.RuleGroupDesc, error) {
	reader, _, err := o.client.GetObject(ctx, objectKey)
	if err != nil {
		if o.client.IsObjectNotFoundErr(err) {
			level.Debug(o.logger).Log("msg", "rule group does not exist", "name", objectKey)
			return nil, errors.Wrapf(rulestore.ErrGroupNotFound, "get rule group user=%q, namespace=%q, name=%q", rg.GetUser(), rg.GetNamespace(), rg.GetName())
		}

		return nil, errors.Wrapf(err, "failed to get rule group %s", objectKey)
	}
	defer func() { _ = reader.Close() }()

	buf, err := io.ReadAll(reader)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read rule group %s", objectKey)
	}

	if rg == nil {
		rg = &rulespb.RuleGroupDesc{}
	} else {
		rg.Reset()
	}

	err = proto.Unmarshal(buf, rg)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal rule group %s", objectKey)
	}

	return rg, nil
}

func (o *RuleStore) ListAllUsers(ctx context.Context) ([]string, error) {
	_, prefixes, err := o.client.List(ctx, rulePrefix, delim)
	if err != nil {
		return nil, err
	}

	var result []string
	for _, p := range prefixes {
		s := string(p)

		s = strings.TrimPrefix(s, rulePrefix)
		s = strings.TrimSuffix(s, delim)

		if s != "" {
			result = append(result, s)
		}
	}

	return result, nil
}

// ListAllRuleGroups implements rules.RuleStore.
func (o *RuleStore) ListAllRuleGroups(ctx context.Context) (map[string]rulespb.RuleGroupList, error) {
	// No delimiter to get *all* rule groups for all users and namespaces.
	ruleGroupObjects, _, err := o.client.List(ctx, rulePrefix, "")
	if err != nil {
		return nil, err
	}

	return convertRuleGroupObjectsToMap(ruleGroupObjects), nil
}

func (o *RuleStore) ListRuleGroupsForUserAndNamespace(ctx context.Context, userID, namespace string) (rulespb.RuleGroupList, error) {
	ruleGroupObjects, _, err := o.client.List(ctx, generateRuleObjectKey(userID, namespace, ""), "")
	if err != nil {
		return nil, err
	}

	return convertRuleGroupObjectsToMap(ruleGroupObjects)[userID], nil
}

func (o *RuleStore) LoadRuleGroups(ctx context.Context, groupsToLoad map[string]rulespb.RuleGroupList) error {
	ch := make(chan *rulespb.RuleGroupDesc)

	// Given we store one file per rule group. With this, we create a pool of workers that will
	// download all rule groups in parallel. We limit the number of workers to avoid a
	// particular user having too many rule groups rate limiting us with the object storage.
	g, gCtx := errgroup.WithContext(ctx)
	for i := 0; i < o.loadConcurrency; i++ {
		g.Go(func() error {
			for gr := range ch {
				if gr == nil {
					continue
				}

				user, namespace, group := gr.GetUser(), gr.GetNamespace(), gr.GetName()
				if user == "" || namespace == "" || group == "" {
					return fmt.Errorf("invalid rule group: user=%q, namespace=%q, group=%q", user, namespace, group)
				}

				key := generateRuleObjectKey(user, namespace, group)

				level.Debug(o.logger).Log("msg", "loading rule group", "key", key, "user", user)
				gr, err := o.getRuleGroup(gCtx, key, gr) // reuse group pointer from the map.
				if err != nil {
					level.Error(o.logger).Log("msg", "failed to get rule group", "key", key, "user", user)
					return err
				}

				if user != gr.User || namespace != gr.Namespace || group != gr.Name {
					return fmt.Errorf("mismatch between requested rule group and loaded rule group, requested: user=%q, namespace=%q, group=%q, loaded: user=%q, namespace=%q, group=%q", user, namespace, group, gr.User, gr.Namespace, gr.Name)
				}
			}

			return nil
		})
	}

outer:
	for _, gs := range groupsToLoad {
		for _, g := range gs {
			select {
			case <-gCtx.Done():
				break outer
			case ch <- g:
				// ok
			}
		}
	}
	close(ch)

	return g.Wait()
}

func convertRuleGroupObjectsToMap(ruleGroupObjects []client.StorageObject) map[string]rulespb.RuleGroupList {
	result := map[string]rulespb.RuleGroupList{}
	for _, rg := range ruleGroupObjects {
		user, namespace, group := decomposeRuleObjectKey(rg.Key)
		if user == "" || namespace == "" || group == "" {
			continue
		}

		result[user] = append(result[user], &rulespb.RuleGroupDesc{
			User:      user,
			Namespace: namespace,
			Name:      group,
		})
	}
	return result
}

// GetRuleGroup returns the requested rule group
func (o *RuleStore) GetRuleGroup(ctx context.Context, userID string, namespace string, grp string) (*rulespb.RuleGroupDesc, error) {
	handle := generateRuleObjectKey(userID, namespace, grp)
	return o.getRuleGroup(ctx, handle, nil)
}

// SetRuleGroup sets provided rule group
func (o *RuleStore) SetRuleGroup(ctx context.Context, userID string, namespace string, group *rulespb.RuleGroupDesc) error {
	data, err := proto.Marshal(group)
	if err != nil {
		return err
	}

	objectKey := generateRuleObjectKey(userID, namespace, group.Name)
	return o.client.PutObject(ctx, objectKey, bytes.NewReader(data))
}

// DeleteRuleGroup deletes the specified rule group
func (o *RuleStore) DeleteRuleGroup(ctx context.Context, userID string, namespace string, groupName string) error {
	objectKey := generateRuleObjectKey(userID, namespace, groupName)
	err := o.client.DeleteObject(ctx, objectKey)
	if o.client.IsObjectNotFoundErr(err) {
		return rulestore.ErrGroupNotFound
	}
	return err
}

// DeleteNamespace deletes all the rule groups in the specified namespace
func (o *RuleStore) DeleteNamespace(ctx context.Context, userID, namespace string) error {
	ruleGroupObjects, _, err := o.client.List(ctx, generateRuleObjectKey(userID, namespace, ""), "")
	if err != nil {
		return err
	}

	if len(ruleGroupObjects) == 0 {
		return rulestore.ErrGroupNamespaceNotFound
	}

	for _, obj := range ruleGroupObjects {
		if err := ctx.Err(); err != nil {
			return err
		}

		level.Debug(o.logger).Log("msg", "deleting rule group", "namespace", namespace, "key", obj.Key)
		err = o.client.DeleteObject(ctx, obj.Key)
		if err != nil {
			level.Error(o.logger).Log("msg", "unable to delete rule group from namespace", "err", err, "namespace", namespace, "key", obj.Key)
			return err
		}
	}

	return nil
}

func generateRuleObjectKey(userID, namespace, groupName string) string {
	if userID == "" {
		return rulePrefix
	}

	prefix := rulePrefix + userID + delim
	if namespace == "" {
		return prefix
	}

	ns := base64.URLEncoding.EncodeToString([]byte(namespace)) + delim
	if groupName == "" {
		return prefix + ns
	}

	return prefix + ns + base64.URLEncoding.EncodeToString([]byte(groupName))
}

func decomposeRuleObjectKey(objectKey string) (userID, namespace, groupName string) {
	if !strings.HasPrefix(objectKey, rulePrefix) {
		return
	}

	components := strings.Split(objectKey, delim)
	if len(components) != 4 {
		return
	}

	ns, err := base64.URLEncoding.DecodeString(components[2])
	if err != nil {
		return
	}

	gr, err := base64.URLEncoding.DecodeString(components[3])
	if err != nil {
		return
	}

	return components[1], string(ns), string(gr)
}
