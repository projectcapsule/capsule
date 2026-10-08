---
type: api
actors: [administrator, tenant-owner, permit-approver]
references:
  - { kind: code, role: implementation, target: "internal/webhook/router.go" }
  - { kind: code, role: implementation, target: "cmd/controller/main.go" }
---

# Kubernetes API

Capsule's resources (Tenant, TenantOwner, CapsuleConfiguration, TenantResource, GlobalTenantResource, ResourcePool,
ResourcePoolClaim, GlobalResourceQuota, CustomQuota, GlobalCustomQuota, ResourcePermitTemplate,
GlobalResourcePermitTemplate, ResourcePermit) and its admission of ordinary Kubernetes requests, used with any
Kubernetes client or a GitOps tool.
