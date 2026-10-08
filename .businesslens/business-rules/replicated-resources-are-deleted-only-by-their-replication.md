---
appliesTo:
  - { type: entity, id: replicated-resource, effect: removes }
permits:
  - unattended: true
references:
  - { kind: code, role: implementation, target: "internal/webhook/generic/replications.go" }
  - { kind: code, role: implementation, target: "internal/webhook/generic/termination.go" }
---

# Only its replication deletes a protected replicated resource

A protected replicated resource is deleted only by Capsule, acting for its replication, or together with its namespace when that namespace is deleted.
