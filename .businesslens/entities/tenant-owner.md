---
kind: person
acts: external
relations:
  - { entity: tenant, verb: owns, cardinality: many-to-many }
references:
  - { kind: code, role: implementation, target: "pkg/tenant/owners.go#CollectOwners" }
  - { kind: code, role: implementation, target: "pkg/users/is_capsule_user.go#IsCapsuleUser" }
---

# Tenant owner

A user, group or ServiceAccount that owns one or more tenants and works in their namespaces without the administrator. An identity is a tenant owner when a Tenant lists it as an owner, when it matches a TenantOwner a Tenant selects, or when it is a ServiceAccount promoted to owner. Other identities granted roles in tenant namespaces meet the same enforcement.
