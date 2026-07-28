// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package internal

import (
	"time"

	bigtablepb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	"google.golang.org/protobuf/types/known/durationpb"
)

// defaultClientConfig is the ClientConfiguration proto the
// ClientConfigurationManager uses before the first successful
// GetClientConfiguration poll and as the fallback when a poll fails after
// the previous server-supplied configuration's validity window has expired.
//
// Kept as a live *bigtablepb.ClientConfiguration (rather than a parallel
// Go-struct hierarchy) so the manager never has to translate between two
// representations of the same shape. Field-level fallbacks read from this
// proto via the accessors defined in client_configuration_manager.go.
//
// Values live in this file so the defaults read as a tunable table
// independent of the polling machinery that consumes them.
var defaultClientConfig = &bigtablepb.ClientConfiguration{
	Polling: &bigtablepb.ClientConfiguration_PollingConfiguration_{
		PollingConfiguration: &bigtablepb.ClientConfiguration_PollingConfiguration{
			PollingInterval:  durationpb.New(300 * time.Second),
			ValidityDuration: durationpb.New(100 * 365 * 24 * time.Hour), // safe representation of 10,000 years
			MaxRpcRetryCount: 5,
		},
	},
	SessionConfiguration: &bigtablepb.SessionClientConfiguration{
		SessionLoad: 0,
		ChannelConfiguration: &bigtablepb.SessionClientConfiguration_ChannelPoolConfiguration{
			MinServerCount:        2,
			MaxServerCount:        25,
			PerServerSessionCount: 10,
			Mode: &bigtablepb.SessionClientConfiguration_ChannelPoolConfiguration_DirectAccessWithFallback_{
				DirectAccessWithFallback: &bigtablepb.SessionClientConfiguration_ChannelPoolConfiguration_DirectAccessWithFallback{
					CheckInterval:      durationpb.New(60 * time.Second),
					ErrorRateThreshold: 0.8,
				},
			},
		},
		SessionPoolConfiguration: &bigtablepb.SessionClientConfiguration_SessionPoolConfiguration{
			Headroom:                           0.5,
			MinSessionCount:                    5,
			MaxSessionCount:                    400,
			NewSessionCreationBudget:           50,
			NewSessionCreationPenalty:          durationpb.New(60 * time.Second),
			ConsecutiveSessionFailureThreshold: 10,
			NewSessionQueueLength:              10,
			LoadBalancingOptions: &bigtablepb.LoadBalancingOptions{
				LoadBalancingStrategy: &bigtablepb.LoadBalancingOptions_LeastInFlight_{
					LeastInFlight: &bigtablepb.LoadBalancingOptions_LeastInFlight{},
				},
			},
		},
	},
}
