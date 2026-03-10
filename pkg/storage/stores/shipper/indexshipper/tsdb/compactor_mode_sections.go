package tsdb

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/compactor"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	tsdbindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/sectionref"
)

type compactionMode interface {
	isSidecarFile(name string) bool
	registerSource(sourceIndexSet compactor.IndexSet, sourceIndex storage.IndexFile) (modeSourceHandle, string, error)
	registerPath(tsdbPath string) (modeSourceHandle, error)
	addSeries(builder *Builder, source modeSourceHandle, lbls labels.Labels, fp model.Fingerprint, chks []tsdbindex.ChunkMeta) error
	releaseSource(source modeSourceHandle)
	writeCompactedSidecar(builder *Builder, tsdbPath string) error
	reset()
}

type modeSourceHandle uint32

type sectionRefCompactionMode struct {
	logger log.Logger
	mtx    sync.Mutex
	nextID modeSourceHandle
	refs   map[modeSourceHandle]sectionref.SectionRefLookup
}

var errSectionsSidecarMissing = errors.New("sections sidecar is missing")

func newCompactionMode(useSectionRefTable bool, logger log.Logger) compactionMode {
	if !useSectionRefTable {
		return nil
	}
	return &sectionRefCompactionMode{
		logger: logger,
		refs:   make(map[modeSourceHandle]sectionref.SectionRefLookup),
	}
}

func (m *sectionRefCompactionMode) isSidecarFile(name string) bool {
	return isSectionsFile(name)
}

func (m *sectionRefCompactionMode) registerSource(sourceIndexSet compactor.IndexSet, sourceIndex storage.IndexFile) (modeSourceHandle, string, error) {
	sectionsFileName, err := sectionsTableFileName(sourceIndex.Name)
	if err != nil {
		return 0, "", err
	}

	sectionsPath, err := sourceIndexSet.GetSourceFile(storage.IndexFile{Name: sectionsFileName})
	if err != nil {
		if errors.Is(err, compactor.ErrSourceFileNotFound) {
			return 0, "", fmt.Errorf("%w: %q for %q", errSectionsSidecarMissing, sectionsFileName, sourceIndex.Name)
		}
		return 0, "", fmt.Errorf("downloading sections table %q for %q: %w", sectionsFileName, sourceIndex.Name, err)
	}

	refs, err := sectionref.OpenMmap(sectionsPath)
	if err != nil {
		return 0, "", fmt.Errorf("mmap sections table %q: %w", sectionsPath, err)
	}
	id := m.storeRefs(refs)
	return id, sectionsPath, nil
}

func (m *sectionRefCompactionMode) registerPath(tsdbPath string) (modeSourceHandle, error) {
	sectionsPath, err := sectionsTablePath(tsdbPath)
	if err != nil {
		return 0, err
	}

	if _, err := os.Stat(sectionsPath); os.IsNotExist(err) {
		return 0, fmt.Errorf("section-ref-table mode enabled but sections file is missing for %q", tsdbPath)
	}

	refs, err := sectionref.OpenMmap(sectionsPath)
	if err != nil {
		return 0, fmt.Errorf("mmap sections table %q: %w", sectionsPath, err)
	}
	return m.storeRefs(refs), nil
}

func (m *sectionRefCompactionMode) addSeries(builder *Builder, source modeSourceHandle, lbls labels.Labels, fp model.Fingerprint, chks []tsdbindex.ChunkMeta) error {
	refs, err := m.refsForSource(source)
	if err != nil {
		return err
	}

	sectionMetas, err := toSectionMetas(chks, refs)
	if err != nil {
		return err
	}
	return builder.AddSeriesWithSectionRefs(lbls, fp, sectionMetas)
}

func (m *sectionRefCompactionMode) releaseSource(source modeSourceHandle) {
	m.mtx.Lock()
	refs, ok := m.refs[source]
	delete(m.refs, source)
	m.mtx.Unlock()
	if ok {
		if err := refs.Close(); err != nil {
			level.Warn(m.logger).Log("msg", "failed to close section-ref table", "source", source, "err", err)
		}
	}
}

func (m *sectionRefCompactionMode) writeCompactedSidecar(builder *Builder, tsdbPath string) error {
	sectionsTable, err := builder.SectionRefTableBytes()
	if err != nil {
		return err
	}
	if len(sectionsTable) == 0 {
		return nil
	}

	sectionsPath, err := sectionsTablePath(tsdbPath)
	if err != nil {
		return err
	}
	return os.WriteFile(sectionsPath, sectionsTable, 0o644)
}

func (m *sectionRefCompactionMode) storeRefs(refs sectionref.SectionRefLookup) modeSourceHandle {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.nextID++
	id := m.nextID
	m.refs[id] = refs
	return id
}

func (m *sectionRefCompactionMode) refsForSource(source modeSourceHandle) (sectionref.SectionRefLookup, error) {
	m.mtx.Lock()
	refs, ok := m.refs[source]
	m.mtx.Unlock()
	if !ok {
		return nil, fmt.Errorf("missing section references for source handle %d", source)
	}
	return refs, nil
}

func (m *sectionRefCompactionMode) reset() {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	for id, refs := range m.refs {
		if err := refs.Close(); err != nil {
			level.Warn(m.logger).Log("msg", "failed to close section-ref table during reset", "source", id, "err", err)
		}
	}
	m.refs = make(map[modeSourceHandle]sectionref.SectionRefLookup)
	m.nextID = 0
}

func sectionsTableFileName(tsdbFile string) (string, error) {
	switch {
	case strings.HasSuffix(tsdbFile, ".tsdb.gz"):
		return strings.TrimSuffix(tsdbFile, ".gz") + ".sections.gz", nil
	case strings.HasSuffix(tsdbFile, ".tsdb"):
		return tsdbFile + ".sections", nil
	default:
		return "", fmt.Errorf("invalid tsdb source file name %q", tsdbFile)
	}
}

func toSectionMetas(chks []tsdbindex.ChunkMeta, refs sectionref.SectionRefLookup) ([]sectionref.SectionMeta, error) {
	sectionMetas := make([]sectionref.SectionMeta, len(chks))
	for i, chk := range chks {
		ref, ok := refs.Lookup(chk.Checksum)
		if !ok {
			return nil, fmt.Errorf("missing section reference for checksum/index %d", chk.Checksum)
		}
		sectionMetas[i] = sectionref.SectionMeta{
			SectionRef: ref,
			ChunkMeta:  chk,
		}
	}
	return sectionMetas, nil
}

func isSectionsFile(name string) bool {
	return strings.HasSuffix(name, ".sections.gz") || strings.HasSuffix(name, ".sections")
}

func sectionsTablePath(tsdbPath string) (string, error) {
	switch {
	case strings.HasSuffix(tsdbPath, ".tsdb"):
		return tsdbPath + ".sections", nil
	case strings.HasSuffix(tsdbPath, ".tsdb.gz"):
		return strings.TrimSuffix(tsdbPath, ".gz") + ".sections.gz", nil
	default:
		return "", fmt.Errorf("invalid tsdb file path %q", tsdbPath)
	}
}

func Reset() {
}
