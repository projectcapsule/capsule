---
appliesTo:
  - { type: entity, id: resource-permit, effect: changes, to: Retrying }
permits:
  - configuredBy: kubernetes-role
references:
  - { kind: code, role: implementation, target: "api/v1beta2/resourcepermit_func.go#RetryPermit" }
---

# Anyone a Kubernetes role lets update a ResourcePermit's status may retry it once it has failed

Only a Failed permit with recorded failure details can be retried; who may ask is left to the cluster's Kubernetes roles.
