// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pubsub

import (
	"context"
	"os"

	"google.golang.org/api/option"
	"google.golang.org/api/option/internaloption"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func init() {
	newTopicAdminClientHook = emulatorHook
	newSubscriptionAdminClientHook = emulatorHook
	newSchemaClientHook = emulatorHook
}

func emulatorHook(ctx context.Context, params clientHookParams) ([]option.ClientOption, error) {
	if addr := os.Getenv("PUBSUB_EMULATOR_HOST"); addr != "" {
		return []option.ClientOption{
			option.WithEndpoint(addr),
			option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
			option.WithoutAuthentication(),
			option.WithTelemetryDisabled(),
			internaloption.SkipDialSettingsValidation(),
		}, nil
	}
	return nil, nil
}
