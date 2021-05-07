// Copyright 2019 Prometheus Team
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pushover

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	commoncfg "github.com/prometheus/common/config"

	"github.com/prometheus/alertmanager/config"
	"github.com/prometheus/alertmanager/notify"
	"github.com/prometheus/alertmanager/template"
	"github.com/prometheus/alertmanager/types"
)

// Notifier implements a Notifier for Pushover notifications.
type Notifier struct {
	conf    *config.PushoverConfig
	tmpl    *template.Template
	logger  log.Logger
	client  *http.Client
	retrier *notify.Retrier
	apiURL  string // for tests.
}

// New returns a new Pushover notifier.
func New(c *config.PushoverConfig, t *template.Template, l log.Logger, httpOpts ...commoncfg.HTTPClientOption) (*Notifier, error) {
	client, err := commoncfg.NewClientFromConfig(*c.HTTPConfig, "pushover", append(httpOpts, commoncfg.WithHTTP2Disabled())...)
	if err != nil {
		return nil, err
	}
	return &Notifier{
		conf:    c,
		tmpl:    t,
		logger:  l,
		client:  client,
		retrier: &notify.Retrier{},
		apiURL:  "https://api.pushover.net/1/messages.json",
	}, nil
}

// Notify implements the Notifier interface.
func (n *Notifier) Notify(ctx context.Context, as ...*types.Alert) (bool, error) {
	key, ok := notify.GroupKey(ctx)
	if !ok {
		return false, fmt.Errorf("group key missing")
	}
	data := notify.GetTemplateData(ctx, n.tmpl, as, n.logger)

	level.Debug(n.logger).Log("incident", key)

	var (
		err     error
		message string
	)
	tmpl := notify.TmplText(n.tmpl, data, &err)
	tmplHTML := notify.TmplHTML(n.tmpl, data, &err)

	parameters := url.Values{}
	parameters.Add("token", tmpl(string(n.conf.Token)))
	parameters.Add("user", tmpl(string(n.conf.UserKey)))

	title, truncated := notify.Truncate(tmpl(n.conf.Title), 250)
	if truncated {
		level.Debug(n.logger).Log("msg", "Truncated title", "truncated_title", title, "incident", key)
	}
	parameters.Add("title", title)

	if n.conf.HTML {
		parameters.Add("html", "1")
		message = tmplHTML(n.conf.Message)
	} else {
		message = tmpl(n.conf.Message)
	}

	message, truncated = notify.Truncate(message, 1024)
	if truncated {
		level.Debug(n.logger).Log("msg", "Truncated message", "truncated_message", message, "incident", key)
	}
	message = strings.TrimSpace(message)
	if message == "" {
		// Pushover rejects empty messages.
		message = "(no details)"
	}
	parameters.Add("message", message)

	supplementaryURL, truncated := notify.Truncate(tmpl(n.conf.URL), 512)
	if truncated {
		level.Debug(n.logger).Log("msg", "Truncated URL", "truncated_url", supplementaryURL, "incident", key)
	}
	parameters.Add("url", supplementaryURL)
	parameters.Add("url_title", tmpl(n.conf.URLTitle))

	parameters.Add("priority", tmpl(n.conf.Priority))
	parameters.Add("retry", fmt.Sprintf("%d", int64(time.Duration(n.conf.Retry).Seconds())))
	parameters.Add("expire", fmt.Sprintf("%d", int64(time.Duration(n.conf.Expire).Seconds())))
	parameters.Add("sound", tmpl(n.conf.Sound))
	if err != nil {
		return false, err
	}

	u, err := url.Parse(n.apiURL)
	if err != nil {
		return false, err
	}
	u.RawQuery = parameters.Encode()
	// Don't log the URL as it contains secret data (see #1825).
	level.Debug(n.logger).Log("msg", "Sending message", "incident", key)
	resp, err := notify.PostText(ctx, n.client, u.String(), nil)
	if err != nil {
		return true, notify.RedactURL(err)
	}
	defer notify.Drain(resp)

	return n.retrier.Check(resp.StatusCode, nil)
}
