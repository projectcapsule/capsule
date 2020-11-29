# Onboarding of a new tenant
Bill receives a new request from the Acme Corp.'s CTO asking a new tenant has to be on board for the Oil line of business. Bill creates a new group account `oil` in the Acme Corp. identity management system and he assigns Alice's identity `alice` to the `oil` group. And because, Alice is the tenant owner, she also needs to be assigned the Capsule group defined by `--capsule-user-group` option, which defaults to `capsule.clastix.io`.

To keep the things simple, we assume that Bill just creates a certificate for authentication on
the CaaS platform using X.509 certificate, so Alice's certificate has `"/CN=alice/O=oil/O=capsule.clastix.io"`.

> Please, note Capsule works not only with clients certificate but with any other authentication system in Kubernetes, including OIDC authentication.

Bill creates a new tenant `oil` in the CaaS manangement portal according to the tenant's profile:

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  labels:
  annotations:
  name: oil
spec:
  owner:
    name: oil
    kind: Group
  namespaceQuota: 9
```

Bill checks the new tenant is created and operational:

```
bill@caas# kubectl get tenant oil
NAME   NAMESPACE QUOTA   NAMESPACE COUNT   OWNER NAME   OWNER KIND   NODE SELECTOR   AGE
oil    9                 0                 oil          Group                         3m
```

> Note that namespaces are not yet assigned to the new tenant.
> The tenant owners are free to create their namespaces in a self-service fashion
> and without any intervention from Bill.

Once the new tenant `oil` is in place, Bill sends the login credentials to Alice along with the other relevant tenant details, for logging into the CaaS.

Alice logs into the CaaS by using her credentials and being part of the
`capsule.clastix.io` users group, she inherits the following authorization:

```yaml
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  labels:
  name: namespace-provisioner
rules:
- apiGroups: [""]
  resources: ["namespaces"]
  verbs: ["create"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: namespace-provisioner
subjects:
  - kind: Group
    name: capsule.clastix.io
roleRef:
  kind: ClusterRole
  name: namespace-provisioner
  apiGroup: rbac.authorization.k8s.io
```

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

# What’s next
See how Alice, the tenant owner, creates new namespaces in her tenants [Create multiple namespaces in a tenant]().