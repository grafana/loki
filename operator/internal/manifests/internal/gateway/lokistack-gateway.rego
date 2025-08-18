package lokistack

import rego.v1

import data.roleBindings
import data.roles
import input

default allow := false

allow if {
	some roleNames
	roleNames = roleBindings[matched_role_binding[_]].roles
	roles[i].name == roleNames[_]
	roles[i].resources[_] = input.resource
	roles[i].permissions[_] = input.permission
	roles[i].tenants[_] = input.tenant
}

matched_role_binding contains i if {
	roleBindings[i].subjects[_] == {"name": input.subject, "kind": "user"}
}

matched_role_binding contains i if {
	roleBindings[i].subjects[_] == {"name": input.groups[_], "kind": "group"}
}
