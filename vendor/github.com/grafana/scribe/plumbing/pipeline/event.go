package pipeline

import (
	"fmt"
	"regexp"
)

type Stringer string

func (s Stringer) String() string {
	return string(s)
}

// FilterValueType is the metadata type that identifies the type present event filter.
type FilterValueType int

const (
	FilterValueString FilterValueType = iota
	FilterValueRegex
	FilterValueGlob
)

type FilterValue struct {
	Type  FilterValueType
	Value fmt.Stringer
}

func (f *FilterValue) String() string {
	return f.Value.String()
}

func StringFilter(v string) *FilterValue {
	return &FilterValue{
		Type:  FilterValueString,
		Value: Stringer(v),
	}
}

func RegexpFilter(v *regexp.Regexp) *FilterValue {
	return &FilterValue{
		Type:  FilterValueRegex,
		Value: v,
	}
}

func GlobFilter(v string) *FilterValue {
	return &FilterValue{
		Type:  FilterValueGlob,
		Value: Stringer(v),
	}
}

// Event is provided when defining a Scribe pipeline to define the events that cause the pipeline to be ran.
// Some example events that might cause pipelines to be created:
// * Manual events with user input, like 'Promotions' in Drone. In this scenario, the user may have the ability to supply any keys/values as arguments, however, pipeline developers in Scribe should be able to specifically define what fields are accepted. See https://docs.drone.io/promote/.
// * git and SCM-related events like 'Pull Reuqest', 'Commit', 'Tag'. Each one of these events has a unique set of arguments / filters. `Commit` may allow pipeline developers to filter by branch or message. Tags may allow developers to filter by name.
// * cron events, which may allow the pipeline in the CI service to be ran on a schedule.
// The Event type stores both the filters and a list of values that it provides to the pipeline.
// Client implementations of the pipeline (type Client) are expected to handle events that they are capable of handling.
// 'Handling' events means that the the arguments in the `Provides` key should be available before any first steps are ran. It will not typically be up to pipeline developers to decide what arguments an event provides.
// The only case where this may happen is if the event is a manual one, where users are able to submit the event with any arbitrary set of keys/values.
// The 'Filters' key is provided in the pipeline code and should not be populated when pre-defined in the Scribe package.
type Event struct {
	Filters  map[string]*FilterValue
	Provides []Argument
}

type GitCommitFilters struct {
	Branch *FilterValue
}

func GitCommitEvent(filters GitCommitFilters) Event {
	f := map[string]*FilterValue{}

	if filters.Branch != nil {
		f["branch"] = filters.Branch
	}

	return Event{
		Filters: f,
		Provides: []Argument{
			ArgumentCommitSHA,
			ArgumentBranch,
			ArgumentRemoteURL,
		},
	}
}

type GitTagFilters struct {
	Name *FilterValue
}

func GitTagEvent(filters GitTagFilters) Event {
	f := map[string]*FilterValue{}
	f["tag"] = filters.Name

	return Event{
		Filters: f,
		Provides: []Argument{
			ArgumentCommitSHA,
			ArgumentCommitRef,
			ArgumentRemoteURL,
		},
	}
}

type PullRequestFilters struct{}

func PullRequestEvent(filters PullRequestFilters) Event {
	f := map[string]*FilterValue{}

	return Event{
		Filters:  f,
		Provides: []Argument{},
	}
}
