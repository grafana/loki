package sarama

type IncrementalAlterConfigsOperation int8

const (
	IncrementalAlterConfigsOperationSet IncrementalAlterConfigsOperation = iota
	IncrementalAlterConfigsOperationDelete
	IncrementalAlterConfigsOperationAppend
	IncrementalAlterConfigsOperationSubtract
)

// IncrementalAlterConfigsRequest is an incremental alter config request type
type IncrementalAlterConfigsRequest struct {
	Version      int16
	Resources    []*IncrementalAlterConfigsResource
	ValidateOnly bool
}

type IncrementalAlterConfigsResource struct {
	Type          ConfigResourceType
	Name          string
	ConfigEntries map[string]IncrementalAlterConfigsEntry
}

type IncrementalAlterConfigsEntry struct {
	Operation IncrementalAlterConfigsOperation
	Value     *string
}

func (a *IncrementalAlterConfigsRequest) encode(pe packetEncoder) error {
	if err := pe.putArrayLength(len(a.Resources)); err != nil {
		return err
	}

	for _, r := range a.Resources {
		if err := r.encode(pe); err != nil {
			return err
		}
	}

	pe.putBool(a.ValidateOnly)
	return nil
}

func (a *IncrementalAlterConfigsRequest) decode(pd packetDecoder, version int16) error {
	resourceCount, err := pd.getArrayLength()
	if err != nil {
		return err
	}

	a.Resources = make([]*IncrementalAlterConfigsResource, resourceCount)
	for i := range a.Resources {
		r := &IncrementalAlterConfigsResource{}
		err = r.decode(pd, version)
		if err != nil {
			return err
		}
		a.Resources[i] = r
	}

	validateOnly, err := pd.getBool()
	if err != nil {
		return err
	}

	a.ValidateOnly = validateOnly

	return nil
}

func (a *IncrementalAlterConfigsResource) encode(pe packetEncoder) error {
	pe.putInt8(int8(a.Type))

	if err := pe.putString(a.Name); err != nil {
		return err
	}

	if err := pe.putArrayLength(len(a.ConfigEntries)); err != nil {
		return err
	}

	for name, e := range a.ConfigEntries {
		if err := pe.putString(name); err != nil {
			return err
		}

		if err := e.encode(pe); err != nil {
			return err
		}
	}

	return nil
}

func (a *IncrementalAlterConfigsResource) decode(pd packetDecoder, version int16) error {
	t, err := pd.getInt8()
	if err != nil {
		return err
	}
	a.Type = ConfigResourceType(t)

	name, err := pd.getString()
	if err != nil {
		return err
	}
	a.Name = name

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}

	if n > 0 {
		a.ConfigEntries = make(map[string]IncrementalAlterConfigsEntry, n)
		for i := 0; i < n; i++ {
			name, err := pd.getString()
			if err != nil {
				return err
			}

			var v IncrementalAlterConfigsEntry

			if err := v.decode(pd, version); err != nil {
				return err
			}

			a.ConfigEntries[name] = v
		}
	}
	return err
}

func (a *IncrementalAlterConfigsEntry) encode(pe packetEncoder) error {
	pe.putInt8(int8(a.Operation))

	if err := pe.putNullableString(a.Value); err != nil {
		return err
	}

	return nil
}

func (a *IncrementalAlterConfigsEntry) decode(pd packetDecoder, version int16) error {
	t, err := pd.getInt8()
	if err != nil {
		return err
	}
	a.Operation = IncrementalAlterConfigsOperation(t)

	s, err := pd.getNullableString()
	if err != nil {
		return err
	}

	a.Value = s

	return nil
}

func (a *IncrementalAlterConfigsRequest) key() int16 {
	return 44
}

func (a *IncrementalAlterConfigsRequest) version() int16 {
	return a.Version
}

func (a *IncrementalAlterConfigsRequest) headerVersion() int16 {
	return 1
}

func (a *IncrementalAlterConfigsRequest) isValidVersion() bool {
	return a.Version == 0
}

func (a *IncrementalAlterConfigsRequest) requiredVersion() KafkaVersion {
	return V2_3_0_0
}
