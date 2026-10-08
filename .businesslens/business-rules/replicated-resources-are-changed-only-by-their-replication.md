---
appliesTo:
  - { type: entity, id: replicated-resource, effect: changes }
permits:
  - unattended: true
references:
  - { kind: code, role: implementation, target: "internal/webhook/generic/replications.go" }
---

# Only its replication changes a protected replicated resource

A protected replicated resource is changed only by Capsule, acting for the TenantResource or GlobalTenantResource that applied it. Administrators and tenant owners are refused alike.
