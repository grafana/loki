// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package variants

import "reflect"

type PanelcfgConfig struct {
	Identifier             string
	OptionsUnmarshaler     func(raw []byte) (any, error)
	FieldConfigUnmarshaler func(raw []byte) (any, error)
	GoConverter            func(inputPanel any) string
}

type DataqueryConfig struct {
	Identifier           string
	DataqueryUnmarshaler func(raw []byte) (Dataquery, error)
	GoConverter          func(inputPanel any) string
}

type Dataquery interface {
	ImplementsDataqueryVariant()
	Equals(other Dataquery) bool
	DataqueryType() string
}

type Panelcfg interface {
	ImplementsPanelcfgVariant()
}

type UnknownDataquery map[string]any

func (unknown UnknownDataquery) DataqueryType() string {
	return "unknown"
}

func (unknown UnknownDataquery) ImplementsDataqueryVariant() {}

func (unknown UnknownDataquery) Equals(otherCandidate Dataquery) bool {
	if otherCandidate == nil {
		return false
	}

	other, ok := otherCandidate.(UnknownDataquery)
	if !ok {
		return false
	}

	if len(unknown) != len(other) {
		return false
	}

	for key := range unknown {
		// TODO: is DeepEqual good enough here?
		if !reflect.DeepEqual(unknown[key], other[key]) {
			return false
		}
	}

	return true
}
