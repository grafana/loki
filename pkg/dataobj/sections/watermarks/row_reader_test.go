package watermarks

import (
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRowReader(t *testing.T) {
	openSection := func(t *testing.T, build func(b *Builder)) *Section {
		t.Helper()

		obj := buildObject(t, build)
		sec, err := Open(t.Context(), obj.Sections()[0])
		require.NoError(t, err)
		return sec
	}

	t.Run("returns the same rows as the iterator", func(t *testing.T) {
		obj := buildObject(t, func(b *Builder) {
			require.NoError(t, b.Set("topic-a", 1, 11))
			require.NoError(t, b.Set("topic-a", 0, 10))
			require.NoError(t, b.Set("topic-b", 0, 20))
		})
		sec, err := Open(t.Context(), obj.Sections()[0])
		require.NoError(t, err)

		r := NewRowReader(sec)
		defer r.Close()
		require.NoError(t, r.Open(t.Context()))

		var got []Watermark
		buf := make([]Watermark, 2)
		for {
			n, err := r.Read(t.Context(), buf)
			got = append(got, buf[:n]...)
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)
		}

		require.Equal(t, iterAll(t, obj), got)
		require.Len(t, got, 3)
	})

	t.Run("returns EOF on an empty section", func(t *testing.T) {
		r := NewRowReader(openSection(t, func(*Builder) {}))
		defer r.Close()
		require.NoError(t, r.Open(t.Context()))

		n, err := r.Read(t.Context(), make([]Watermark, 4))
		require.Zero(t, n)
		require.ErrorIs(t, err, io.EOF)
	})

	t.Run("returns an error when read before Open", func(t *testing.T) {
		r := NewRowReader(openSection(t, func(b *Builder) { require.NoError(t, b.Set("topic", 0, 1)) }))
		defer r.Close()

		_, err := r.Read(t.Context(), make([]Watermark, 1))
		require.Error(t, err)
		require.NotErrorIs(t, err, io.EOF)
	})

	t.Run("returns an error when read after Close", func(t *testing.T) {
		r := NewRowReader(openSection(t, func(b *Builder) { require.NoError(t, b.Set("topic", 0, 1)) }))
		require.NoError(t, r.Open(t.Context()))
		require.NoError(t, r.Close())

		_, err := r.Read(t.Context(), make([]Watermark, 1))
		require.Error(t, err)
		require.NotErrorIs(t, err, io.EOF)
	})
}
