# Use cases for Capsule
Using Capsule, a cluster admin can implement complex multi-tenants scenarios for both public and private deployments. Here a list of common scenarios addressed by Capsule.

## Container as a Service (CaaS)
Acme Corp., our sample organization, is using a Container as a Service platform (CaaS), based on Kubernetes, serving multiple lines of business. Each line of business, has its own team of engineers that are responsible for development, deployment, and operating their digital products.

To simplify the usage of Capsule in this scenario, we'll work with the following actors:

* *Bill*:
  he is the cluster administrator from the operations department of Acme Corp. and he is in charge of admin and maintains the CaaS platform.

* *Alice*:
  she works as IT Project Leader at Oil & Gas Business Units, two new lines of business at Acme Corp. Alice is responsible for all the strategic IT projects and she is responsible also for a team made of different background (developers, administrators, SRE engineers, etc.) and organized in separate departments.

* *Joe*:
  he works at Acme Corp., as a lead developer of a distributed team in Alice's organization.
  Joe is responsible for developing a mission-critical project in the Oil market.

* *Bob*:
  he is the head of Engineering for the Water Business Unit, the main and historichal line of business at Acme Corp. He is responsible for development, deployment, and operating multiple digital products in production for a large set of customers.

Bill, at Acme Corp. can use Capsule to address any of the following scenarios:

* [Onboarding of a new tenant]()
* [Create multiple namespaces]()
* [Assign permissions roles]()
* [Resources quota and limits enforcement]()
* [Assign nodes pools]()
* [Assign Ingress Classes]()
* [Assign Storage Classes]()
* [Assign Network Policies]()
* [Assign Trusted Images Registries]()
* [Assign Pod Security Policies]()
* [Create custom resources in a tenant]()
* [Taint namespaces]()
* [Multiple tenants owned by the same user]()

# What’s next
See how the cluster admin puts a new tenant on board: [Onboarding of a new tenant]().