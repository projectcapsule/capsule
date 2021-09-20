# Enforcing Pod containers image PullPolicy

Bill is a cluster admin providing a Container as a Service platform using shared nodes.

Alice, a Tenant Owner, can start container images using private images: according to the Kubernetes architecture, the `kubelet` will download the layers on its cache.

Bob, an attacker, could try to schedule a Pod on the same node where Alice is running her Pods backed by private images: they could start new Pods using `ImagePullPolicy=IfNotPresent` and be able to start them, even without required authentication since the image is cached on the node. 

To avoid this kind of attack, Bill, the cluster admin, can force Alice, the tenant owner, to start her Pods using only the allowed values for `ImagePullPolicy`, enforcing the `kubelet` to check the authorization first.

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
  imagePullPolicies:
  - Always
EOF
```

Allowed values are: `Always`, `IfNotPresent`, `Never`.

Any attempt of Alice to use a disallowed `imagePullPolicies` value is denied by the Validation Webhook enforcing it.

# What’s next

See how Bill, the cluster admin, can assign trusted images registries to Alice's tenant. [Assign Trusted Images Registries](./images-registries.md).
