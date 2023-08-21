package cloudflare

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFields(t *testing.T) {
	tests := []struct {
		name             string
		fieldsType       FieldsType
		additionalFields []string
		expected         []string
	}{
		{
			name:             "Default fields",
			fieldsType:       "default",
			additionalFields: []string{},
			expected:         defaultFields,
		},
		{
			name:             "Custom fields",
			fieldsType:       FieldsTypeCustom,
			additionalFields: []string{"ClientIP", "OriginResponseBytes"},
			expected:         []string{"ClientIP", "OriginResponseBytes"},
		},
		{
			name:             "Default fields with added custom fields",
			fieldsType:       FieldsTypeDefault,
			additionalFields: []string{"WAFFlags", "WAFMatchedVar"},
			expected:         append(defaultFields, "WAFFlags", "WAFMatchedVar"),
		},
		{
			name:             "Default fields with duplicated custom fields",
			fieldsType:       FieldsTypeDefault,
			additionalFields: []string{"WAFFlags", "WAFFlags", "ClientIP"},
			expected:         append(defaultFields, "WAFFlags"), // clientIP is already part of defaultFields
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := Fields(test.fieldsType, test.additionalFields)
			assert.NoError(t, err)
			assert.ElementsMatch(t, test.expected, result)
		})
	}
}
