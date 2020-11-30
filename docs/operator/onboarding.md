# Onboarding of a new tenant
Bill receives a new request from the Acme Corp.'s CTO asking a new tenant for Alice's organization has to be on board. Bill assigns the Alice's identity `alice` in the Acme Corp. identity management system. And because, Alice is a tenant owner, Bill needs to assign `alice` the Capsule group defined by `--capsule-user-group` option, which defaults to `capsule.clastix.io`.

To keep the things simple, we assume that Bill just creates a client certificate for authentication using X.509 Certificate Signing Request, so Alice's certificate has `"/CN=alice/O=capsule.clastix.io"`.

Bill creates a new tenant `oil` in the CaaS manangement portal according to the tenant's profile:

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: oil
spec:
  owner:
    name: alice
    kind: User
  namespaceQuota: 3
```

Bill checks the new tenant is created and operational:

```
bill@caas# kubectl get tenant oil
NAME   NAMESPACE QUOTA   NAMESPACE COUNT   OWNER NAME   OWNER KIND   NODE SELECTOR   AGE
oil    9                 0                 alice        User                         3m
```

> Note that namespaces are not yet assigned to the new tenant.
> The tenant owners are free to create their namespaces in a self-service fashion
> and without any intervention from Bill.

Once the new tenant `oil` is in place, Bill sends the login credentials to Alice.

Alice can log in to the CaaS platform and checks if she can create a namespace

```
alice@caas# kubectl auth can-i create namespaces
Warning: resource 'namespaces' is not namespace scoped
yes
``` 

or even delete the namespace

```
alice@caas# kubectl auth can-i delete ns -n oil-production
Warning: resource 'namespaces' is not namespace scoped
yes
```

However, cluster resources are not accessible to Alice

```
alice@caas# kubectl auth can-i get namespaces
Warning: resource 'namespaces' is not namespace scoped
no

alice@caas# kubectl auth can-i get nodes
Warning: resource 'nodes' is not namespace scoped
no

alice@caas# kubectl auth can-i get persistentvolumes
Warning: resource 'persistentvolumes' is not namespace scoped
no
```

including the `Tenant` resources

```
alice@caas# kubectl auth can-i get tenants
Warning: resource 'tenants' is not namespace scoped
no
```

## Assign a group of users as tenant owner
In the example above, Bill assigned the ownership of `oil` tenant to `alice` user. However, is more likely that multiple users in the Alice's oraganization, need to admin the `oil` tenant. In such cases, Bill can assign the ownership of the `oil` tenant to a group of users instead of a single one.

Bill creates a new group account `oil` in the Acme Corp. identity management system and then he assigns Alice's identity `alice` to the `oil` group.

The tenant manifest is modified as in the following:

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: oil
spec:
  owner:
    name: oil
    kind: Group
  namespaceQuota: 3
```

With the snippet above, any user belonging to the Alice's organization will be owner of the `oil` tenant with the same permissions of Alice.

# What’s next
See how Alice, the tenant owner, creates new namespaces in her tenants [Create multiple namespaces in a tenant]().
