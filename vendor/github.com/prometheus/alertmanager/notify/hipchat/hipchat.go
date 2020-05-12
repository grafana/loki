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

package hipchat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-kit/kit/log"
	commoncfg "github.com/prometheus/common/config"

	"github.com/prometheus/alertmanager/config"
	"github.com/prometheus/alertmanager/notify"
	"github.com/prometheus/alertmanager/template"
	"github.com/prometheus/alertmanager/types"
)

// Notifier implements a Notifier for Hipchat notifications.
type Notifier struct {
	conf    *config.HipchatConfig
	tmpl    *template.Template
	logger  log.Logger
	client  *http.Client
	retrier *notify.Retrier
}

// New returns a new Hipchat notification handler.
func New(c *config.HipchatConfig, t *template.Template, l log.Logger) (*Notifier, error) {
	client, err := commoncfg.NewClientFromConfig(*c.HTTPConfig, "hipchat", false)
	if err != nil {
		return nil, err
	}
	return &Notifier{
		conf:   c,
		tmpl:   t,
		logger: l,
		client: client,
		// Response codes 429 (rate limiting) and 5xx can potentially recover.
		// 2xx response codes indicate successful requests.
		// https://developer.atlassian.com/hipchat/guide/hipchat-rest-api/api-response-codes
		retrier: &notify.Retrier{RetryCodes: []int{http.StatusTooManyRequests}},
	}, nil
}

type hipchatReq struct {
	From          string `json:"from"`
	Notify        bool   `json:"notify"`
	Message       string `json:"message"`
	MessageFormat string `json:"message_format"`
	Color         string `json:"color"`
}

// Notify implements the Notifier interface.
func (n *Notifier) Notify(ctx context.Context, as ...*types.Alert) (bool, error) {
	var err error
	var msg string
	var (
		data     = notify.GetTemplateData(ctx, n.tmpl, as, n.logger)
		tmplText = notify.TmplText(n.tmpl, data, &err)
		tmplHTML = notify.TmplHTML(n.tmpl, data, &err)
		roomid   = tmplText(n.conf.RoomID)
		apiURL   = n.conf.APIURL.Copy()
	)
	apiURL.Path += fmt.Sprintf("v2/room/%s/notification", roomid)
	q := apiURL.Query()
	q.Set("auth_token", string(n.conf.AuthToken))
	apiURL.RawQuery = q.Encode()

	if n.conf.MessageFormat == "html" {
		msg = tmplHTML(n.conf.Message)
	} else {
		msg = tmplText(n.conf.Message)
	}

	req := &hipchatReq{
		From:          tmplText(n.conf.From),
		Notify:        n.conf.Notify,
		Message:       msg,
		MessageFormat: n.conf.MessageFormat,
		Color:         tmplText(n.conf.Color),
	}
	if err != nil {
		return false, err
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(req); err != nil {
		return false, err
	}

	resp, err := notify.PostJSON(ctx, n.client, apiURL.String(), &buf)
	if err != nil {
		return true, notify.RedactURL(err)
	}
	defer notify.Drain(resp)

	return n.retrier.Check(resp.StatusCode, nil)
}
