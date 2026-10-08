---
appliesTo:
  - { type: capability, id: request-resource-permit }
  - { type: entity, id: resource-permit, facts: [Requestor] }
references:
  - { kind: code, role: implementation, target: "internal/webhook/resourcepermit/resourcepermit_mutating.go" }
---

# A ResourcePermit's requestor is always the identity that created it

Whatever requestor a request names, Capsule records the identity that made it.
