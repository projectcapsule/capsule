---
availability:
  - { place: kubernetes-api::cluster-administration }
domain: replications
references:
  - { kind: code, role: implementation, target: "internal/controllers/resources/global.go" }
---

# Replicate global tenant resources

Capsule keeps the objects a GlobalTenantResource describes present and in sync in the selected tenants, per namespace, per tenant or once for the cluster.
