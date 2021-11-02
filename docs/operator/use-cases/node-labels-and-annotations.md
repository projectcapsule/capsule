# Denying specific user-defined labels or annotations on Nodes

When using `capsule` together with [capsule-proxy](https://github.com/clastix/capsule-proxy), Bill can allow Tenant Owners to [modify Nodes](../../proxy/overview.md).

By default, it will allow tenant owners to add and modify any label or annotation on their nodes. 

But there are some scenarios, when tenant owners should not have an ability to add or modify specific labels or annotations (there are some types of labels or annotations, which must be protected from modifications - for example, which are set by `cloud-providers` or `autoscalers`).

Bill, the cluster admin, can deny Tenant Owners to add or modify specific labels and annotations on Nodes:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1alpha1
kind: CapsuleConfiguration
metadata:
  name: default
  annotations:
    capsule.clastix.io/forbidden-node-labels: foo.acme.net,bar.acme.net
    capsule.clastix.io/forbidden-node-labels-regexp: .*.acme.net
    capsule.clastix.io/forbidden-node-annotations: foo.acme.net,bar.acme.net
    capsule.clastix.io/forbidden-node-annotations-regexp: .*.acme.net
spec:
  userGroups:
    - capsule.clastix.io
    - system:serviceaccounts:default
EOF
```

> **Important note**
>
>Due to [CVE-2021-25735](https://github.com/kubernetes/kubernetes/issues/100096) this feature is only supported for Kubernetes version older than:
>* v1.18.18
>* v1.19.10
>* v1.20.6
>* v1.21.0

# What’s next

This ends our tour in Capsule use cases. As we improve Capsule, more  use cases about multi-tenancy, policy admission control, and cluster  governance will be covered in the future.

Stay tuned!