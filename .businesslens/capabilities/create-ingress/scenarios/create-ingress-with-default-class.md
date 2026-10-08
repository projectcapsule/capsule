---
kind: primary
routes:
  api: Kubernetes API
steps:
  - text: "The Tenant owner submits an Ingress without a class for an allowed hostname"
    kind: actor
    actor: tenant-owner
    entities:
      - { entity: ingress, effect: reads, facts: [Hostnames] }
      - { entity: tenant, effect: reads, facts: [Ingress options] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
  - text: "The Product sets the default ingress class of the Tenant, checks the hostnames and admits the Ingress"
    kind: product
    actor: tenant-owner
    entities:
      - { entity: ingress, effect: creates, facts: [Ingress class, Hostnames] }
      - { entity: tenant, effect: reads, facts: [Ingress options] }
    contexts:
      api: { place: kubernetes-api::tenant-workspace }
---

# Create an Ingress with the tenant's default class

## Trigger

A tenant owner publishes an application on a hostname the tenant allows.

## Outcome

The Ingress exists with the Tenant's default ingress class.

## Edge cases

- An ingress class outside the Tenant's allowed classes is refused; with no default, a missing class is refused.
- Hostnames outside the Tenant's allowed hostnames are refused.
- Hostname rules also check TLS hosts, OpenShift Routes and Gateway API listeners and routes.
