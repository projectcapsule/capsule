---
availability:
  - { place: kubernetes-api::cluster-administration }
domain: tenants
references:
  - { kind: code, role: implementation, target: "internal/webhook/namespace/mutation/assignment.go" }
  - { kind: code, role: implementation, target: "internal/webhook/namespace/validation/handler.go" }
---

# Assign namespace to tenant

An administrator puts an existing namespace into a tenant, or moves it from one tenant to another.
