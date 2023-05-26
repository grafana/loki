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
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	ResourceVersion string            `json:"resourceVersion"`
	Labels          map[string]string `json:"labels"`
}

type Subset struct {
	Addresses []Address `json:"addresses"`
	Ports     []Port    `json:"ports"`
}

type Address struct {
	IP        string           `json:"ip"`
	TargetRef *ObjectReference `json:"targetRef,omitempty"`
}

type ObjectReference struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}
type Port struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}
