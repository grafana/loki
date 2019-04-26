package server

import (
	"context"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"

	"github.com/grafana/loki/pkg/promtail/server/ui"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/template"
)

func getTemplate(name string) (string, error) {
	var tmpl string

	appendf := func(name string) error {
		f, err := ui.Assets.Open(path.Join("/templates", name))
		if err != nil {
			return err
		}
		defer f.Close()
		b, err := ioutil.ReadAll(f)
		if err != nil {
			return err
		}
		tmpl += string(b)
		return nil
	}

	err := appendf("_base.html")
	if err != nil {
		return "", errors.Wrap(err, "error reading base template")
	}
	err = appendf(name)
	if err != nil {
		return "", errors.Wrapf(err, "error reading page template %s", name)
	}

	return tmpl, nil
}

func executeTemplate(ctx context.Context, w http.ResponseWriter, externalURL *url.URL, name string, data interface{}) {
	text, err := getTemplate(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	tmpl := template.NewTemplateExpander(
		ctx,
		text,
		name,
		data,
		model.Now(),
		nil,
		externalURL,
	)

	result, err := tmpl.ExpandHTML(nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	io.WriteString(w, result)
}
