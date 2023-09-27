# Introduction

The integration between Rancher and Capsule, aims to provide a multi-tenant Kubernetes service to users, enabling:
- a self-service approach
- access to cluster-wide resources

to end-users.

Tenant users will have the ability to access Kubernetes resources through:
- Rancher UI
- Rancher Shell
- Kubernetes CLI

On the other side, administrators need to manage the Kubernetes clusters through Rancher.

Rancher provides a feature called **Projects** to segregate resources inside a common domain.
At the same time Projects doesn't provide way to segregate Kubernetes cluster-scope resources.

Capsule as a project born for creating a framework for multi-tenant platforms, integrates with Rancher Projects enhancing the experience with **Tenants**.

Capsule allows tenants isolation and resources control in a declarative way, while enabling a self-service experience to tenants.
With Capsule Proxy users can also access cluster-wide resources, as configured by administrators at `Tenant` custom resource-level.

You can read in detail how the integration works and how to configure it, in the following guides.
- [How to integrate Rancher Projects with Capsule Tenants](./capsule-proxy-rancher.md)
- [How to enable cluster-wide resources and Rancher shell access](./capsule-proxy-rancher.md).

