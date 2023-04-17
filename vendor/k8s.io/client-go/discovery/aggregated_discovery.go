/*
Copyright 2022 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package discovery

import (
	"fmt"

	apidiscovery "k8s.io/api/apidiscovery/v2beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// SplitGroupsAndResources transforms "aggregated" discovery top-level structure into
// the previous "unaggregated" discovery groups and resources.
func SplitGroupsAndResources(aggregatedGroups apidiscovery.APIGroupDiscoveryList) (*metav1.APIGroupList, map[schema.GroupVersion]*metav1.APIResourceList) {
	// Aggregated group list will contain the entirety of discovery, including
	// groups, versions, and resources.
	groups := []*metav1.APIGroup{}
	resourcesByGV := map[schema.GroupVersion]*metav1.APIResourceList{}
	for _, aggGroup := range aggregatedGroups.Items {
		group, resources := convertAPIGroup(aggGroup)
		groups = append(groups, group)
		for gv, resourceList := range resources {
			resourcesByGV[gv] = resourceList
		}
	}
	// Transform slice of groups to group list before returning.
	groupList := &metav1.APIGroupList{}
	groupList.Groups = make([]metav1.APIGroup, 0, len(groups))
	for _, group := range groups {
		groupList.Groups = append(groupList.Groups, *group)
	}
	return groupList, resourcesByGV
}

// convertAPIGroup tranforms an "aggregated" APIGroupDiscovery to an "legacy" APIGroup,
// also returning the map of APIResourceList for resources within GroupVersions.
func convertAPIGroup(g apidiscovery.APIGroupDiscovery) (*metav1.APIGroup, map[schema.GroupVersion]*metav1.APIResourceList) {
	// Iterate through versions to convert to group and resources.
	group := &metav1.APIGroup{}
	gvResources := map[schema.GroupVersion]*metav1.APIResourceList{}
	group.Name = g.ObjectMeta.Name
	for i, v := range g.Versions {
		version := metav1.GroupVersionForDiscovery{}
		gv := schema.GroupVersion{Group: g.Name, Version: v.Version}
		version.GroupVersion = gv.String()
		version.Version = v.Version
		group.Versions = append(group.Versions, version)
		if i == 0 {
			group.PreferredVersion = version
		}
		resourceList := &metav1.APIResourceList{}
		resourceList.GroupVersion = gv.String()
		for _, r := range v.Resources {
			resource := convertAPIResource(r)
			resourceList.APIResources = append(resourceList.APIResources, resource)
			// Subresources field in new format get transformed into full APIResources.
			for _, subresource := range r.Subresources {
				sr := convertAPISubresource(resource, subresource)
				resourceList.APIResources = append(resourceList.APIResources, sr)
			}
		}
		gvResources[gv] = resourceList
	}
	return group, gvResources
}

// convertAPIResource tranforms a APIResourceDiscovery to an APIResource.
func convertAPIResource(in apidiscovery.APIResourceDiscovery) metav1.APIResource {
	return metav1.APIResource{
		Name:         in.Resource,
		SingularName: in.SingularResource,
		Namespaced:   in.Scope == apidiscovery.ScopeNamespace,
		Group:        in.ResponseKind.Group,
		Version:      in.ResponseKind.Version,
		Kind:         in.ResponseKind.Kind,
		Verbs:        in.Verbs,
		ShortNames:   in.ShortNames,
		Categories:   in.Categories,
	}
}

// convertAPISubresource tranforms a APISubresourceDiscovery to an APIResource.
func convertAPISubresource(parent metav1.APIResource, in apidiscovery.APISubresourceDiscovery) metav1.APIResource {
	return metav1.APIResource{
		Name:         fmt.Sprintf("%s/%s", parent.Name, in.Subresource),
		SingularName: parent.SingularName,
		Namespaced:   parent.Namespaced,
		Group:        in.ResponseKind.Group,
		Version:      in.ResponseKind.Version,
		Kind:         in.ResponseKind.Kind,
		Verbs:        in.Verbs,
	}
}
