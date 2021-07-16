# Enforcing Pod containers image PullPolicy

Bill is a cluster admin providing a Container as a Service platform using shared nodes.

Alice, a Tenant Owner, can start container images using private images: according to the Kubernetes architecture, the `kubelet` will download the layers on its cache.

Bob, an attacker, could try to schedule a Pod on the same node where Alice is running their Pod backed by private images: they could start new Pods using `ImagePullPolicy=IfNotPresent` and able to start them, even without required authentication since the image is cached on the node. 

To avoid this kind of attack all the Tenant Owners must start their Pods using the `ImagePullPolicy` to `Always`, enforcing the `kubelet` to check the authorization first.

Capsule provides a way to enforce this behavior, as follows.

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: oil
  annotations:
    capsule.clastix.io/allowed-image-pull-policy: Always
spec:
  owner:
    name: alice
    kind: User
```

If you need to address specific use-case, the said annotation supports multiple values comma separated

```yaml
capsule.clastix.io/allowed-image-pull-policy: Always,IfNotPresent
```

# What’s next

See how Bill, the cluster admin, can assign trusted images registries to Alice's tenant. [Assign Trusted Images Registries](./images-registries.md).
