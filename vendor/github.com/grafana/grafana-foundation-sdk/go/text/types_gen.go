// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package text

import (
	"encoding/json"

	variants "github.com/grafana/grafana-foundation-sdk/go/cog/variants"
	dashboard "github.com/grafana/grafana-foundation-sdk/go/dashboard"
)

type TextMode string

const (
	TextModeHTML     TextMode = "html"
	TextModeMarkdown TextMode = "markdown"
	TextModeCode     TextMode = "code"
)

type CodeLanguage string

const (
	CodeLanguageJson       CodeLanguage = "json"
	CodeLanguageYaml       CodeLanguage = "yaml"
	CodeLanguageXml        CodeLanguage = "xml"
	CodeLanguageTypescript CodeLanguage = "typescript"
	CodeLanguageSql        CodeLanguage = "sql"
	CodeLanguageGo         CodeLanguage = "go"
	CodeLanguageMarkdown   CodeLanguage = "markdown"
	CodeLanguageHtml       CodeLanguage = "html"
	CodeLanguagePlaintext  CodeLanguage = "plaintext"
)

type CodeOptions struct {
	// The language passed to monaco code editor
	Language        CodeLanguage `json:"language"`
	ShowLineNumbers bool         `json:"showLineNumbers"`
	ShowMiniMap     bool         `json:"showMiniMap"`
}

func (resource CodeOptions) Equals(other CodeOptions) bool {
	if resource.Language != other.Language {
		return false
	}
	if resource.ShowLineNumbers != other.ShowLineNumbers {
		return false
	}
	if resource.ShowMiniMap != other.ShowMiniMap {
		return false
	}

	return true
}

type Options struct {
	Mode    TextMode     `json:"mode"`
	Code    *CodeOptions `json:"code,omitempty"`
	Content string       `json:"content"`
}

func (resource Options) Equals(other Options) bool {
	if resource.Mode != other.Mode {
		return false
	}
	if resource.Code == nil && other.Code != nil || resource.Code != nil && other.Code == nil {
		return false
	}

	if resource.Code != nil {
		if !resource.Code.Equals(*other.Code) {
			return false
		}
	}
	if resource.Content != other.Content {
		return false
	}

	return true
}

func VariantConfig() variants.PanelcfgConfig {
	return variants.PanelcfgConfig{
		Identifier: "text",
		OptionsUnmarshaler: func(raw []byte) (any, error) {
			options := &Options{}

			if err := json.Unmarshal(raw, options); err != nil {
				return nil, err
			}

			return options, nil
		},
		GoConverter: func(inputPanel any) string {
			if panel, ok := inputPanel.(*dashboard.Panel); ok {
				return PanelConverter(*panel)
			}

			return PanelConverter(inputPanel.(dashboard.Panel))
		},
	}
}
