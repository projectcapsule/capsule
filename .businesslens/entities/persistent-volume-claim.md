---
references:
  - { kind: code, role: implementation, target: "internal/webhook/pvc/handler.go" }
---

# PersistentVolumeClaim

A Kubernetes PersistentVolumeClaim in a tenant namespace.

## Information kept

- **Storage class** — its storage class
- **Volume selector** — the selector choosing which volumes it may bind
- **Volume** — the named volume it binds, and the tenant that volume belongs to
