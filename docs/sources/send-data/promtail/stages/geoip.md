---
title: geoip
menuTitle:  
description: The 'geoip' Promtail pipeline stage. 
aliases: 
- ../../../clients/promtail/stages/geoip/
weight:  
---

# geoip

{{< docs/shared source="loki" lookup="promtail-deprecation.md" version="<LOKI_VERSION>" >}}

The `geoip` stage is a parsing stage that reads an ip address and populates the labelset with geoip fields. [Maxmind's GeoIP2 database](https://www.maxmind.com/en/home) is used for the lookup.

Populated fields for City db:

- geoip_city_name
- geoip_country_name
- geoip_continent_name
- geoip_continent_code
- geoip_location_latitude
- geoip_location_longitude
- geoip_postal_code
- geoip_timezone
- geoip_subdivision_name
- geoip_subdivision_code

Populated fields for ASN (Autonomous System Number) db:

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

## GeoIP with City database example

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
"34.120.177.193 - "POST /loki/api/push/ HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"
```

The `regex` stage parses the log line and `ip` is extracted. Then the extracted `ip` value is given as `source` to `geoip` stage. The `geoip` stage performs a lookup on the `ip` and populates the following labels:

- `geoip_city_name`: `Kansas City`
- `geoip_country_name`: `United States`
- `geoip_continent_name`: `North America`
- `geoip_continent_code`: `NA`
- `geoip_location_latitude`: `"39.1027`
- `geoip_location_longitude`: `-94.5778`
- `geoip_postal_code`: `64184`
- `geoip_timezone`: `America/Chicago`
- `geoip_subdivision_name`: `Missouri`
- `geoip_subdivision_code`: `MO`

If only a subset of these labels are required, you can chain the above pipeline with the `labeldrop` or `labelallow` stage.

### labelallow example

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

### labeldrop example

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

## GeoIP with ASN (Autonomous System Number) database example

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
"34.120.177.193 - "POST /loki/api/push/ HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"
```

The `regex` stage parses the log line and `ip` is extracted. Then the extracted `ip` value is given as `source` to `geoip` stage. The `geoip` stage performs a lookup on the `ip` and populates the following labels:

- `geoip_autonomous_system_number`: `396982`
- `geoip_autonomous_system_organization`: `GOOGLE-CLOUD-PLATFORM`

For more information and real life example, see [Protect PII and add geolocation data: Monitoring legacy systems with Grafana
](/blog/2023/03/14/protect-pii-and-add-geolocation-data-monitoring-legacy-systems-with-grafana/) which has real-life examples on how to infuse dashboards with geo-location data.
