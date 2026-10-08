---
availability:
  - { place: kubernetes-api::tenant-workspace }
references:
  - { kind: code, role: implementation, target: "internal/webhook/pvc/pvc_validating_class.go" }
  - { kind: code, role: implementation, target: "internal/webhook/pvc/pvc_validating_volume.go" }
  - { kind: code, role: implementation, target: "internal/webhook/pvc/pvc_mutating_volume.go" }
  - { kind: code, role: implementation, target: "internal/webhook/defaults/storage.go" }
  - { kind: code, role: implementation, target: "internal/controllers/pv/controller.go" }
---

# Create PersistentVolumeClaim

A tenant owner creates a PersistentVolumeClaim in a tenant namespace, admitted only with an allowed storage class and only for volumes of the same tenant.
