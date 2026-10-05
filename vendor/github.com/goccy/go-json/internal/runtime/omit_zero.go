//go:build go1.24

package runtime

// omitZeroOption is whether the omitzero option of a field is honored: encoding/json has it from Go 1.24.
const omitZeroOption = true
