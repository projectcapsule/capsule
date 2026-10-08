---
availability:
  - { place: kubernetes-api::cluster-administration }
domain: tenants
references:
  - { kind: code, role: implementation, target: "internal/webhook/generic/cordoning.go" }
  - { kind: code, role: implementation, target: "internal/webhook/namespace/validation/cordoning.go" }
---

# Cordon tenant

An administrator freezes a tenant so its owners can no longer change anything in it.
