# Use cases for Capsule
Using Capsule, a cluster admin can implement complex multi-tenants scenarios for both public and private deployments. Here a list of common scenarios addressed by Capsule.

## Acme Corp. Container as a Service (CaaS)
Acme Corp. deployed a Container as a Service platform, based on Kubernetes, serving multiple lines of business in their organization. Each line of business, has its own team of engineers that are responsible for development, deployment, and operating their digital set of products.

To simplify the usage of Capsule in this scenario, we'll work with the following actors:

* *Bill*:
  he is the cluster administrator from the operations department of Acme Corp. and he is in charge of admin and maintains the CaaS platform.

* *Alice*:
  she works as IT Project Leader at Oil & Gas Business Units, two new lines of business at Acme Corp. Alice is responsible for all the strategic IT projects and she is responsible also for a team made of different background (developers, administrators, SRE engineers, etc.) and organized in separate departments.

* *Joe*:
  he works at Acme Corp., as a lead developer of a distributed team in Alice's organization.
  Joe is responsible for developing a mission-critical project in the Gas market.

* *Bob*:
  he is the head of Engineering for the Water Business Unit, the main and historichal line of business at Acme Corp. He is responsible for development, deployment, and operating multiple digital products in production for a large set of customers.

Bill, at Acme Corp. can use Capsule to address any of the following scenarios:

* Onboarding of a new tenant

  How Bill puts a new tenant onboard.

* Create multiple namespaces in a tenant

  How Alice, the tenant owner, creates new namespaces in her tenants.

* Assign permissions roles in a tenant
  
  How Alice, the tenant owner, can assign different user roles in her tenants.

* Resources quota and limits enforcement for a tenant
  
  How Bill, the cluster admin, set resources quota and limits for Alice's tenant.

* Assign nodes pool to a tenant
  
  How Bill, the cluster admin, can assign a pool of nodes to Alice's tenant.

* Assign Ingress Classes to a tenant
  
  How Bill, the cluster admin, can assign an Ingress Class to Alice's tenant.

* Assign Storage Classes to a tenant
  
  How Bill, the cluster admin, can assign an Ingress Class to Alice's tenant.

* Assign Network Policies to a tenant
  
  How Bill, the cluster admin, can assign Network Policies to Alice's tenant.

* Assign Trusted Images Registries to a tenant
  
  How Bill, the cluster admin, can assign trusted images registries to Alice's tenant.

* Assign Pod Security Policies to a tenant
  
  How Bill, the cluster admin, can assign Pod Security Policies to Alice's tenant.

* Create custom resources in a tenant

  How Bill, the cluster admin, can assign to Alice the permissions to create custom resources in her tenant. 

* Enable tenant backup

  How Bill, the cluster admin, can backup Alice's tenant.

* Multiple tenants owned by the same user

  How Bill, the cluster admin, can assign to Alice the onwership of multiple tenants.

# What’s next
See how the cluster admin puts a new tenant on board: [Onboarding of a new tenant]().