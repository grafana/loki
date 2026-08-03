package cfg

import (
	"flag"
	"os"
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"
)

func TestNonStrictYAMLIgnoresUnknownFields(t *testing.T) {
	y := []byte(`
verbose: true
server:
  port: 8080
typo_field: value
`)

	var data Data
	u := &UnknownFields{}
	err := dYAML(y, false, u)(&data)

	require.NoError(t, err)
	require.True(t, data.Verbose)
	require.Equal(t, 8080, data.Server.Port)
	require.Equal(t, []string{"typo_field"}, u.List())
}

func TestDYAMLCollectsUnknownFields(t *testing.T) {
	y := []byte(`
verbose: true
server:
  port: 2000
  bogus: 1
unknown_top:
  nested_a: 1
  nested_b: 2
`)

	t.Run("non-strict collects unknown fields and does not fail", func(t *testing.T) {
		var data Data
		u := &UnknownFields{}
		err := dYAML(y, false, u)(&data)
		require.NoError(t, err)

		// Known fields are still applied.
		require.True(t, data.Verbose)
		require.Equal(t, 2000, data.Server.Port)

		// A nested unknown object counts once (unknown_top), plus the scalar bogus.
		require.ElementsMatch(t, []string{"bogus", "unknown_top"}, u.List())
		require.Equal(t, 2, u.Len())
	})

	t.Run("strict with collector defers error and still collects", func(t *testing.T) {
		var data Data
		u := &UnknownFields{}
		// With a collector present, unknown fields are recorded rather than
		// returned; strictness is enforced centrally by DynamicUnmarshal.
		err := dYAML(y, true, u)(&data)
		require.NoError(t, err)
		require.Equal(t, 2, u.Len())
	})

	t.Run("real type errors still fail even with collector", func(t *testing.T) {
		var data Data
		u := &UnknownFields{}
		bad := []byte(`
server:
  port: not-a-number
`)
		err := dYAML(bad, false, u)(&data)
		require.Error(t, err)
		require.Equal(t, 0, u.Len())
	})
}

func TestUnknownFieldName(t *testing.T) {
	for _, tc := range []struct {
		msg  string
		want string
		ok   bool
	}{
		{"field bogus not found in type cfg.Server", "bogus", true},
		{"field unknown_top not found in type cfg.Data", "unknown_top", true},
		{"cannot unmarshal !!str into int", "", false},
		{"field already set in type cfg.Data", "", false},
	} {
		name, ok := unknownFieldName(tc.msg)
		require.Equal(t, tc.ok, ok, tc.msg)
		require.Equal(t, tc.want, name, tc.msg)
	}
}

// strictData wraps Data with a toggleable strict flag to exercise the
// StrictParser enforcement path in DynamicUnmarshal.
type strictData struct {
	Data       `yaml:",inline"`
	ConfigFile string
	Strict     bool
}

func (d *strictData) Clone() flagext.Registerer {
	return func(d strictData) *strictData { return &d }(*d)
}

func (d *strictData) RegisterFlags(fs *flag.FlagSet) {
	d.Data.RegisterFlags(fs)
	fs.StringVar(&d.ConfigFile, "config.file", "", "")
	fs.BoolVar(&d.Strict, "config.strict", true, "")
}

func (d *strictData) StrictConfig() bool         { return d.Strict }
func (d *strictData) ApplyDynamicConfig() Source { return func(_ Cloneable) error { return nil } }

func TestDynamicUnmarshalStrictEnforcement(t *testing.T) {
	config := `
verbose: true
bogus_field: 1
`
	writeConfig := func(t *testing.T) string {
		f, err := os.CreateTemp(t.TempDir(), "config.yaml")
		require.NoError(t, err)
		_, err = f.WriteString(config)
		require.NoError(t, err)
		require.NoError(t, f.Close())
		return f.Name()
	}

	t.Run("strict makes unknown fields fatal", func(t *testing.T) {
		var d strictData
		fs := flag.NewFlagSet(t.Name(), flag.ContinueOnError)
		args := []string{"-config.file", writeConfig(t), "-config.strict=true"}
		u, err := DynamicUnmarshal(&d, args, fs)
		require.Error(t, err)
		require.Contains(t, err.Error(), "bogus_field")
		require.Equal(t, 1, u.Len())
	})

	t.Run("non-strict collects unknown fields without error", func(t *testing.T) {
		var d strictData
		fs := flag.NewFlagSet(t.Name(), flag.ContinueOnError)
		args := []string{"-config.file", writeConfig(t), "-config.strict=false"}
		u, err := DynamicUnmarshal(&d, args, fs)
		require.NoError(t, err)
		require.Equal(t, []string{"bogus_field"}, u.List())
		require.True(t, d.Verbose)
	})
}
