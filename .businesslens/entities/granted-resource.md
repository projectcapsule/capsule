---
references:
  - { kind: code, role: implementation, target: "internal/webhook/generic/resourcepermit.go" }
---

# Granted resource

An object a ResourcePermit created while it is active.

## Information kept

- **Active until** — when the permit that granted it expires
- **Protected** — whether changes by anyone but the permit are refused
- **Deletion policy** — whether it is removed or left in place when the permit expires
