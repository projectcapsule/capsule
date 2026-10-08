---
domain: replications
references:
  - { kind: code, role: implementation, target: "internal/webhook/generic/replications.go" }
  - { kind: code, role: implementation, target: "pkg/api/processor/processor_func.go" }
---

# Replicated resource

An object a TenantResource or GlobalTenantResource placed in a namespace.

## Information kept

- **Origin** — the replication and the item that produced it
- **Protected** — whether changes by anyone but its replication are refused
- **Deletion policy** — whether it is removed or left in place when its replication stops producing it
