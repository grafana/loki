package metrics

import (
	"errors"
	"testing"
	"time"
)

func TestParseScalar(t *testing.T) {
	vector := `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1787287976.7,"648332.74"]}]}}`
	v, err := parseScalar([]byte(vector), "expr")
	if err != nil {
		t.Fatalf("parseScalar: %v", err)
	}
	if v != 648332.74 {
		t.Fatalf("value = %v, want 648332.74", v)
	}
}

func TestParseScalar_EmptyIsNoData(t *testing.T) {
	empty := `{"status":"success","data":{"resultType":"vector","result":[]}}`
	_, err := parseScalar([]byte(empty), "expr")
	if !errors.As(err, &errNoData{}) {
		t.Fatalf("empty result: got %v, want errNoData", err)
	}
}

func TestParseScalar_NaNIsNoData(t *testing.T) {
	nan := `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,"NaN"]}]}}`
	_, err := parseScalar([]byte(nan), "expr")
	if !errors.As(err, &errNoData{}) {
		t.Fatalf("NaN sample: got %v, want errNoData", err)
	}
}

func TestParseScalar_BadStatus(t *testing.T) {
	bad := `{"status":"error","data":{"result":[]}}`
	if _, err := parseScalar([]byte(bad), "expr"); err == nil {
		t.Fatal("expected error on non-success status")
	}
}

func TestParseScalar_Malformed(t *testing.T) {
	if _, err := parseScalar([]byte("not json"), "expr"); err == nil {
		t.Fatal("expected parse error on malformed output")
	}
}

func TestPromDuration(t *testing.T) {
	cases := map[time.Duration]string{
		14 * time.Minute: "840s",
		30 * time.Second: "30s",
		time.Hour:        "3600s",
		0:                "1s", // never emit an empty or zero window
	}
	for d, want := range cases {
		if got := promDuration(d); got != want {
			t.Errorf("promDuration(%s) = %q, want %q", d, got, want)
		}
	}
}

func TestParseScalar_InfIsNoData(t *testing.T) {
	inf := `{"status":"success","data":{"result":[{"value":[1,"+Inf"]}]}}`
	if _, err := parseScalar([]byte(inf), "expr"); !errors.As(err, &errNoData{}) {
		t.Fatalf("+Inf sample: got %v, want errNoData", err)
	}
}

func TestParseScalar_BadFloatIsError(t *testing.T) {
	bad := `{"status":"success","data":{"result":[{"value":[1,"abc"]}]}}`
	_, err := parseScalar([]byte(bad), "expr")
	if err == nil {
		t.Fatal("expected an error on a non-numeric sample")
	}
	if errors.As(err, &errNoData{}) {
		t.Fatal("a non-numeric sample is a parse error, not no-data")
	}
}
