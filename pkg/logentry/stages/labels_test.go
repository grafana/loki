package stages

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

var (
	lv1  = "lv1"
	lv2c = "l2"
	lv3  = ""
	lv3c = "l3"
)

func Test(t *testing.T) {
	tests := map[string]struct {
		config       LabelsConfig
		err          error
		expectedCfgs LabelsConfig
	}{
		"missing config": {
			config:       nil,
			err:          errors.New(ErrEmptyLabelStageConfig),
			expectedCfgs: nil,
		},
		"invalid label name": {
			config: LabelsConfig{
				"#*FDDS*": nil,
			},
			err:          fmt.Errorf(ErrInvalidLabelName, "#*FDDS*"),
			expectedCfgs: nil,
		},
		"label value is set from name": {
			config: LabelsConfig{
				"l1": &lv1,
				"l2": nil,
				"l3": &lv3,
			},
			err: nil,
			expectedCfgs: LabelsConfig{
				"l1": &lv1,
				"l2": &lv2c,
				"l3": &lv3c,
			},
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := validateLabelsConfig(test.config)
			if (err != nil) != (test.err != nil) {
				t.Errorf("validateLabelsConfig() expected error = %v, actual error = %v", test.err, err)
				return
			}
			if (err != nil) && (err.Error() != test.err.Error()) {
				t.Errorf("validateLabelsConfig() expected error = %v, actual error = %v", test.err, err)
				return
			}
			if test.expectedCfgs != nil {
				assert.Equal(t, test.expectedCfgs, test.config)
			}
		})
	}
}

//TODO test label processing
