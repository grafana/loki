package metastore

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

func TestGoRecovering(t *testing.T) {
	t.Run("returns an error with the panic value when a function panics, and lets the others finish", func(t *testing.T) {
		var (
			group    errgroup.Group
			finished = make(chan struct{})
		)

		goRecovering(t.Context(), &group, func() error { panic("corrupt page") })
		goRecovering(t.Context(), &group, func() error {
			close(finished)
			return nil
		})

		require.EqualError(t, group.Wait(), "reading the sections of an index object: corrupt page")
		<-finished
	})

	t.Run("guards the goroutines that open the readers of the postings sections", func(t *testing.T) {
		selector := newStreamSelector([]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}, nil, now, now, nil)

		err := selector.open(t.Context(), []*postings.Section{nil}, 1)

		require.ErrorContains(t, err, "reading the sections of an index object")
	})

	t.Run("guards the goroutines that scan the postings sections for predicate admissions", func(t *testing.T) {
		selector := newStreamSelector(nil, []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "detected_level", "error")}, now, now, nil)
		selector.opened = true
		selector.scanners = []*postings.Scanner{nil}

		_, err := selector.scanPredicateAdmissions(t.Context())

		require.ErrorContains(t, err, "reading the sections of an index object")
	})

	t.Run("guards the goroutines that open the postings sections", func(t *testing.T) {
		_, err := openPostingsSectionsConcurrently(t.Context(), []*dataobj.Section{nil})

		require.ErrorContains(t, err, "reading the sections of an index object")
	})
}
