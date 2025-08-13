package dataobj_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

func TestBuilder_preserve_section_version(t *testing.T) {
	builder := dataobj.NewBuilder(nil)
	err := builder.Append(fakeSectionBuilder{
		SectionType: dataobj.SectionType{
			Namespace: "github.com/grafana/loki",
			Kind:      "custom-section",
			Version:   42,
		},
	})
	require.NoError(t, err)

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	require.Len(t, obj.Sections(), 1, "expected only one section in the object")
	require.Equal(t, uint32(42), obj.Sections()[0].Type.Version, "expected section version to be preserved")
}

func TestBuilder_preserve_extension(t *testing.T) {
	builder := dataobj.NewBuilder(nil)
	err := builder.Append(fakeSectionBuilder{
		SectionType: dataobj.SectionType{Namespace: "github.com/grafana/loki", Kind: "logs"},
		FlushFunc: func(w dataobj.SectionWriter) (n int64, err error) {
			return w.WriteSection([]byte("test data"), []byte("test metadata"), []byte("test extension"))
		},
	})
	require.NoError(t, err)

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	require.Len(t, obj.Sections(), 1, "expected only one section in the object")
	require.Equal(t, []byte("test extension"), obj.Sections()[0].Reader.ExtensionData())
}

type fakeSectionBuilder struct {
	SectionType dataobj.SectionType
	FlushFunc   func(w dataobj.SectionWriter) (n int64, err error)
	ResetFunc   func()
}

var _ dataobj.SectionBuilder = (*fakeSectionBuilder)(nil)

func (fake fakeSectionBuilder) Type() dataobj.SectionType { return fake.SectionType }

func (fake fakeSectionBuilder) Flush(w dataobj.SectionWriter) (n int64, err error) {
	if fake.FlushFunc != nil {
		return fake.FlushFunc(w)
	}
	return w.WriteSection(nil, nil, nil)
}

func (fake fakeSectionBuilder) Reset() {
	if fake.ResetFunc != nil {
		fake.ResetFunc()
	}
}
