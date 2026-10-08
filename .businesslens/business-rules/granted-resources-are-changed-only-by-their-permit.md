---
appliesTo:
  - { type: entity, id: granted-resource, effect: changes }
permits:
  - unattended: true
references:
  - { kind: code, role: implementation, target: "internal/webhook/generic/resourcepermit.go" }
---

# Only its permit changes a protected granted resource

A protected granted resource is changed only by Capsule, acting for the ResourcePermit that created it.
