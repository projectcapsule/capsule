---
id: capsule
summary: Kubernetes multi-tenancy that groups namespaces into tenants, lets tenant owners provision them on their own and profiles every namespace through rules.
category: platform-engineering
tags: [kubernetes, multi-tenancy, policy, admission-control]
authors:
  - name: Project Capsule Authors
    url: https://projectcapsule.dev
license: Apache-2.0
languages: [en]
limitations:
  - Capsule works inside one Kubernetes cluster and only through the Kubernetes API.
  - People use the identities their Kubernetes cluster already authenticates; Capsule never manages credentials.
  - Who may write Tenants and Capsule's other cluster-wide resources is left to the cluster's own access control.
  - Mutation rules change only new pods and ephemeral containers newly added to a pod; running containers are never changed.
  - An audit rule records events and never refuses a request.
references:
  - { kind: doc, role: context, target: "README.md" }
  - { kind: doc, role: context, target: "https://projectcapsule.dev" }
---

# Capsule

Capsule turns one Kubernetes cluster into a shared, policy-based environment. Administrators group namespaces into
tenants and give each tenant owners; tenant owners then create namespaces and work in them without the
administrator. Every namespace inherits its tenant's policy, and the tenant's ordered rules compose a profile for
each namespace: what may be admitted there, what is written into new workloads, who holds which roles and which
quotas apply. Capsule also replicates objects into tenant namespaces, shares quotas and pools of resources across
namespaces, and grants resources on request for a limited time.

## Intent

Let many teams share a cluster safely instead of each running its own, with the isolation, governance and resource
control of separate clusters and a native Kubernetes experience that stays declarative.
