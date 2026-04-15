// Copyright 2023-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// https://pb33f.io

package what_changed

import "github.com/pb33f/libopenapi/what-changed/model"

// Changed represents an object that was changed
type Changed interface {
	// GetAllChanges returns all top level changes made to properties in this object
	GetAllChanges() []*model.Change

	// TotalChanges returns a count of all changes made on the object, including all children
	TotalChanges() int

	// TotalBreakingChanges returns a count of all breaking changes on this object
	TotalBreakingChanges() int

	// GetPropertyChanges
	GetPropertyChanges() []*model.Change

	// PropertiesOnly will set a change object to only render properties and not the whole timeline.
	PropertiesOnly()
}
