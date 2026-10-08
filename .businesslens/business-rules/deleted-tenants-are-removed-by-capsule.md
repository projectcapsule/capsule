---
appliesTo:
  - { type: entity, id: tenant, effect: removes }
permits:
  - configuredBy: kubernetes-role
  - unattended: true
references:
  - { kind: code, role: context, target: "internal/controllers/rbac/manager.go" }
---

# Capsule removes a deleted Tenant once nothing of it remains

A Tenant that is terminating stays until its namespaces and the quotas its rules generated are gone; Capsule then removes it. Who may ask for the deletion is decided by the cluster's Kubernetes roles.
