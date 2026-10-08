---
appliesTo:
  - { type: entity, id: resource-permit-template, effect: creates }
permits:
  - configuredBy: kubernetes-role
references:
  - { kind: code, role: context, target: "internal/controllers/rbac/manager.go" }
---

# Only identities a Kubernetes role allows create ResourcePermitTemplates

Capsule validates what is written but does not check who creates ResourcePermitTemplates; the cluster's Kubernetes roles decide.
