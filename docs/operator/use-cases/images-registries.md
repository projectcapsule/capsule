# Assign Trusted Images Registries
Bill, the cluster admin, can set a strict policy on the applications running into Alice's tenant: he'd like to allow running just images hosted on a list of specific container registries.

The spec `containerRegistries` addresses this task and can provide a combination with hard enforcement using a list of allowed values.


```yaml
kubectl -n oil-production apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  containerRegistries:
    allowed:
    - docker.io
    - quay.io
    allowedRegex: 'internal.registry.\\w.tld'
```

> In case of `non-FQDI` (non fully qualified Docker image) and official images hosted on Docker Hub,
> Capsule is going to retrieve the registry even if it's not explicit: a `busybox:latest` Pod
> running on a Tenant allowing `docker.io` will not be blocked, even if the image
> field is not explicit as `docker.io/busybox:latest`.

A Pod running `internal.registry.foo.tld` as registry will be allowed, as well `internal.registry.bar.tld` since these are matching the regular expression.

> A catch-all regex entry as `.*` allows every kind of registry, which would be the same result of unsetting `containerRegistries` at all.

Any attempt of Alice to use a not allowed `containerRegistries` value is denied by the Validation Webhook enforcing it.

# What’s next
See how Bill, the cluster admin, can assign Pod Security Policies to Alice's tenant. [Assign Pod Security Policies](./pod-security-policies.md).
