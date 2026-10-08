---
appliesTo:
  - { type: entity, id: granted-resource, effect: removes }
permits:
  - unattended: true
references:
  - { kind: code, role: implementation, target: "internal/webhook/generic/resourcepermit.go" }
---

# Only its permit deletes a protected granted resource

A protected granted resource is deleted only by Capsule when its ResourcePermit expires, or together with its namespace when that namespace is deleted.
