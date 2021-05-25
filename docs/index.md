# Capsule Documentation
**Capsule** helps to implement a multi-tenancy and policy-based environment in your Kubernetes cluster. It has been designed as a micro-services based ecosystem with the minimalist approach, leveraging only on upstream Kubernetes. 

Currently, the Capsule ecosystem comprises the following:

* [Capsule Operator](./operator/overview.md)
* [Capsule Proxy](./proxy/overview.md)
* [Capsule Lens extension](lens-extension/overview.md)  Coming soon!

## Documents structure
```command
docs
├── index.md
├── lens-extension
│   └── overview.md
├── proxy
│   ├── overview.md
│   ├── sidecar.md
│   └── standalone.md
└── operator
    ├── contributing.md
    ├── getting-started.md
    ├── monitoring.md
    ├── overview.md
    ├── references.md
    └── use-cases
        ├── create-namespaces.md
        ├── custom-resources.md
        ├── images-registries.md
        ├── ingress-classes.md
        ├── ingress-hostnames.md
        ├── multiple-tenants.md
        ├── network-policies.md
        ├── node-ports.md
        ├── nodes-pool.md
        ├── onboarding.md
        ├── overview.md
        ├── permissions.md
        ├── pod-priority-class.md
        ├── pod-security-policies.md
        ├── resources-quota-limits.md
        ├── storage-classes.md
        └── taint-namespaces.md
```
