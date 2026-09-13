// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package extensions

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
)

var canonicalExtensionTypes = []arrow.ExtensionType{
	&JSONType{},
	&OpaqueType{},
	&TimestampWithOffsetType{},
	&VariantType{},
	NewBool8Type(),
	NewUUIDType(),
}

func init() {
	for _, extType := range canonicalExtensionTypes {
		if err := arrow.RegisterExtensionType(extType); err != nil {
			panic(err)
		}
	}

	// arrow-go originally registered Variant as parquet.variant. Keep that
	// name in the registry so older IPC still deserializes. Seed default
	// storage so GetExtensionType("parquet.variant") is a complete type.
	if err := arrow.RegisterExtensionType(&legacyVariantType{VariantType: *NewDefaultVariantType()}); err != nil {
		panic(err)
	}
}

// legacyVariantType is a compatibility adapter for the historical
// parquet.variant name. Deserialize always returns a canonical VariantType;
// newly written IPC uses VariantExtensionName.
type legacyVariantType struct {
	VariantType
}

func (*legacyVariantType) ExtensionName() string { return LegacyVariantExtensionName }

func (v *legacyVariantType) String() string {
	return fmt.Sprintf("extension<%s>", v.ExtensionName())
}

func (v *legacyVariantType) ExtensionEquals(other arrow.ExtensionType) bool {
	return variantExtensionEquals(v.StorageType(), other)
}

func (*legacyVariantType) Deserialize(storageType arrow.DataType, _ string) (arrow.ExtensionType, error) {
	return NewVariantType(storageType)
}
