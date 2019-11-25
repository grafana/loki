package positions

import (
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func tempFilename(t *testing.T) string {
	t.Helper()

	temp, err := ioutil.TempFile("", "positions")
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
  /tmp/random.log: "17623"
`)
	err := ioutil.WriteFile(temp, yaml, 0644)
	if err != nil {
		t.Fatal(err)
	}

	pos, err := readPositionsFile(temp)
	require.NoError(t, err)
	require.Equal(t, "17623", pos["/tmp/random.log"])
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

	_, err = readPositionsFile(temp)
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
	err := ioutil.WriteFile(temp, badYaml, 0644)
	if err != nil {
		t.Fatal(err)
	}

	_, err = readPositionsFile(temp)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), temp)) // error must contain filename
}
