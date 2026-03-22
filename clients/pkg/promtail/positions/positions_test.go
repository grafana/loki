package positions

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func tempFilename(t *testing.T) string {
	t.Helper()

	temp, err := os.CreateTemp("", "positions")
	if err != nil {
		t.Fatal("tempFilename:", err)
	}
	err = temp.Close()
	if err != nil {
		t.Fatal("tempFilename:", err)
	}

	name := temp.Name()
	err = os.Remove(name)
	if err != nil {
		t.Fatal("tempFilename:", err)
	}

	return name
}

func TestReadPositionsOK(t *testing.T) {
	temp := tempFilename(t)
	defer func() {
		_ = os.Remove(temp)
	}()

	yaml := []byte(`positions:
  /log/path/random.log: "17623"
`)
	err := os.WriteFile(temp, yaml, 0644)
	if err != nil {
		t.Fatal(err)
	}

	pos, err := readPositionsFile(Config{
		PositionsFile: temp,
	}, log.NewNopLogger())

	require.NoError(t, err)
	require.Equal(t, "17623", pos["/log/path/random.log"])
}

func TestReadPositionsEmptyFile(t *testing.T) {
	temp := tempFilename(t)
	defer func() {
		_ = os.Remove(temp)
	}()

	yaml := []byte(``)
	err := os.WriteFile(temp, yaml, 0644)
	if err != nil {
		t.Fatal(err)
	}

	pos, err := readPositionsFile(Config{
		PositionsFile: temp,
	}, log.NewNopLogger())

	require.NoError(t, err)
	require.NotNil(t, pos)
}

func TestReadPositionsFromDir(t *testing.T) {
	temp := tempFilename(t)
	err := os.Mkdir(temp, 0644)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		_ = os.Remove(temp)
	}()

	_, err = readPositionsFile(Config{
		PositionsFile: temp,
	}, log.NewNopLogger())

	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), temp)) // error must contain filename
}

func TestReadPositionsFromBadYaml(t *testing.T) {
	temp := tempFilename(t)
	defer func() {
		_ = os.Remove(temp)
	}()

	badYaml := []byte(`positions:
  /tmp/random.log: "176
`)
	err := os.WriteFile(temp, badYaml, 0644)
	if err != nil {
		t.Fatal(err)
	}

	_, err = readPositionsFile(Config{
		PositionsFile: temp,
	}, log.NewNopLogger())

	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), temp)) // error must contain filename
}

func TestReadPositionsFromBadYamlIgnoreCorruption(t *testing.T) {
	temp := tempFilename(t)
	defer func() {
		_ = os.Remove(temp)
	}()

	badYaml := []byte(`positions:
  /tmp/random.log: "176
`)
	err := os.WriteFile(temp, badYaml, 0644)
	if err != nil {
		t.Fatal(err)
	}

	out, err := readPositionsFile(Config{
		PositionsFile:     temp,
		IgnoreInvalidYaml: true,
	}, log.NewNopLogger())

	require.NoError(t, err)
	require.Equal(t, map[string]string{}, out)
}

func Test_ReadOnly(t *testing.T) {
	temp := tempFilename(t)
	defer func() {
		_ = os.Remove(temp)
	}()
	yaml := []byte(`positions:
  /log/path/random.log: "17623"
`)
	err := os.WriteFile(temp, yaml, 0644)
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(util_log.Logger, Config{
		SyncPeriod:    20 * time.Nanosecond,
		PositionsFile: temp,
		ReadOnly:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	p.Put("/foo/bar/f", 12132132)
	p.PutString("/foo/f", "100")
	pos, err := p.Get("/log/path/random.log")
	if err != nil {
		t.Fatal(err)
	}
	require.Equal(t, int64(17623), pos)
	p.(*positions).save()
	out, err := readPositionsFile(Config{
		PositionsFile:     temp,
		IgnoreInvalidYaml: true,
		ReadOnly:          true,
	}, log.NewNopLogger())

	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"/log/path/random.log": "17623",
	}, out)

}
