package sizing

type Pod struct {
	Replicas int `json:"replicas"`
	Rate     int `json:"rate"`
	CPU      struct {
		Request float64 `json:"request"`
		Limit   float64 `json:"limit"`
	} `json:"cpu"`
	Memory struct {
		Request int `json:"request"`
		Limit   int `json:"limit"`
	} `json:"memory"`
}

type Loki struct {
	AuthEnabled bool `json:"auth_enabled"`
}

type Read struct {
	Replicas  int       `json:"replicas"`
	Resources Resources `json:"resources"`
}

type Write struct {
	Replicas  int       `json:"replicas"`
	Resources Resources `json:"resources"`
}

type Resources struct {
	Requests struct {
		CPU    float64 `json:"cpu"`
		Memory int     `json:"memory"`
	} `json:"requests"`
	Limits struct {
		CPU    float64 `json:"cpu"`
		Memory int     `json:"memory"`
	} `json:"limits"`
}

type Values struct {
	Loki  Loki  `json:"loki"`
	Read  Read  `json:"read"`
	Write Write `json:"write"`
}

func constructHelmValues(cluster ClusterSize, nodeType NodeType) Values {
	return Values{
		Loki: Loki{
			AuthEnabled: false,
		},
		Read: Read{
			Replicas: cluster.TotalReadReplicas,
			Resources: Resources{
				Requests: struct {
					CPU    float64 `json:"cpu"`
					Memory int     `json:"memory"`
				}{
					CPU:    nodeType.readPod.cpuRequest,
					Memory: nodeType.readPod.memoryRequest,
				},
				Limits: struct {
					CPU    float64 `json:"cpu"`
					Memory int     `json:"memory"`
				}{
					CPU:    nodeType.readPod.cpuLimit,
					Memory: nodeType.readPod.memoryLimit,
				},
			},
		},
		Write: Write{
			Replicas: cluster.TotalWriteReplicas,
			Resources: Resources{
				Requests: struct {
					CPU    float64 `json:"cpu"`
					Memory int     `json:"memory"`
				}{
					CPU:    nodeType.writePod.cpuRequest,
					Memory: nodeType.writePod.memoryRequest,
				},
				Limits: struct {
					CPU    float64 `json:"cpu"`
					Memory int     `json:"memory"`
				}{
					CPU:    nodeType.writePod.cpuLimit,
					Memory: nodeType.writePod.memoryLimit,
				},
			},
		},
	}
}
