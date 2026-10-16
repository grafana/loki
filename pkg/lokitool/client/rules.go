package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	log "github.com/sirupsen/logrus"
	"go.yaml.in/yaml/v3"

	"github.com/grafana/loki/v3/pkg/lokitool/rules/rwrulefmt"
)

const (
	rulerAPIPath       = "/loki/api/v1/rules"
	legacyRulerAPIPath = "/api/prom/rules"
)

func (c *Client) rulesPath() string {
	if c.cfg.UseLegacyRoutes {
		return legacyRulerAPIPath
	}
	return rulerAPIPath
}

// CreateRuleGroup creates or updates a rule group.
func (c *Client) CreateRuleGroup(ctx context.Context, namespace string, rg rwrulefmt.RuleGroup) error {
	payload, err := yaml.Marshal(&rg)
	if err != nil {
		return err
	}

	path := c.rulesPath() + "/" + url.PathEscape(namespace)
	res, err := c.Do(ctx, http.MethodPost, path, "", payload)
	if err != nil {
		return err
	}
	return res.Body.Close()
}

// DeleteRuleGroup deletes a rule group.
func (c *Client) DeleteRuleGroup(ctx context.Context, namespace, groupName string) error {
	path := c.rulesPath() + "/" + url.PathEscape(namespace) + "/" + url.PathEscape(groupName)
	res, err := c.Do(ctx, http.MethodDelete, path, "", nil)
	if err != nil {
		return err
	}
	return res.Body.Close()
}

// GetRuleGroup retrieves a rule group.
func (c *Client) GetRuleGroup(ctx context.Context, namespace, groupName string) (*rwrulefmt.RuleGroup, error) {
	path := c.rulesPath() + "/" + url.PathEscape(namespace) + "/" + url.PathEscape(groupName)
	res, err := c.Do(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	rg := rwrulefmt.RuleGroup{}
	if err := yaml.Unmarshal(body, &rg); err != nil {
		log.WithField("body", string(body)).Debug("failed to unmarshal rule group from response")
		return nil, fmt.Errorf("unable to unmarshal response: %w", err)
	}

	return &rg, nil
}

// ListRules retrieves the rule groups of a namespace by namespace.
// It retrieves the rule groups of all namespaces if the namespace is empty.
func (c *Client) ListRules(ctx context.Context, namespace string) (map[string][]rwrulefmt.RuleGroup, error) {
	path := c.rulesPath()
	if namespace != "" {
		path += "/" + url.PathEscape(namespace)
	}

	res, err := c.Do(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	ruleSet := map[string][]rwrulefmt.RuleGroup{}
	if err := yaml.Unmarshal(body, &ruleSet); err != nil {
		return nil, fmt.Errorf("unable to unmarshal response: %w", err)
	}

	return ruleSet, nil
}
