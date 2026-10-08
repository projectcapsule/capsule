---
appliesTo:
  - { type: entity, id: resource-permit, effect: changes, facts: [Rendered resources, Resolved identity] }
permits:
  - unattended: true
references:
  - { kind: code, role: implementation, target: "internal/webhook/resourcepermit/resourcepermit_validating.go" }
  - { kind: code, role: implementation, target: "internal/webhook/resourcepermit/resourcepermit_mutating.go" }
---

# Only Capsule sets a permit's rendered resources and resolved identity

Reviewers and requesters change a permit only by asking for a phase change; what the template rendered and which identity applies it are set by Capsule alone, and the reviewer recorded is always the identity that made the request.
