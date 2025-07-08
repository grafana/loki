// Copyright 2025 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build !localvalidationscheme

package expfmt

import (
	"bufio"
	"io"

	"google.golang.org/protobuf/encoding/protodelim"

	"github.com/prometheus/common/model"
)

// protoDecoder implements the Decoder interface for protocol buffers.
type protoDecoder struct {
	r protodelim.Reader
}

// NewDecoder returns a new decoder based on the given input format.
// If the input format does not imply otherwise, a text format decoder is returned.
func NewDecoder(r io.Reader, format Format) Decoder {
	switch format.FormatType() {
	case TypeProtoDelim:
		return &protoDecoder{r: bufio.NewReader(r)}
	}
	return &textDecoder{r: r}
}

func (d *protoDecoder) isValidMetricName(name string) bool {
	return model.IsValidMetricName(model.LabelValue(name))
}

func (d *protoDecoder) isValidLabelName(name string) bool {
	return model.LabelName(name).IsValid()
}
