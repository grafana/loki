package main

import (
	"fmt"
	"math"
	"os"
	"time"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/operator/internal/sizes"

	"github.com/ViaQ/logerr/v2/log"
)

const (
	// defaultDuration is the time for which the metric needs to be predicted for.
	// It is passed as second parameter to predict_linear.
	defaultDuration string = "24h"
	// range1xSmall defines the range (in GB)
	// of t-shirt size 1x.small i.e., 0 <= 1x.small <= 500
	range1xSmall int = 500
	// sizeOneXSmall defines the size of a single Loki deployment
	// with small resources/limits requirements. This size is dedicated for setup **without** the
	// requirement for single replication factor and auto-compaction.
	sizeOneXSmall string = "1x.small"
	// sizeOneXMedium defines the size of a single Loki deployment
	// with small resources/limits requirements. This size is dedicated for setup **with** the
	// requirement for single replication factor and auto-compaction.
	sizeOneXMedium string = "1x.medium"
)

func main() {
	logger := log.NewLogger("size-calculator")

	logger.Info("starting storage size calculator...")

	for {
		duration, parseErr := model.ParseDuration(defaultDuration)
		if parseErr != nil {
			logger.Error(parseErr, "failed to parse duration")
			os.Exit(1)
		}

		logsCollected, err := sizes.PredictFor(duration)
		if err != nil {
			logger.Error(err, "Failed to collect metrics data")
			os.Exit(1)
		}

		logsCollectedInGB := int(math.Ceil(logsCollected / math.Pow(1024, 3)))
		logger.Info(fmt.Sprintf("Amount of logs expected in 24 hours is %f Bytes or %dGB", logsCollected, logsCollectedInGB))

		if logsCollectedInGB <= range1xSmall {
			logger.Info(fmt.Sprintf("Recommended t-shirt size for %dGB is %s", logsCollectedInGB, sizeOneXSmall))
		} else {
			logger.Info(fmt.Sprintf("Recommended t-shirt size for %dGB is %s", logsCollectedInGB, sizeOneXMedium))
		}

		time.Sleep(1 * time.Minute)
	}
}
