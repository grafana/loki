package sizing

type NodeType struct {
	name     string
	cores    int
	memoryGB int
	readPod  NodePod
	writePod NodePod
}

type NodePod struct {
	cpuRequest      float64
	cpuLimit        float64 // Or null
	memoryRequest   int
	memoryLimit     int
	rateBytesSecond float64
}

var StandardWrite = NodePod{
	cpuRequest:      1,
	cpuLimit:        2,
	memoryRequest:   6,
	memoryLimit:     12,
	rateBytesSecond: 3 * 1024 * 1024,
}

var StandardRead = NodePod{
	cpuRequest:      3,
	cpuLimit:        3, // Undefined TODO: Is this a bug
	memoryRequest:   6,
	memoryLimit:     8,
	rateBytesSecond: 768 * 1024 * 1024,
}

var NodeTypesByProvider = map[string]map[string]NodeType{
	"AWS": {
		"t2.xlarge": {
			name:     "t2.xlarge",
			cores:    4,
			memoryGB: 16,
			readPod:  StandardRead,
			writePod: StandardWrite,
		},
		"t2.2xlarge": {
			name:     "t2.2xlarge",
			cores:    8,
			memoryGB: 32,
			readPod:  StandardRead,
			writePod: StandardWrite,
		},
	},
	"GCP": {
		"e2-standard-4": {
			name:     "e2-standard-4",
			cores:    4,
			memoryGB: 16,
			readPod:  StandardRead,
			writePod: StandardWrite,
		},
		"e2-standard-8": {
			name:     "e2-standard-8",
			cores:    8,
			memoryGB: 32,
			readPod:  StandardRead,
			writePod: StandardWrite,
		},
		"e2-standard-16": {
			name:     "e2-standard-16",
			cores:    16,
			memoryGB: 64,
			readPod:  StandardRead,
			writePod: StandardWrite,
		},
	},
	"OVHcloud": {
		"b2-30": {
			name:     "b2-30",
			cores:    8,
			memoryGB: 30,
			readPod:  StandardRead,
			writePod: StandardWrite,
		},
		"b2-60": {
			name:     "b2-60",
			cores:    16,
			memoryGB: 60,
			readPod:  StandardRead,
			writePod: StandardWrite,
		},
		"b2-120": {
			name:     "b2-120",
			cores:    32,
			memoryGB: 120,
			readPod:  StandardRead,
			writePod: StandardWrite,
		},
	},
}
