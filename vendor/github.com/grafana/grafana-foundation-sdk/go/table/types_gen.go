// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package table

import (
	"encoding/json"

	variants "github.com/grafana/grafana-foundation-sdk/go/cog/variants"
	common "github.com/grafana/grafana-foundation-sdk/go/common"
	dashboard "github.com/grafana/grafana-foundation-sdk/go/dashboard"
)

type Options struct {
	// Represents the index of the selected frame
	FrameIndex float64 `json:"frameIndex"`
	// Controls whether the panel should show the header
	ShowHeader bool `json:"showHeader"`
	// Controls whether the header should show icons for the column types
	ShowTypeIcons *bool `json:"showTypeIcons,omitempty"`
	// Used to control row sorting
	SortBy []common.TableSortByFieldState `json:"sortBy,omitempty"`
	// Controls footer options
	Footer *common.TableFooterOptions `json:"footer,omitempty"`
	// Controls the height of the rows
	CellHeight *common.TableCellHeight `json:"cellHeight,omitempty"`
}

func (resource Options) Equals(other Options) bool {
	if resource.FrameIndex != other.FrameIndex {
		return false
	}
	if resource.ShowHeader != other.ShowHeader {
		return false
	}
	if resource.ShowTypeIcons == nil && other.ShowTypeIcons != nil || resource.ShowTypeIcons != nil && other.ShowTypeIcons == nil {
		return false
	}

	if resource.ShowTypeIcons != nil {
		if *resource.ShowTypeIcons != *other.ShowTypeIcons {
			return false
		}
	}

	if len(resource.SortBy) != len(other.SortBy) {
		return false
	}

	for i1 := range resource.SortBy {
		if !resource.SortBy[i1].Equals(other.SortBy[i1]) {
			return false
		}
	}
	if resource.Footer == nil && other.Footer != nil || resource.Footer != nil && other.Footer == nil {
		return false
	}

	if resource.Footer != nil {
		if !resource.Footer.Equals(*other.Footer) {
			return false
		}
	}
	if resource.CellHeight == nil && other.CellHeight != nil || resource.CellHeight != nil && other.CellHeight == nil {
		return false
	}

	if resource.CellHeight != nil {
		if *resource.CellHeight != *other.CellHeight {
			return false
		}
	}

	return true
}

type FieldConfig = common.TableFieldOptions

func VariantConfig() variants.PanelcfgConfig {
	return variants.PanelcfgConfig{
		Identifier: "table",
		OptionsUnmarshaler: func(raw []byte) (any, error) {
			options := &Options{}

			if err := json.Unmarshal(raw, options); err != nil {
				return nil, err
			}

			return options, nil
		},
		FieldConfigUnmarshaler: func(raw []byte) (any, error) {
			fieldConfig := &FieldConfig{}

			if err := json.Unmarshal(raw, fieldConfig); err != nil {
				return nil, err
			}

			return fieldConfig, nil
		},
		GoConverter: func(inputPanel any) string {
			if panel, ok := inputPanel.(*dashboard.Panel); ok {
				return PanelConverter(*panel)
			}

			return PanelConverter(inputPanel.(dashboard.Panel))
		},
	}
}
