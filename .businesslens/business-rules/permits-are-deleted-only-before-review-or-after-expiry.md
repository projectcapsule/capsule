---
appliesTo:
  - { type: entity, id: resource-permit, effect: removes }
permits:
  - actors: [tenant-owner]
    when:
      - { state: Created }
      - { entity: tenant, fact: Cordoned, is: false }
  - actors: [tenant-owner]
    when:
      - { state: Requested }
      - { entity: tenant, fact: Cordoned, is: false }
  - actors: [tenant-owner]
    when:
      - { state: Expired }
      - { entity: tenant, fact: Cordoned, is: false }
  - actors: [administrator]
  - unattended: true
    when:
      - { state: Expired }
references:
  - { kind: code, role: implementation, target: "internal/webhook/resourcepermit/resourcepermit_validating.go" }
---

# A ResourcePermit is deleted only before review or after it has expired

A tenant owner deletes a permit while it is Created or Requested, or once it is Expired and its retention has ended, and never while the tenant is cordoned. Administrators delete permits in any phase; Capsule deletes expired permits when their retention ends. While its namespace is terminating, a permit can be deleted in any phase.
