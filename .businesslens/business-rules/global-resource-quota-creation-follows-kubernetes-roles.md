---
appliesTo:
  - { type: entity, id: global-resource-quota, effect: creates }
permits:
  - configuredBy: kubernetes-role
  - unattended: true
references:
  - { kind: code, role: context, target: "internal/controllers/rbac/manager.go" }
---

# Only identities a Kubernetes role allows create GlobalResourceQuotas

Capsule validates what is written but does not check who creates GlobalResourceQuotas; the cluster's Kubernetes roles decide. Capsule itself keeps the GlobalResourceQuotas that namespace rules declare.
