package kuberesolver

type EventType string

const (
	Added    EventType = "ADDED"
	Modified EventType = "MODIFIED"
	Deleted  EventType = "DELETED"
	Error    EventType = "ERROR"
)

// Event represents a single event to a watched resource.
type Event struct {
	Type   EventType `json:"type"`
	Object Endpoints `json:"object"`
}

type Endpoints struct {
	Kind       string   `json:"kind"`
	ApiVersion string   `json:"apiVersion"`
	Metadata   Metadata `json:"metadata"`
	Subsets    []Subset `json:"subsets"`
}

type Metadata struct {
	Name            string `json:"name"`
	ResourceVersion string `json:"resourceVersion"`
}

type Subset struct {
	Addresses []Address `json:"addresses"`
	Ports     []Port    `json:"ports"`
}

type Address struct {
	IP string `json:"ip"`
}

type Port struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}
