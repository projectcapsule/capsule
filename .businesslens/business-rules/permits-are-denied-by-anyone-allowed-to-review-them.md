---
appliesTo:
  - { type: entity, id: resource-permit, effect: changes, to: Denied }
permits:
  - configuredBy: kubernetes-role
references:
  - { kind: code, role: implementation, target: "internal/webhook/resourcepermit/resourcepermit_mutating.go" }
  - { kind: code, role: implementation, target: "api/v1beta2/resourcepermit_func.go#DenyPermit" }
---

# Anyone a Kubernetes role lets update a ResourcePermit's status may deny it

Denying a permit is not limited to the approvers its template names: Capsule records whoever denies it as the reviewer, and the cluster's Kubernetes roles decide who may.
