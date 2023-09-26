package cfg

import (
	"bytes"
	"flag"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaults checks that defaults are correctly obtained from a
// flagext.Registerer
func TestDefaults(t *testing.T) {
	data := Data{}
	fs := flag.NewFlagSet(t.Name(), flag.PanicOnError)

	err := Unmarshal(&data,
		Defaults(fs),
	)

	require.NoError(t, err)
	assert.Equal(t, Data{
		Verbose: false,
		Server: Server{
			Port:    80,
			Timeout: 60 * time.Second,
		},
		TLS: TLS{
			Cert: "DEFAULTCERT",
			Key:  "DEFAULTKEY",
		},
	}, data)
}

// TestFlags checks that defaults and flag values (they can't be separated) are
// correctly obtained from the command line
func TestFlags(t *testing.T) {
	data := Data{}
	fs := flag.NewFlagSet(t.Name(), flag.PanicOnError)
	err := Unmarshal(&data,
		Defaults(fs),
		dFlags(fs, []string{"-server.timeout=10h", "-verbose"}),
	)
	require.NoError(t, err)

	assert.Equal(t, Data{
		Verbose: true,
		Server: Server{
			Port:    80,
			Timeout: 10 * time.Hour,
		},
		TLS: TLS{
			Cert: "DEFAULTCERT",
			Key:  "DEFAULTKEY",
		},
	}, data)
}

// TestCategorizedUsage checks that the name of every flag can be "Titled" correctly
func TestCategorizedUsage(t *testing.T) {
	output := &bytes.Buffer{}
	fs := flag.NewFlagSet(t.Name(), flag.PanicOnError)
	fs.SetOutput(output)
	// "TestAPI" expected
	fs.String("testAPI.one", "", "")
	fs.String("testAPI.two", "", "")
	// "TESTapi" expected
	fs.String("tESTapi.one", "", "")
	fs.String("tESTapi.two", "", "")
	// "TestAPI" expected
	fs.String("TestAPI.one", "", "")
	fs.String("TestAPI.two", "", "")
	categorizedUsage(fs)()
	expected := "Usage of TestCategorizedUsage:\n\n TestAPI:\n   -TestAPI.one string:\n      \n   -TestAPI.two string:\n      \n\n TESTapi:\n   -tESTapi.one string:\n      \n   -tESTapi.two string:\n      \n\n TestAPI:\n   -testAPI.one string:\n      \n   -testAPI.two string:\n      \n\n"
	assert.Equal(t, expected, output.String())
}
