---
appliesTo:
  - { type: entity, id: global-resource-quota, effect: removes }
permits:
  - configuredBy: kubernetes-role
  - unattended: true
references:
  - { kind: code, role: context, target: "internal/controllers/rbac/manager.go" }
---

# Only identities a Kubernetes role allows delete GlobalResourceQuotas

Capsule validates what is written but does not check who deletes GlobalResourceQuotas; the cluster's Kubernetes roles decide. Capsule itself keeps the GlobalResourceQuotas that namespace rules declare.
