# Assign Trusted Images Registries
Bill, the cluster admin, can set a strict policy on the applications running into Alice's tenant: he'd like to allow running just images hosted on a list of specific container registries.

The spec `containerRegistries` addresses this task and can provide combination with hard enforcement using a list of allowed values.


```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: oil
spec:
  owner:
    name: alice
    kind: User
  containerRegistries:
    allowed:
    - docker.io
    - quay.io
    allowedRegex: ''
```

> In case of naked and official images hosted on Docker Hub, Capsule is going
> to retrieve the registry even if it's not explicit: a `busybox:latest` Pod
> running on a Tenant allowing `docker.io` will not blocked, even if the image
> field is not explicit as `docker.io/busybox:latest`.


Alternatively, use a valid regular expression for a maximum flexibility

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: oil
spec:
  owner:
    name: alice
    kind: User
  containerRegistries:
    allowed: []
    regex: "internal.registry.\\w.tld"
```

A Pod running `internal.registry.foo.tld` as registry will be allowed, as well `internal.registry.bar.tld` since these are matching the regular expression.

> You can also set a catch-all as .* to allow every kind of registry,
> that would be the same result of unsetting `containerRegistries` at all

As per Ingress and Storage classes, also the allowed registries can be inspected from the Tenant's namespace

```
alice@caas# kubectl describe ns oil-production
Name:         oil-production
Labels:       capsule.clastix.io/tenant=oil
Annotations:  capsule.clastix.io/allowed-registries: docker.io
              capsule.clastix.io/allowed-registries-regexp: ^registry\.internal\.\w+$
...
```

# What’s next
See how Bill, the cluster admin, can assign Pod Security Policies to Alice's tenant. [Assign Pod Security Policies](./pod-security-policies.md).

