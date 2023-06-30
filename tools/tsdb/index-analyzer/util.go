package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/config"
)

const (
	daySeconds = int64(24 * time.Hour / time.Second)
)

// regexp for finding the trailing index bucket number at the end of table name
var extractTableNumberRegex = regexp.MustCompile(`[0-9]+$`)

// copied from indexshipper/downloads
// extractTableNumberFromName extract the table number from a given tableName.
// if the tableName doesn't match the regex, it would return -1 as table number.
func extractTableNumberFromName(tableName string) (int64, error) {
	match := extractTableNumberRegex.Find([]byte(tableName))
	if match == nil {
		return -1, nil
	}

	tableNumber, err := strconv.ParseInt(string(match), 10, 64)
	if err != nil {
		return -1, err
	}

	return tableNumber, nil
}
func getActiveTableNumber() int64 {
	return getTableNumberForTime(model.Now())
}

func getTableNumberForTime(t model.Time) int64 {
	return t.Unix() / daySeconds
}

// copied from storage/store.go
func getIndexStoreTableRanges(indexType string, periodicConfigs []config.PeriodConfig) config.TableRanges {
	var ranges config.TableRanges
	for i := range periodicConfigs {
		if periodicConfigs[i].IndexType != indexType {
			continue
		}

		periodEndTime := config.DayTime{Time: math.MaxInt64}
		if i < len(periodicConfigs)-1 {
			periodEndTime = config.DayTime{Time: periodicConfigs[i+1].From.Time.Add(-time.Millisecond)}
		}

		ranges = append(ranges, periodicConfigs[i].GetIndexTableNumberRange(periodEndTime))
	}

	return ranges
}

func resolveTenants(objectClient client.ObjectClient, bucket string, tableRanges config.TableRanges) ([]string, string, error) {
	if bucket == "" {
		return nil, "", errors.New("empty bucket")
	}

	tableNo, err := extractTableNumberFromName(bucket)
	if err != nil {
		return nil, "", err
	}

	tableName, ok := tableRanges.TableNameFor(tableNo)
	if !ok {
		return nil, "", fmt.Errorf("no table name found for table number %d", tableNo)
	}

	prefix := filepath.Join("index", tableName)
	indices, _, err := objectClient.List(context.Background(), prefix, "")
	if err != nil {
		return nil, "", fmt.Errorf("error listing tenants: %w", err)
	}

	tenants := make(map[string]struct{})
	for _, index := range indices {
		s := strings.TrimPrefix(index.Key, prefix+"/")
		tenant := strings.Split(s, "/")[0]
		tenants[tenant] = struct{}{}
	}

	var result []string
	for tenant := range tenants {
		result = append(result, tenant)
	}

	return result, tableName, nil
}
