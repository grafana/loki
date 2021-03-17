---
title: geoip
---
# `geoip` stage

The `geoip` stage is a parsing stage that reads a ip address and 
populates the labelset with geoip fields. Maxmind's GeoIP2 databse is used for the lookup.

Populated fields for City db:

- geoip_city_name
- geoip_country_name
- geoip_continet_name
- geoip_continent_code
- geoip_location_latitude
- geoip_location_longitude
- geoip_postal_code
- geoip_timezone
- geoip_subdivision_name
- geoip_subdivision_code

Populated fields for ASN db:

- geoip_autonomous_system_number
- geoip_autonomous_system_organization

## Schema

```yaml
geoip:
  # Path to the Maxmind DB file
  [db: <string>]

  # IP from extracted data to parse.
  [source: <string>]
  
  # Maxmind DB type. Allowed values are "city", "asn"
  [db_type: <string>]
```

## GeoIP with City database

For the given pipeline

```yaml
- regex:
    expression: "^(?P<ip>\S+) .*"
- geoip:
    db: "/path/to/GeoIP2-City.mmdb"
    source: "ip"
    db_type: "city"
```

And the log line:

```
"81.2.69.142 - "POST /loki/api/push/ HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"
```

The `regex` stage parses the log line and `ip` is extracted. Then the extracted `ip` value is given as `source` to `geoip` stage. `geoip` stage performs a lookup on the `ip` and populates below labels

- `geoip_city_name`: `Bedmond`
- `geoip_country_name`: `United Kingdom`
- `geoip_continet_name`: `Europe`
- `geoip_continent_code`: `EU`
- `geoip_location_latitude`: `51.7196`
- `geoip_location_longitude`: `-0.4144`
- `geoip_postal_code`: `WD5`
- `geoip_timezone`: `Europe/London`
- `geoip_subdivision_name`: `Hertfordshire`
- `geoip_subdivision_code`: `HRT`

If only a subset of above labels are required. We can chain the above pipeline with `labeldrop` or `labelallow` stage

### labelallow
```yaml
- regex:
    expression: "^(?P<ip>\S+) .*"
- geoip:
    db: "/path/to/GeoCity.mmdb"
    source: "ip"
    db_type: "city"
- labelallow:
  - geoip_city_name
  - geoip_country_name
  - geoip_location_latitude
  - geoip_location_longitude
```

Only the labels listed under `labelallow` will be sent to Loki.

### labeldrop

```yaml
- regex:
    expression: "^(?P<ip>\S+) .*"
- geoip:
    db: "/path/to/GeoCity.mmdb"
    source: "ip"
    db_type: "city"
- labeldrop:
  - geoip_postal_code
  - geoip_subdivision_code
```

All the labels except the ones listed under `labeldrop` will be sent to Loki.

## GeoIP with ASN database

```yaml
- regex:
    expression: "^(?P<ip>\S+) .*"
- geoip:
    db: "/path/to/GeoIP2-ASN.mmdb"
    source: "ip"
    db_type: "asn"
```

And the log line:

```
"81.2.69.142 - "POST /loki/api/push/ HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"
```

The `regex` stage parses the log line and `ip` is extracted. Then the extracted `ip` value is given as `source` to `geoip` stage. `geoip` stage performs a lookup on the `ip` and populates below labels

- `geoip_autonomous_system_number`: `20712`
- `geoip_autonomous_system_organization`: `Andrews & Arnold Ltd`
