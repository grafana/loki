package builder

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// loghubSystem is one Loghub dataset used as a source of real application log
// lines. member selects the log file inside the archive: an exact name for
// tarballs, a suffix for zips (every matching member is read in name order).
type loghubSystem struct {
	archive   string
	member    string
	namespace string
	app       string
}

// loghubSystems are the application-style datasets of the Loghub collection
// (He et al., https://github.com/logpai/loghub), Zenodo record 8196385,
// licensed CC-BY-4.0.
var loghubSystems = []loghubSystem{
	{"Apache.tar.gz", "Apache.log", "web", "apache"},
	{"SSH.tar.gz", "SSH.log", "infra", "sshd"},
	{"OpenStack.tar.gz", "openstack_normal1.log", "cloud", "nova"},
	{"Hadoop.zip", ".log", "data", "hadoop-mr"},
	{"Zookeeper.tar.gz", "Zookeeper.log", "data", "zookeeper"},
	{"HealthApp.tar.gz", "HealthApp.log", "mobile", "healthapp"},
	{"Proxifier.tar.gz", "Proxifier.log", "infra", "proxifier"},
	{"Linux.tar.gz", "Linux.log", "infra", "syslog"},
	{"Mac.tar.gz", "Mac.log", "desktop", "macos"},
}

const loghubBaseURL = "https://zenodo.org/records/8196385/files/"

// TestGenerateLoghubCapture writes a capture file of real application logs.
// Lines come from the Loghub datasets in file order (no line is repeated as
// long as each system has enough lines for its share), interleaved across
// systems the way a Kafka partition interleaves streams. Each record is one
// stream push of 10-100 consecutive lines from one pod of one system.
//
// Timestamps follow the production shape described in CLAUDE.md: ~90% of
// records are within the last minute before GEN_NOW and ~10% are scattered
// over the previous 7 days. Every choice is driven by an AES-CTR keystream, so
// the output is deterministic for a given GEN_NOW and archive set.
//
//	GEN_LOGHUB=1 go test ./pkg/logline/builder/ -run TestGenerateLoghubCapture -v
//
// Env: GEN_OUT (path), GEN_MB (target value MiB, default 20),
// LOGHUB_DIR (archive cache, default testdata/loghub; missing archives are
// downloaded), GEN_NOW (RFC 3339 reference time, default genDefaultNow).
func TestGenerateLoghubCapture(t *testing.T) {
	if os.Getenv("GEN_LOGHUB") == "" {
		t.Skip("Set GEN_LOGHUB=1 to generate the Loghub capture")
	}
	out := envOr("GEN_OUT", "testdata/kafka_records_loghub20.bin")
	targetBytes := int64(envInt("GEN_MB", 20)) << 20
	dir := envOr("LOGHUB_DIR", "testdata/loghub")
	now := genNow(t)

	// Each system needs at most its equal share of the target, plus slack for
	// the uneven keystream-driven interleaving.
	perSystem := 2*targetBytes/int64(len(loghubSystems)) + (1 << 20)
	sources := make([][]string, len(loghubSystems))
	for i, sys := range loghubSystems {
		path := filepath.Join(dir, sys.archive)
		require.NoError(t, fetchLoghubArchive(path, loghubBaseURL+sys.archive+"?download=1"))
		lines, err := readLoghubLines(path, sys.member, perSystem)
		require.NoError(t, err, "read %s", sys.archive)
		require.NotEmpty(t, lines, "no lines in %s", sys.archive)
		sources[i] = lines
		t.Logf("%-18s %7d lines loaded", sys.archive, len(lines))
	}

	var key, iv [16]byte
	copy(key[:], "logline-loghub!!")
	block, err := aes.NewCipher(key[:])
	require.NoError(t, err)
	ctr := cipher.NewCTR(block, iv[:])
	ks := make([]byte, 1<<16)
	zeros := make([]byte, len(ks))
	ksPos := len(ks)
	nextU64 := func() uint64 {
		if ksPos+8 > len(ks) {
			ctr.XORKeyStream(ks, zeros)
			ksPos = 0
		}
		v := binary.LittleEndian.Uint64(ks[ksPos:])
		ksPos += 8
		return v
	}

	const podsPerSystem = 4
	labelSets := make([][]string, len(loghubSystems))
	for i, sys := range loghubSystems {
		for p := range podsPerSystem {
			pod := fmt.Sprintf("%s-%08x-%05x", sys.app, nextU64()&0xffffffff, nextU64()&0xfffff)
			labelSets[i] = append(labelSets[i], fmt.Sprintf(
				`{namespace=%q, app=%q, pod=%q, container=%q, instance="%d"}`,
				sys.namespace, sys.app, pod, sys.app, p))
		}
	}

	f, err := os.Create(out)
	require.NoError(t, err)
	w := bufio.NewWriterSize(f, 1<<20)
	require.NoError(t, binary.Write(w, binary.LittleEndian, uint32(0)))

	const (
		recentSpan = int64(time.Minute)
		tailSpan   = int64(7 * 24 * time.Hour)
	)
	next := make([]int, len(loghubSystems))
	var (
		valueBytes  int64
		recordCount uint32
		entryCount  int
		textBytes   int64
		wrapped     = make([]bool, len(loghubSystems))
	)
	for valueBytes < targetBytes {
		sys := int(nextU64() % uint64(len(loghubSystems)))
		labels := labelSets[sys][nextU64()%podsPerSystem]
		n := 10 + int(nextU64()%91)

		var base int64
		if nextU64()%10 == 0 {
			base = now.UnixNano() - int64(nextU64()%uint64(tailSpan))
		} else {
			base = now.UnixNano() - int64(nextU64()%uint64(recentSpan))
		}

		entries := make([]logproto.Entry, n)
		ts := base
		for i := range n {
			src := sources[sys]
			if next[sys] >= len(src) {
				next[sys] = 0
				wrapped[sys] = true
			}
			line := src[next[sys]]
			next[sys]++
			entries[i] = logproto.Entry{Timestamp: time.Unix(0, ts).UTC(), Line: line}
			ts += int64(nextU64() % uint64(5*time.Millisecond))
			textBytes += int64(len(line))
		}
		stream := logproto.Stream{Labels: labels, Entries: entries}
		val, err := stream.Marshal()
		require.NoError(t, err)

		require.NoError(t, binary.Write(w, binary.LittleEndian, now.UnixNano()))
		require.NoError(t, binary.Write(w, binary.LittleEndian, uint32(len(val))))
		_, err = w.Write(val)
		require.NoError(t, err)

		valueBytes += int64(len(val))
		recordCount++
		entryCount += n
	}
	require.NoError(t, w.Flush())
	_, err = f.Seek(0, 0)
	require.NoError(t, err)
	require.NoError(t, binary.Write(f, binary.LittleEndian, recordCount))
	require.NoError(t, f.Close())

	for i, sys := range loghubSystems {
		require.False(t, wrapped[i], "%s ran out of lines; raise perSystem", sys.archive)
	}
	t.Logf("wrote %s: %d records, %d entries, %.1f MiB of values, %.1f MiB of text",
		out, recordCount, entryCount, float64(valueBytes)/(1<<20), float64(textBytes)/(1<<20))
}

func fetchLoghubArchive(path, url string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	resp, err := http.Get(url) //nolint:gosec,noctx // fixed dataset URL, test-only generator
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	tmp := path + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// readLoghubLines returns the non-empty lines of the selected archive member(s)
// in order, stopping once maxBytes of line text has been collected.
func readLoghubLines(path, member string, maxBytes int64) ([]string, error) {
	var lines []string
	var total int64
	collect := func(r io.Reader) error {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() && total < maxBytes {
			line := strings.TrimRight(sc.Text(), "\r")
			if line == "" {
				continue
			}
			lines = append(lines, line)
			total += int64(len(line))
		}
		return sc.Err()
	}

	if strings.HasSuffix(path, ".zip") {
		zr, err := zip.OpenReader(path)
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		files := slices.Clone(zr.File)
		slices.SortFunc(files, func(a, b *zip.File) int { return strings.Compare(a.Name, b.Name) })
		for _, zf := range files {
			if total >= maxBytes {
				break
			}
			if !strings.HasSuffix(zf.Name, member) {
				continue
			}
			rc, err := zf.Open()
			if err != nil {
				return nil, err
			}
			err = collect(rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
		}
		return lines, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("member %s not found", member)
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(hdr.Name) == member {
			return lines, collect(tr)
		}
	}
}
