# Denying user-defined labels or annotations

By default, capsule allows tenant owners to add and modify any label or annotation on their namespaces. 

But there are some scenarios, when tenant owners should not have an ability to add or modify specific labels or annotations (for example, this can be labels used in [Kubernetes network policies](https://kubernetes.io/docs/concepts/services-networking/network-policies/) which are added by cluster administrator).

Bill, the cluster admin, can deny Alice to add specific labels and annotations on namespaces:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
  annotations:
    capsule.clastix.io/forbidden-namespace-labels: foo.acme.net, bar.acme.net
    capsule.clastix.io/forbidden-namespace-labels-regexp: .*.acme.net
    capsule.clastix.io/forbidden-namespace-annotations: foo.acme.net, bar.acme.net
    capsule.clastix.io/forbidden-namespace-annotations-regexp: .*.acme.net
spec:
  owners:
  - name: alice
    kind: User
EOF
```

# What’s next
This ends our tour in Capsule use cases. As we improve Capsule, more use cases about multi-tenancy, policy admission control, and cluster governance will be covered in the future.

Stay tuned!