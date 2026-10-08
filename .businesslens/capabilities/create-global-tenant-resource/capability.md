---
availability:
  - { place: kubernetes-api::cluster-administration }
domain: replications
references:
  - { kind: code, role: implementation, target: "api/v1beta2/tenantresource_global.go" }
  - { kind: doc, role: context, target: "https://projectcapsule.dev/docs/replications/global/" }
---

# Create GlobalTenantResource

An administrator sets up replication of objects into the namespaces of tenants selected by label.
