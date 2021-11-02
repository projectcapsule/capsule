# Use cases for Capsule
Using Capsule, a cluster admin can implement complex multi-tenant scenarios for both public and private deployments. Here is a list of common scenarios addressed by Capsule.

# Container as a Service (CaaS)
***Acme Corp***, our sample organization, built a Container as a Service platform (CaaS), based on Kubernetes to serve multiple lines of business. Each line of business has its team of engineers that are responsible for the development, deployment, and operating of their digital products.

To simplify the usage of Capsule in this scenario, we'll work with the following actors:

* ***Bill***:
  he is the cluster administrator from the operations department of Acme Corp. and he is in charge of administration and maintains the CaaS platform.

* ***Alice***:
  she works as the IT Project Leader in the Oil & Gas Business Units. These are two new lines of business at Acme Corp. Alice is responsible for all the strategic IT projects in the two LOBs. She also is responsible for a team made of different job responsibilities (developers, administrators, SRE engineers, etc.) working in separate departments.
  
* ***Joe***:
  he works at Acme Corp, as a lead developer of a distributed team in Alice's organization. Joe is responsible for developing a mission-critical project in the Oil market.

* ***Bob***:
  he is the head of Engineering for the Water Business Unit, the main and historical line of business at Acme Corp. He is responsible for the development, deployment, and operation of multiple digital products in production for a large set of customers.

Use Capsule to address any of the following scenarios:

* [Assign Tenant Ownership](/docs/operator/use-cases/tenant-ownership)
* [Create Namespaces](/docs/operator/use-cases/create-namespaces)
* [Assign Permissions](/docs/operator/use-cases/permissions)
* [Enforce Resources Quotas and Limits](/docs/operator/use-cases/resources-quota-limits)
* [Enforce Pod Priority Classes](/docs/operator/use-cases/pod-priority-classes)
* [Assign specific Node Pools](/docs/operator/use-cases/nodes-pool)
* [Assign Ingress Classes](/docs/operator/use-cases/ingress-classes)
* [Assign Ingress Hostnames](/docs/operator/use-cases/ingress-hostnames)
* [Control hostname collision in Ingresses](/docs/operator/use-cases/hostname-collision)
* [Assign Storage Classes](/docs/operator/use-cases/storage-classes)
* [Assign Network Policies](/docs/operator/use-cases/network-policies)
* [Enforce Containers image PullPolicy](/docs/operator/use-cases/images-pullpolicy)
* [Assign Trusted Images Registries](/docs/operator/use-cases/images-registries)
* [Assign Pod Security Policies](/docs/operator/use-cases/pod-security-policies)
* [Create Custom Resources](/docs/operator/use-cases/custom-resources)
* [Taint Namespaces](/docs/operator/use-cases/taint-namespaces)
* [Assign multiple Tenants](/docs/operator/use-cases/multiple-tenants)
* [Cordon Tenants](/docs/operator/use-cases/cordoning-tenant)
* [Disable Service Types](/docs/operator/use-cases/service-type)
* [Taint Services](/docs/operator/use-cases/taint-services)
* [Allow adding labels and annotations on namespaces](/docs/operator/use-cases/namespace-labels-and-annotations)
* [Velero Backup Restoration](/docs/operator/use-cases/velero-backup-restoration)
* [Deny Wildcard Hostnames](/docs/operator/use-cases/deny-wildcard-hostnames)
* [Denying specific user-defined labels or annotations on Nodes](/docs/operator/use-cases/deny-specific-user-defined-labels-or-annotations-on-nodes)

> NB: as we improve Capsule, more use cases about multi-tenancy and cluster governance will be covered.

# What’s next
Now let's see how the cluster admin onboards a new tenant. [Onboarding a new tenant](/docs/operator/use-cases/onboarding).
