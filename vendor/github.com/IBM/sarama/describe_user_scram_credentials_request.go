package sarama

// DescribeUserScramCredentialsRequest is a request to get list of SCRAM user names
type DescribeUserScramCredentialsRequest struct {
	// Version 0 is currently only supported
	Version int16

	// If this is an empty array, all users will be queried
	DescribeUsers []DescribeUserScramCredentialsRequestUser
}

func (r *DescribeUserScramCredentialsRequest) setVersion(v int16) {
	r.Version = v
}

// DescribeUserScramCredentialsRequestUser is a describe request about specific user name
type DescribeUserScramCredentialsRequestUser struct {
	Name string
}

func (r *DescribeUserScramCredentialsRequest) encode(pe packetEncoder) error {
	if err := pe.putArrayLength(len(r.DescribeUsers)); err != nil {
		return err
	}
	for _, d := range r.DescribeUsers {
		if err := pe.putString(d.Name); err != nil {
			return err
		}
		pe.putEmptyTaggedFieldArray()
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (r *DescribeUserScramCredentialsRequest) decode(pd packetDecoder, version int16) error {
	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n == -1 {
		n = 0
	}

	r.DescribeUsers = make([]DescribeUserScramCredentialsRequestUser, n)
	for i := 0; i < n; i++ {
		r.DescribeUsers[i] = DescribeUserScramCredentialsRequestUser{}
		if r.DescribeUsers[i].Name, err = pd.getString(); err != nil {
			return err
		}
		if _, err = pd.getEmptyTaggedFieldArray(); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *DescribeUserScramCredentialsRequest) key() int16 {
	return apiKeyDescribeUserScramCredentials
}

func (r *DescribeUserScramCredentialsRequest) version() int16 {
	return r.Version
}

func (r *DescribeUserScramCredentialsRequest) headerVersion() int16 {
	return 2
}

func (r *DescribeUserScramCredentialsRequest) isValidVersion() bool {
	return r.Version == 0
}

func (r *DescribeUserScramCredentialsRequest) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *DescribeUserScramCredentialsRequest) isFlexibleVersion(version int16) bool {
	return version >= 0
}

func (r *DescribeUserScramCredentialsRequest) requiredVersion() KafkaVersion {
	return V2_7_0_0
}
