---
availability:
  - { place: kubernetes-api::cluster-administration }
domain: tenants
references:
  - { kind: code, role: implementation, target: "api/v1beta2/tenant_types.go#TenantSpec" }
---

# Edit tenant

An administrator changes a Tenant's policy: namespace quota, prefix rule, node selector, allowed classes, ingress and Service options, distributed quotas, limit ranges and network policies, and additional role bindings.
