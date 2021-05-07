package alertspb

import "errors"

var (
	ErrNotFound = errors.New("alertmanager storage object not found")
)

// ToProto transforms a yaml Alertmanager config and map of template files to an AlertConfigDesc
func ToProto(cfg string, templates map[string]string, user string) AlertConfigDesc {
	tmpls := []*TemplateDesc{}
	for fn, body := range templates {
		tmpls = append(tmpls, &TemplateDesc{
			Body:     body,
			Filename: fn,
		})
	}
	return AlertConfigDesc{
		User:      user,
		RawConfig: cfg,
		Templates: tmpls,
	}
}

// ParseTemplates returns a alertmanager config object
func ParseTemplates(cfg AlertConfigDesc) map[string]string {
	templates := map[string]string{}
	for _, t := range cfg.Templates {
		templates[t.Filename] = t.Body
	}
	return templates
}
