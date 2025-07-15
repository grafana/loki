package godo

// Meta describes generic information about a response.
type Meta struct {
	Page  int `json:"page,,omitempty"`
	Pages int `json:"pages,,omitempty"`
	Total int `json:"total"`
}
