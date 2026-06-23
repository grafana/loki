package postings_test

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// schemaNames and schemaValues are the shared label-name and value space the
// generator and randomMatchers both draw from, so generated data and generated
// matchers overlap and produce both hits and misses.
var (
	schemaNames  = []string{"svc", "app", "job", "namespace", "tenant"}
	schemaValues = []string{"loki", "worker", "distributor", "dev", "prod", "29", "2914"}
)

// generateSection creates a random section with the given number of streams,
// returning both the labelPostings for the fixture builder and a ground-truth
// map of streamID -> label name -> label value for oracle matching.
func generateSection(rng *rand.Rand, nStreams int) ([]labelPosting, map[int64]map[string]string) {
	names := schemaNames
	values := schemaValues

	groundTruth := make(map[int64]map[string]string)
	var labelPostings []labelPosting

	for streamID := int64(0); streamID < int64(nStreams); streamID++ {
		groundTruth[streamID] = make(map[string]string)

		// Always assign "svc" to ensure a positive matcher can always seed.
		svcValue := values[rng.Intn(len(values))]
		groundTruth[streamID]["svc"] = svcValue
		labelPostings = append(labelPostings, labelPosting{
			name:     "svc",
			value:    svcValue,
			streamID: streamID,
			obj:      "obj-0",
			section:  0,
			minTs:    int64(rng.Intn(1000)),
			maxTs:    int64(rng.Intn(1000) + 1000),
		})

		// Assign other names with ~70% probability.
		for _, name := range names[1:] {
			if rng.Float32() < 0.7 {
				value := values[rng.Intn(len(values))]
				groundTruth[streamID][name] = value
				labelPostings = append(labelPostings, labelPosting{
					name:     name,
					value:    value,
					streamID: streamID,
					obj:      "obj-0",
					section:  0,
					minTs:    int64(rng.Intn(1000)),
					maxTs:    int64(rng.Intn(1000) + 1000),
				})
			}
		}
	}

	return labelPostings, groundTruth
}

// oracleMatch applies matcher semantics: for each stream in the ground truth,
// check whether all matchers match it. A matcher matches a stream iff:
// - the stream has the label and m.Matches(value) returns true, OR
// - the stream lacks the label and m.Matches("") returns true.
func oracleMatch(streams map[int64]map[string]string, matchers []*labels.Matcher) map[int64]struct{} {
	result := make(map[int64]struct{})
	for streamID, streamLabels := range streams {
		matchAll := true
		for _, m := range matchers {
			value := ""
			if v, ok := streamLabels[m.Name]; ok {
				value = v
			}
			if !m.Matches(value) {
				matchAll = false
				break
			}
		}
		if matchAll {
			result[streamID] = struct{}{}
		}
	}
	return result
}

// randomMatchers generates 1..4 matchers that always includes a positive
// svc=<value> matcher, plus random MatchEqual/MatchNotEqual/MatchRegexp on
// other names.
func randomMatchers(rng *rand.Rand, nMatchers int) []*labels.Matcher {
	if nMatchers < 1 {
		nMatchers = 1
	}
	if nMatchers > 4 {
		nMatchers = 4
	}

	names := schemaNames
	values := schemaValues
	matchTypes := []labels.MatchType{labels.MatchEqual, labels.MatchNotEqual, labels.MatchRegexp, labels.MatchNotRegexp}

	result := make([]*labels.Matcher, 0, nMatchers)

	// Always include a positive svc matcher.
	svcValue := values[rng.Intn(len(values))]
	result = append(result, labels.MustNewMatcher(labels.MatchEqual, "svc", svcValue))

	// Generate remaining matchers.
	for i := 1; i < nMatchers; i++ {
		name := names[rng.Intn(len(names))]
		matchType := matchTypes[rng.Intn(len(matchTypes))]

		var value string
		if matchType == labels.MatchRegexp || matchType == labels.MatchNotRegexp {
			regexes := []string{"lo.*", ".+", "wor.*", "dev.*", "prod.*"}
			value = regexes[rng.Intn(len(regexes))]
		} else {
			// MatchEqual or MatchNotEqual: use schema values or empty.
			if rng.Float32() < 0.3 {
				value = ""
			} else {
				value = values[rng.Intn(len(values))]
			}
		}

		m, err := labels.NewMatcher(matchType, name, value)
		if err != nil {
			// If regex is invalid, skip it and try again.
			i--
			continue
		}
		result = append(result, m)
	}

	return result
}

// TestStreamResolver_OracleParity verifies the resolver against an independent
// oracle on randomized single-section inputs. Uses deterministic iteration-based
// seeds for reproducibility.
func TestStreamResolver_OracleParity(t *testing.T) {
	ctx := context.Background()

	for seed := int64(0); seed < 300; seed++ {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			nStreams := 200 + rng.Intn(800)
			nMatchers := 1 + rng.Intn(4)

			labelPostings, groundTruth := generateSection(rng, nStreams)
			secs, closer := buildResolveTestSection(t, labelPostings, nil)
			defer closer()

			matchers := randomMatchers(rng, nMatchers)

			// Resolve using the real resolver.
			r := postings.NewStreamResolver(matchers, nil, time.Unix(0, 0), time.Unix(0, 2000000))
			got, err := r.Resolve(ctx, secs)
			require.NoError(t, err)

			// Collect stream IDs from the resolver result.
			gotIDs := make(map[int64]struct{})
			for _, sr := range got {
				for _, id := range streamIDs(sr) {
					gotIDs[id] = struct{}{}
				}
			}

			// Compute expected result via oracle.
			want := oracleMatch(groundTruth, matchers)

			require.Equal(t, want, gotIDs, "seed=%d matchers=%v", seed, matchers)
		})
	}
}

// TestStreamResolver_OracleParityMultiSection verifies the resolver against
// the oracle on multiple interleaved logical sections (same physical postings
// section holding rows for different (obj, section) pairs).
func TestStreamResolver_OracleParityMultiSection(t *testing.T) {
	ctx := context.Background()

	for seed := int64(0); seed < 200; seed++ {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			nSections := 2 + rng.Intn(4)
			nMatchers := 1 + rng.Intn(4)

			// Generate multiple logical sections.
			var allLabelPostings []labelPosting
			allGroundTruths := make(map[string]map[int64]map[string]string) // key -> streamID -> label map

			for s := int64(0); s < int64(nSections); s++ {
				nStreams := 100 + rng.Intn(400)
				labelPostings, groundTruth := generateSection(rng, nStreams)

				// Rewrite streamIDs and obj/section to make them distinct.
				for i := range labelPostings {
					labelPostings[i].obj = fmt.Sprintf("obj-%d", s)
					labelPostings[i].section = s
				}

				allLabelPostings = append(allLabelPostings, labelPostings...)

				// Map by "obj/section" key.
				key := fmt.Sprintf("obj-%d/%d", s, s)
				allGroundTruths[key] = groundTruth
			}

			secs, closer := buildResolveTestSection(t, allLabelPostings, nil)
			defer closer()

			matchers := randomMatchers(rng, nMatchers)

			// Resolve using the real resolver.
			r := postings.NewStreamResolver(matchers, nil, time.Unix(0, 0), time.Unix(0, 2000000))
			got, err := r.Resolve(ctx, secs)
			require.NoError(t, err)

			// Map results by "obj/section" key.
			gotByKey := make(map[string]map[int64]struct{})
			for _, sr := range got {
				key := fmt.Sprintf("%s/%d", sr.ObjectPath, sr.SectionIndex)
				if gotByKey[key] == nil {
					gotByKey[key] = make(map[int64]struct{})
				}
				for _, id := range streamIDs(sr) {
					gotByKey[key][id] = struct{}{}
				}
			}

			// Verify each logical section.
			for key, groundTruth := range allGroundTruths {
				want := oracleMatch(groundTruth, matchers)
				got := gotByKey[key]
				if got == nil {
					got = make(map[int64]struct{})
				}
				require.Equal(t, want, got, "seed=%d key=%s matchers=%v", seed, key, matchers)
			}
		})
	}
}

// TestStreamResolver_EmptyCapableNameAbsentFromSection guards that an
// empty-capable matcher on a label name present in no row of the section keeps
// every seeded stream: all of them lack the label. app=loki seeds {0,1}; "team"
// is absent from the section, so team!="bar" must leave {0,1} intact.
func TestStreamResolver_EmptyCapableNameAbsentFromSection(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildResolveTestSection(t, []labelPosting{
		{name: "app", value: "loki", streamID: 0, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "app", value: "loki", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	r := postings.NewStreamResolver([]*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "loki"),
		labels.MustNewMatcher(labels.MatchNotEqual, "team", "bar"),
	}, nil, time.Unix(0, 0), time.Unix(0, 1000))

	res, err := r.Resolve(ctx, secs)
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.ElementsMatch(t, []int64{0, 1}, streamIDs(res[0]))
}

// randomEqualPredicates generates 0..2 MatchEqual predicates whose names are
// drawn from schemaNames, so predicate names collide with label names and
// exercise the predicate-as-stream-label path.
func randomEqualPredicates(rng *rand.Rand) []*labels.Matcher {
	n := rng.Intn(3)
	out := make([]*labels.Matcher, 0, n)
	for i := 0; i < n; i++ {
		name := schemaNames[rng.Intn(len(schemaNames))]
		value := schemaValues[rng.Intn(len(schemaValues))]
		out = append(out, labels.MustNewMatcher(labels.MatchEqual, name, value))
	}
	return out
}

// bloomsForPredicates emits, per stream that carries a predicate's name as a
// label, a bloom posting on that column holding the stream's actual value. This
// mirrors how structured-metadata blooms are populated, so the bloom gate
// admits a section exactly when some stream's value tests positive.
func bloomsForPredicates(groundTruth map[int64]map[string]string, preds []*labels.Matcher) []bloomPosting {
	var out []bloomPosting
	for streamID, streamLabels := range groundTruth {
		for _, p := range preds {
			if v, ok := streamLabels[p.Name]; ok {
				out = append(out, bloomPosting{
					columnName: p.Name,
					values:     []string{v},
					streamID:   streamID,
					obj:        "obj-0",
					section:    0,
				})
			}
		}
	}
	return out
}

// oracleAmbiguousNames returns the predicate names that exist as a label on any
// stream in the section, when the section survives. The resolver records a name
// as ambiguous if it observed that name as a stream label anywhere in the
// section, independent of which streams ultimately survive, so the oracle scans
// the whole ground truth rather than only the survivors.
func oracleAmbiguousNames(groundTruth map[int64]map[string]string, survivors map[int64]struct{}, preds []*labels.Matcher) map[string]struct{} {
	out := make(map[string]struct{})
	if len(survivors) == 0 {
		return out
	}
	for _, p := range preds {
		for _, streamLabels := range groundTruth {
			if _, ok := streamLabels[p.Name]; ok {
				out[p.Name] = struct{}{}
				break
			}
		}
	}
	return out
}

// TestStreamResolver_OracleParityWithPredicates verifies the resolver against
// the oracle on randomized inputs that include equal-predicates colliding with
// label names, covering the predicate-as-stream-label path and AmbiguousNames.
func TestStreamResolver_OracleParityWithPredicates(t *testing.T) {
	ctx := context.Background()

	for seed := int64(0); seed < 300; seed++ {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			nStreams := 200 + rng.Intn(800)
			nMatchers := 1 + rng.Intn(4)

			labelPostings, groundTruth := generateSection(rng, nStreams)
			matchers := randomMatchers(rng, nMatchers)
			preds := randomEqualPredicates(rng)
			blooms := bloomsForPredicates(groundTruth, preds)

			secs, closer := buildResolveTestSection(t, labelPostings, blooms)
			defer closer()

			r := postings.NewStreamResolver(matchers, preds, time.Unix(0, 0), time.Unix(0, 2000000))
			got, err := r.Resolve(ctx, secs)
			require.NoError(t, err)

			gotIDs := make(map[int64]struct{})
			gotAmbiguous := make(map[string]struct{})
			for _, sr := range got {
				for _, id := range streamIDs(sr) {
					gotIDs[id] = struct{}{}
				}
				for _, name := range sr.AmbiguousNames {
					gotAmbiguous[name] = struct{}{}
				}
			}

			want := oracleMatch(groundTruth, matchers)
			require.Equal(t, want, gotIDs, "seed=%d matchers=%v preds=%v", seed, matchers, preds)

			wantAmbiguous := oracleAmbiguousNames(groundTruth, want, preds)
			require.Equal(t, wantAmbiguous, gotAmbiguous, "seed=%d preds=%v", seed, preds)
		})
	}
}
