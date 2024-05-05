package stages

import (
	"fmt"
	"net"
	"reflect"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/mitchellh/mapstructure"
	"github.com/oschwald/geoip2-golang"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
)

const (
	ErrEmptyGeoIPStageConfig       = "geoip stage config cannot be empty"
	ErrEmptyDBPathGeoIPStageConfig = "db path cannot be empty"
	ErrEmptySourceGeoIPStageConfig = "source cannot be empty"
	ErrEmptyDBTypeGeoIPStageConfig = "db type should be either city or asn"
)

type GeoIPFields int

const (
	CITYNAME GeoIPFields = iota
	COUNTRYNAME
	CONTINENTNAME
	CONTINENTCODE
	LOCATION
	POSTALCODE
	TIMEZONE
	SUBDIVISIONNAME
	SUBDIVISIONCODE
)

var fields = map[GeoIPFields]string{
	CITYNAME:        "geoip_city_name",
	COUNTRYNAME:     "geoip_country_name",
	CONTINENTNAME:   "geoip_continent_name",
	CONTINENTCODE:   "geoip_continent_code",
	LOCATION:        "geoip_location",
	POSTALCODE:      "geoip_postal_code",
	TIMEZONE:        "geoip_timezone",
	SUBDIVISIONNAME: "geoip_subdivision_name",
	SUBDIVISIONCODE: "geoip_subdivision_code",
}

// GeoIPConfig represents GeoIP stage config
type GeoIPConfig struct {
	DB     string  `mapstructure:"db"`
	Source *string `mapstructure:"source"`
	DBType string  `mapstructure:"db_type"`
}

func validateGeoIPConfig(c *GeoIPConfig) error {
	if c == nil {
		return errors.New(ErrEmptyGeoIPStageConfig)
	}

	if c.DB == "" {
		return errors.New(ErrEmptyDBPathGeoIPStageConfig)
	}

	if c.Source != nil && *c.Source == "" {
		return errors.New(ErrEmptySourceGeoIPStageConfig)
	}

	if c.DBType == "" {
		return errors.New(ErrEmptyDBTypeGeoIPStageConfig)
	}

	return nil
}

func newGeoIPStage(logger log.Logger, configs interface{}) (Stage, error) {
	cfgs := &GeoIPConfig{}
	err := mapstructure.Decode(configs, cfgs)
	if err != nil {
		return nil, err
	}

	err = validateGeoIPConfig(cfgs)
	if err != nil {
		return nil, err
	}

	db, err := geoip2.Open(cfgs.DB)
	if err != nil {
		return nil, err
	}

	return &geoIPStage{
		db:     db,
		logger: logger,
		cfgs:   cfgs,
	}, nil
}

type geoIPStage struct {
	logger log.Logger
	db     *geoip2.Reader
	cfgs   *GeoIPConfig
}

// Run implements Stage
func (g *geoIPStage) Run(in chan Entry) chan Entry {
	out := make(chan Entry)
	go func() {
		defer close(out)
		defer g.close()
		for e := range in {
			g.process(e.Labels, e.Extracted, &e.Timestamp, &e.Entry.Line)
			out <- e
		}
	}()
	return out
}

// Name implements Stage
func (g *geoIPStage) Name() string {
	return StageTypeGeoIP
}

// Cleanup implements Stage.
func (*geoIPStage) Cleanup() {
	// no-op
}

func (g *geoIPStage) process(labels model.LabelSet, extracted map[string]interface{}, _ *time.Time, _ *string) {
	var ip net.IP
	if g.cfgs.Source != nil {
		if _, ok := extracted[*g.cfgs.Source]; !ok {
			if Debug {
				level.Debug(g.logger).Log("msg", "source does not exist in the set of extracted values", "source", *g.cfgs.Source)
			}
			return
		}

		value, err := getString(extracted[*g.cfgs.Source])
		if err != nil {
			if Debug {
				level.Debug(g.logger).Log("msg", "failed to convert source value to string", "source", *g.cfgs.Source, "err", err, "type", reflect.TypeOf(extracted[*g.cfgs.Source]))
			}
			return
		}
		ip = net.ParseIP(value)
	}
	switch g.cfgs.DBType {
	case "city":
		record, err := g.db.City(ip)
		if err != nil {
			level.Error(g.logger).Log("msg", "unable to get City record for the ip", "err", err, "ip", ip)
			return
		}
		g.populateLabelsWithCityData(labels, record)
	case "asn":
		record, err := g.db.ASN(ip)
		if err != nil {
			level.Error(g.logger).Log("msg", "unable to get ASN record for the ip", "err", err, "ip", ip)
			return
		}
		g.populateLabelsWithASNData(labels, record)
	default:
		level.Error(g.logger).Log("msg", "unknown database type")
	}
}

func (g *geoIPStage) close() {
	if err := g.db.Close(); err != nil {
		level.Error(g.logger).Log("msg", "error while closing geoip db", "err", err)
	}
}

func (g *geoIPStage) populateLabelsWithCityData(labels model.LabelSet, record *geoip2.City) {
	for field, label := range fields {
		switch field {
		case CITYNAME:
			cityName := record.City.Names["en"]
			if cityName != "" {
				labels[model.LabelName(label)] = model.LabelValue(cityName)
			}
		case COUNTRYNAME:
			contryName := record.Country.Names["en"]
			if contryName != "" {
				labels[model.LabelName(label)] = model.LabelValue(contryName)
			}
		case CONTINENTNAME:
			continentName := record.Continent.Names["en"]
			if continentName != "" {
				labels[model.LabelName(label)] = model.LabelValue(continentName)
			}
		case CONTINENTCODE:
			continentCode := record.Continent.Code
			if continentCode != "" {
				labels[model.LabelName(label)] = model.LabelValue(continentCode)
			}
		case POSTALCODE:
			postalCode := record.Postal.Code
			if postalCode != "" {
				labels[model.LabelName(label)] = model.LabelValue(postalCode)
			}
		case TIMEZONE:
			timezone := record.Location.TimeZone
			if timezone != "" {
				labels[model.LabelName(label)] = model.LabelValue(timezone)
			}
		case LOCATION:
			latitude := record.Location.Latitude
			longitude := record.Location.Longitude
			if latitude != 0 || longitude != 0 {
				labels[model.LabelName(fmt.Sprintf("%s_latitude", label))] = model.LabelValue(fmt.Sprint(latitude))
				labels[model.LabelName(fmt.Sprintf("%s_longitude", label))] = model.LabelValue(fmt.Sprint(longitude))
			}
		case SUBDIVISIONNAME:
			if len(record.Subdivisions) > 0 {
				// we get most specific subdivision https://dev.maxmind.com/release-note/most-specific-subdivision-attribute-added/
				subdivisionName := record.Subdivisions[len(record.Subdivisions)-1].Names["en"]
				if subdivisionName != "" {
					labels[model.LabelName(label)] = model.LabelValue(subdivisionName)
				}
			}
		case SUBDIVISIONCODE:
			if len(record.Subdivisions) > 0 {
				subdivisionCode := record.Subdivisions[len(record.Subdivisions)-1].IsoCode
				if subdivisionCode != "" {
					labels[model.LabelName(label)] = model.LabelValue(subdivisionCode)
				}
			}
		default:
			level.Error(g.logger).Log("msg", "unknown geoip field")
		}
	}
}

func (g *geoIPStage) populateLabelsWithASNData(labels model.LabelSet, record *geoip2.ASN) {
	autonomousSystemNumber := record.AutonomousSystemNumber
	autonomousSystemOrganization := record.AutonomousSystemOrganization
	if autonomousSystemNumber != 0 {
		labels[model.LabelName("geoip_autonomous_system_number")] = model.LabelValue(fmt.Sprint(autonomousSystemNumber))
	}
	if autonomousSystemOrganization != "" {
		labels[model.LabelName("geoip_autonomous_system_organization")] = model.LabelValue(autonomousSystemOrganization)
	}
}
