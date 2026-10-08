---
appliesTo:
  - { type: entity, id: resource-permit, effect: changes, to: Expired }
permits:
  - configuredBy: kubernetes-role
  - unattended: true
references:
  - { kind: code, role: implementation, target: "api/v1beta2/resourcepermit_func.go#ExpirePermit" }
  - { kind: code, role: implementation, target: "internal/controllers/resourcepermit/resourcepermit_controller.go" }
---

# Anyone a Kubernetes role lets update a ResourcePermit's status may expire it, and Capsule expires it when its duration ends

A permit can be ended from any phase by anyone the cluster's Kubernetes roles allow to update its status; Capsule records who ended it.
