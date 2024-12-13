package ingester

import (
	"sort"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

var (
	// cm11, cm12, cm13 are colliding with fp1.
	// cm21, cm22 are colliding with fp2.
	// cm31, cm32 are colliding with fp3, which is below maxMappedFP.
	// Note that fingerprints are set and not actually calculated.
	// The collision detection is independent from the actually used
	// fingerprinting algorithm.
	fp1  = model.Fingerprint(maxMappedFP + 1)
	fp2  = model.Fingerprint(maxMappedFP + 2)
	fp3  = model.Fingerprint(1)
	cm11 = []labels.Label{
		{Name: "dings", Value: "bumms"},
		{Name: "foo", Value: "bar"},
	}
	cm12 = []labels.Label{
		{Name: "bar", Value: "foo"},
	}
	cm13 = []labels.Label{
		{Name: "foo", Value: "bar"},
	}
	cm21 = []labels.Label{
		{Name: "dings", Value: "bar"},
		{Name: "foo", Value: "bumms"},
	}
	cm22 = []labels.Label{
		{Name: "bar", Value: "bumms"},
		{Name: "dings", Value: "foo"},
	}
	cm31 = []labels.Label{
		{Name: "bumms", Value: "dings"},
	}
	cm32 = []labels.Label{
		{Name: "bar", Value: "foo"},
		{Name: "bumms", Value: "dings"},
	}
)

func copyValuesAndSort(a []labels.Label) labels.Labels {
	c := make(labels.Labels, len(a))
	for i, pair := range a {
		c[i].Name = pair.Name
		c[i].Value = pair.Value
	}
	sort.Sort(c)
	return c
}

func TestFPMapper(t *testing.T) {
	sm := map[model.Fingerprint]labels.Labels{}

	mapper := NewFPMapper(func(fp model.Fingerprint) labels.Labels {
		return sm[fp]
	})

	// Everything is empty, resolving a FP should do nothing.
	assertFingerprintEqual(t, mapper.MapFP(fp1, cm11), fp1)
	assertFingerprintEqual(t, mapper.MapFP(fp1, cm12), fp1)

	// cm11 is in sm. Adding cm11 should do nothing. Mapping cm12 should resolve
	// the collision.
	sm[fp1] = copyValuesAndSort(cm11)
	assertFingerprintEqual(t, mapper.MapFP(fp1, cm11), fp1)
	assertFingerprintEqual(t, mapper.MapFP(fp1, cm12), model.Fingerprint(1))

	// The mapped cm12 is added to sm, too. That should not change the outcome.
	sm[model.Fingerprint(1)] = copyValuesAndSort(cm12)
	assertFingerprintEqual(t, mapper.MapFP(fp1, cm11), fp1)
	assertFingerprintEqual(t, mapper.MapFP(fp1, cm12), model.Fingerprint(1))

	// Now map cm13, should reproducibly result in the next mapped FP.
	assertFingerprintEqual(t, mapper.MapFP(fp1, cm13), model.Fingerprint(2))
	assertFingerprintEqual(t, mapper.MapFP(fp1, cm13), model.Fingerprint(2))

	// Add cm13 to sm. Should not change anything.
	sm[model.Fingerprint(2)] = copyValuesAndSort(cm13)
	assertFingerprintEqual(t, mapper.MapFP(fp1, cm11), fp1)
	assertFingerprintEqual(t, mapper.MapFP(fp1, cm12), model.Fingerprint(1))
	assertFingerprintEqual(t, mapper.MapFP(fp1, cm13), model.Fingerprint(2))

	// Now add cm21 and cm22 in the same way, checking the mapped FPs.
	assertFingerprintEqual(t, mapper.MapFP(fp2, cm21), fp2)
	sm[fp2] = copyValuesAndSort(cm21)
	assertFingerprintEqual(t, mapper.MapFP(fp2, cm21), fp2)
	assertFingerprintEqual(t, mapper.MapFP(fp2, cm22), model.Fingerprint(3))
	sm[model.Fingerprint(3)] = copyValuesAndSort(cm22)
	assertFingerprintEqual(t, mapper.MapFP(fp2, cm21), fp2)
	assertFingerprintEqual(t, mapper.MapFP(fp2, cm22), model.Fingerprint(3))

	// Map cm31, resulting in a mapping straight away.
	assertFingerprintEqual(t, mapper.MapFP(fp3, cm31), model.Fingerprint(4))
	sm[model.Fingerprint(4)] = copyValuesAndSort(cm31)

	// Map cm32, which is now mapped for two reasons...
	assertFingerprintEqual(t, mapper.MapFP(fp3, cm32), model.Fingerprint(5))
	sm[model.Fingerprint(5)] = copyValuesAndSort(cm32)

	// Now check ALL the mappings, just to be sure.
	assertFingerprintEqual(t, mapper.MapFP(fp1, cm11), fp1)
	assertFingerprintEqual(t, mapper.MapFP(fp1, cm12), model.Fingerprint(1))
	assertFingerprintEqual(t, mapper.MapFP(fp1, cm13), model.Fingerprint(2))
	assertFingerprintEqual(t, mapper.MapFP(fp2, cm21), fp2)
	assertFingerprintEqual(t, mapper.MapFP(fp2, cm22), model.Fingerprint(3))
	assertFingerprintEqual(t, mapper.MapFP(fp3, cm31), model.Fingerprint(4))
	assertFingerprintEqual(t, mapper.MapFP(fp3, cm32), model.Fingerprint(5))

	// Remove all the fingerprints from sm, which should change nothing, as
	// the existing mappings stay and should be detected.
	delete(sm, fp1)
	delete(sm, fp2)
	delete(sm, fp3)
	assertFingerprintEqual(t, mapper.MapFP(fp1, cm11), fp1)
	assertFingerprintEqual(t, mapper.MapFP(fp1, cm12), model.Fingerprint(1))
	assertFingerprintEqual(t, mapper.MapFP(fp1, cm13), model.Fingerprint(2))
	assertFingerprintEqual(t, mapper.MapFP(fp2, cm21), fp2)
	assertFingerprintEqual(t, mapper.MapFP(fp2, cm22), model.Fingerprint(3))
	assertFingerprintEqual(t, mapper.MapFP(fp3, cm31), model.Fingerprint(4))
	assertFingerprintEqual(t, mapper.MapFP(fp3, cm32), model.Fingerprint(5))
}

// assertFingerprintEqual asserts that two fingerprints are equal.
func assertFingerprintEqual(t *testing.T, gotFP, wantFP model.Fingerprint) {
	t.Helper()
	if gotFP != wantFP {
		t.Errorf("got fingerprint %v, want fingerprint %v", gotFP, wantFP)
	}
}
