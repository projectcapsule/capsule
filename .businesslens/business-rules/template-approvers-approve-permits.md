---
appliesTo:
  - { type: entity, id: resource-permit, effect: changes, to: Approved }
permits:
  - related: [{ verb: offers, entity: resource-permit-template }, { verb: names, entity: permit-approver }]
  - related: [{ verb: offers, entity: global-resource-permit-template }, { verb: names, entity: permit-approver }]
  - actors: [permit-approver]
    when:
      - { entity: resource-permit-template, fact: Approvers, absent: true }
  - actors: [permit-approver]
    when:
      - { entity: global-resource-permit-template, fact: Approvers, absent: true }
  - unattended: true
    when:
      - { entity: resource-permit-template, fact: Auto approval, is: true }
  - unattended: true
    when:
      - { entity: global-resource-permit-template, fact: Auto approval, is: true }
references:
  - { kind: code, role: implementation, target: "internal/webhook/resourcepermit/resourcepermit_validating.go" }
---

# Only a template's approvers approve its permits, or anyone when it names none

A requested permit is approved by an approver its template names, by any permit approver when the template names none, or by Capsule when the template approves automatically. Self-approval is not refused unless an approval condition refuses it.
