// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/confmap/internal"

import "go.opentelemetry.io/collector/featuregate"

var EnableMergeAppendOption = featuregate.GlobalRegistry().MustRegister(
	"confmap.enableMergeAppendOption",
	featuregate.StageAlpha,
	featuregate.WithRegisterFromVersion("v0.120.0"),
	featuregate.WithRegisterDescription("Combines lists when resolving configs from different sources. This feature gate will not be stabilized 'as is'; the current behavior will remain the default."),
	featuregate.WithRegisterReferenceURL("https://github.com/open-telemetry/opentelemetry-collector/issues/8754"),
)
