---
availability:
  - { place: kubernetes-api::cluster-administration }
domain: tenants
references:
  - { kind: code, role: implementation, target: "internal/controllers/tenant/status.go" }
---

# Uncordon tenant

An administrator lifts a tenant's freeze.
