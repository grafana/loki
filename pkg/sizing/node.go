package sizing

type NodeType struct {
	name string
	//cloudType: CloudType;
	cores    int
	memoryGB int
	readPod  NodePod
	writePod NodePod
}

type NodePod struct {
	cpuRequest    float64
	cpuLimit      float64 // Or null
	memoryRequest int
	memoryLimit   int
	rateMbSecond  float64
}

var StandardWrite = NodePod{
	cpuRequest:    1,
	cpuLimit:      2,
	memoryRequest: 6,
	memoryLimit:   12,
	rateMbSecond:  3,
}

var StandardRead = NodePod{
	cpuRequest:    3,
	cpuLimit:      0, // Undefined
	memoryRequest: 6,
	memoryLimit:   8,
	rateMbSecond:  768,
}

var NodeTypesByProvider = map[string]map[string]NodeType{
	"AWS": {
		"t2.xlarge": {
			name: "t2.xlarge",
			//cloudType: "AWS",
			cores:    4,
			memoryGB: 16,
			readPod:  StandardRead,
			writePod: StandardWrite,
		},
		"t2.2xlarge": {
			name: "t2.2xlarge",
			//cloudType: "AWS",
			cores:    8,
			memoryGB: 32,
			readPod:  StandardRead,
			writePod: StandardWrite,
		},
		"t2.4xlarge": {
			name: "t2.4xlarge",
			//cloudType: "AWS",
			cores:    16,
			memoryGB: 64,
			readPod:  StandardRead,
			writePod: StandardWrite,
		},
	},
	"GCP": {
		"e2-standard-4": {
			name: "e2-standard-4",
			//cloudType: "GCP",
			cores:    4,
			memoryGB: 16,
			readPod:  StandardRead,
			writePod: StandardWrite,
		},
		"e2-standard-8": {
			name: "e2-standard-8",
			//cloudType: "GCP",
			cores:    8,
			memoryGB: 32,
			readPod:  StandardRead,
			writePod: StandardWrite,
		},
		"e2-standard-16": {
			name: "e2-standard-16",
			//cloudType: "GCP",
			cores:    16,
			memoryGB: 64,
			readPod:  StandardRead,
			writePod: StandardWrite,
		},
	},
}
