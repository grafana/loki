package server

import (
	"context"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	template_text "text/template"
	"time"

	"github.com/grafana/loki/pkg/promtail/server/ui"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/template"
)

// templateOptions is a set of options to render a template.
type templateOptions struct {
	ExternalURL                   *url.URL
	Name, PageTitle, BuildVersion string
	Data                          interface{}
	TemplateFuncs                 template_text.FuncMap
}

// tmplFuncs create a default template function for a given template options
func (opts templateOptions) tmplFuncs() template_text.FuncMap {
	return template_text.FuncMap{
		"since": func(t time.Time) time.Duration {
			return time.Since(t) / time.Millisecond * time.Millisecond
		},
		"pathPrefix":   func() string { return opts.ExternalURL.Path },
		"pageTitle":    func() string { return opts.PageTitle },
		"buildVersion": func() string { return opts.BuildVersion },
	}
}

// executeTemplate execute a template and write result to the http.ResponseWriter
func executeTemplate(ctx context.Context, w http.ResponseWriter, tmplOpts templateOptions) {
	text, err := getTemplate(tmplOpts.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	tmpl := template.NewTemplateExpander(
		ctx,
		text,
		tmplOpts.Name,
		tmplOpts.Data,
		model.Now(),
		nil,
		tmplOpts.ExternalURL,
	)

	tmpl.Funcs(tmplOpts.tmplFuncs())
	tmpl.Funcs(tmplOpts.TemplateFuncs)

	result, err := tmpl.ExpandHTML(nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = io.WriteString(w, result)
}

func getTemplate(name string) (string, error) {
	var tmpl string

	appendf := func(name string) error {
		f, err := ui.Assets.Open(path.Join("/templates", name))
		if err != nil {
			return err
		}
		defer func() {
			_ = f.Close()
		}()
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
