---
availability:
  - { place: kubernetes-api::cluster-administration }
domain: tenants
references:
  - { kind: code, role: implementation, target: "internal/webhook/namespace/validation/handler.go" }
---

# Remove namespace from tenant

An administrator takes a namespace out of its tenant without deleting it.
