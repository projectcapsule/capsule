---
appliesTo:
  - { type: capability, id: create-persistent-volume-claim }
  - { type: entity, id: persistent-volume-claim, facts: [Volume, Volume selector] }
references:
  - { kind: code, role: implementation, target: "internal/webhook/pvc/pvc_validating_volume.go" }
  - { kind: code, role: implementation, target: "internal/controllers/pv/controller.go" }
---

# A PersistentVolumeClaim binds only volumes of its own tenant

A claim that selects or names a volume is limited to volumes labelled with its tenant.
