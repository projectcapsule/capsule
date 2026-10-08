---
appliesTo:
  - { type: capability, id: request-resource-permit }
  - { type: capability, id: approve-resource-permit }
references:
  - { kind: code, role: implementation, target: "internal/webhook/resourcepermit/resourcepermit_validating.go" }
---

# A ResourcePermit stays within its template's limits

A permit's parameters match the template's schema and its duration never exceeds the template's maximum duration, when requested and when approved.
