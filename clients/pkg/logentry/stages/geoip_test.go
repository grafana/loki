package stages

import (
	"fmt"
	"net"
	"testing"

	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/oschwald/geoip2-golang"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func Test_ValidateConfigs(t *testing.T) {
	source := "ip"
	tests := []struct {
		config    GeoIPConfig
		wantError error
	}{
		{
			GeoIPConfig{
				DB:     "test",
				Source: &source,
				DBType: "city",
			},
			nil,
		},
		{
			GeoIPConfig{
				Source: &source,
				DBType: "city",
			},
			errors.New(ErrEmptyDBPathGeoIPStageConfig),
		},
		{
			GeoIPConfig{
				DB:     "test",
				DBType: "city",
			},
			errors.New(ErrEmptySourceGeoIPStageConfig),
		},
		{
			GeoIPConfig{
				DB:     "test",
				Source: &source,
			},
			errors.New(ErrEmptyDBTypeGeoIPStageConfig),
		},
	}
	for _, tt := range tests {
		err := validateGeoIPConfig(&tt.config)
		if err != nil {
			require.Equal(t, tt.wantError.Error(), err.Error())
		}
		if tt.wantError == nil {
			require.Nil(t, err)
		}
	}
}

/*
	NOTE:
	database schema: https://github.com/maxmind/MaxMind-DB/tree/main/source-data
	Script used to build the minimal binaries: https://github.com/vimt/MaxMind-DB-Writer-python
*/

func Test_MaxmindAsn(t *testing.T) {
	db, err := geoip2.Open("testdata/geoip_maxmind_asn.mmdb")
	if err != nil {
		t.Error(err)
		return
	}
	defer db.Close()
	ip := "192.0.2.1"
	record, err := db.ASN(net.ParseIP(ip))
	if err != nil {
		t.Error(err)
	}
	source := "ip"
	testStage := &geoIPStage{
		db:     db,
		logger: util_log.Logger,
		cfgs: &GeoIPConfig{
			DB:     "test",
			Source: &source,
			DBType: "asn",
		},
	}
	labels := model.LabelSet{}
	testStage.populateLabelsWithASNData(labels, record)

	for _, label := range []string{
		fields[ASN],
		fields[ASNORG],
	} {
		_, present := labels[model.LabelName(label)]
		if !present {
			t.Errorf("GeoIP label %v not present", label)
		}
	}
}

func Test_MaxmindCity(t *testing.T) {
	db, err := geoip2.Open("testdata/geoip_maxmind_city.mmdb")
	if err != nil {
		t.Error(err)
		return
	}
	defer db.Close()
	ip := "192.0.2.1"
	record, err := db.City(net.ParseIP(ip))
	if err != nil {
		t.Error(err)
	}
	source := "ip"
	testStage := &geoIPStage{
		db:     db,
		logger: util_log.Logger,
		cfgs: &GeoIPConfig{
			DB:     "test",
			Source: &source,
			DBType: "city",
		},
	}
	labels := model.LabelSet{}
	testStage.populateLabelsWithCityData(labels, record)

	for _, label := range []string{
		fields[COUNTRYNAME],
		fields[COUNTRYCODE],
		fields[CONTINENTNAME],
		fields[CONTINENTCODE],
		fields[CITYNAME],
		fmt.Sprintf("%s_latitude", fields[LOCATION]),
		fmt.Sprintf("%s_longitude", fields[LOCATION]),
		fields[POSTALCODE],
		fields[TIMEZONE],
		fields[SUBDIVISIONNAME],
		fields[SUBDIVISIONCODE],
		fields[COUNTRYNAME],
	} {
		_, present := labels[model.LabelName(label)]
		if !present {
			t.Errorf("GeoIP label %v not present", label)
		}
	}
}

func Test_MaxmindCountry(t *testing.T) {
	db, err := geoip2.Open("testdata/geoip_maxmind_country.mmdb")
	if err != nil {
		t.Error(err)
		return
	}
	defer db.Close()
	ip := "192.0.2.1"
	record, err := db.Country(net.ParseIP(ip))
	if err != nil {
		t.Error(err)
	}
	source := "ip"
	testStage := &geoIPStage{
		db:     db,
		logger: util_log.Logger,
		cfgs: &GeoIPConfig{
			DB:     "test",
			Source: &source,
			DBType: "country",
		},
	}
	labels := model.LabelSet{}
	testStage.populateLabelsWithCountryData(labels, record)

	for _, label := range []string{
		fields[COUNTRYNAME],
		fields[COUNTRYCODE],
		fields[CONTINENTNAME],
		fields[CONTINENTCODE],
	} {
		_, present := labels[model.LabelName(label)]
		if !present {
			t.Errorf("GeoIP label %v not present", label)
		}
	}
}
