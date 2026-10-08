---
availability:
  - { place: kubernetes-api::cluster-administration }
domain: tenants
references:
  - { kind: code, role: implementation, target: "internal/controllers/tenant/rolebindings.go" }
---

# Remove tenant owner

An administrator removes a user, group or ServiceAccount from a Tenant's owners.
