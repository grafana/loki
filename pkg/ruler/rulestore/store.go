package rulestore

import (
	"context"
	"errors"

	"github.com/cortexproject/cortex/pkg/ruler/rulespb"
)

var (
	// ErrGroupNotFound is returned if a rule group does not exist
	ErrGroupNotFound = errors.New("group does not exist")
	// ErrGroupNamespaceNotFound is returned if a namespace does not exist
	ErrGroupNamespaceNotFound = errors.New("group namespace does not exist")
	// ErrUserNotFound is returned if the user does not currently exist
	ErrUserNotFound = errors.New("no rule groups found for user")
)

// RuleStore is used to store and retrieve rules.
// Methods starting with "List" prefix may return partially loaded groups: with only group Name, Namespace and User fields set.
// To make sure that rules within each group are loaded, client must use LoadRuleGroups method.
type RuleStore interface {
	// ListAllUsers returns all users with rule groups configured.
	ListAllUsers(ctx context.Context) ([]string, error)

	// ListAllRuleGroups returns all rule groups for all users.
	ListAllRuleGroups(ctx context.Context) (map[string]rulespb.RuleGroupList, error)

	// ListRuleGroupsForUserAndNamespace returns all the active rule groups for a user from given namespace.
	// If namespace is empty, groups from all namespaces are returned.
	ListRuleGroupsForUserAndNamespace(ctx context.Context, userID string, namespace string) (rulespb.RuleGroupList, error)

	// LoadRuleGroups loads rules for each rule group in the map.
	// Parameter with groups to load *MUST* be coming from one of the List methods.
	// Reason is that some implementations don't do anything, since their List method already loads the rules.
	LoadRuleGroups(ctx context.Context, groupsToLoad map[string]rulespb.RuleGroupList) error

	GetRuleGroup(ctx context.Context, userID, namespace, group string) (*rulespb.RuleGroupDesc, error)
	SetRuleGroup(ctx context.Context, userID, namespace string, group *rulespb.RuleGroupDesc) error

	// DeleteRuleGroup deletes single rule group.
	DeleteRuleGroup(ctx context.Context, userID, namespace string, group string) error

	// DeleteNamespace lists rule groups for given user and namespace, and deletes all rule groups.
	// If namespace is empty, deletes all rule groups for user.
	DeleteNamespace(ctx context.Context, userID, namespace string) error
}
