package lokistack

import input
import data.roles
import data.roleBindings

default allow = false

allow {
  some roleNames
  roleNames = roleBindings[matched_role_binding[_]].roles
  roles[i].name == roleNames[_]
  roles[i].resources[_] = input.resource
  roles[i].permissions[_] = input.permission
  roles[i].tenants[_] = input.tenant
}

matched_role_binding[i] {
  roleBindings[i].subjects[_] == {"name": input.subject, "kind": "user"}
}

matched_role_binding[i] {
  roleBindings[i].subjects[_] == {"name": input.groups[_], "kind": "group"}
}
