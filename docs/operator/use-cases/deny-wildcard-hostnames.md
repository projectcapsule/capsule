# Deny Wildcard Hostnames

Bill, the cluster admin, can deny the use of wildcard hostnames.

Let's assume that we had a big organization, having a domain `bigorg.com` and there are two tenants, `gas` and `oil`.

As a tenant-owner of `gas`, Alice create ingress with the host like `- host: "*.bigorg.com"`. That can lead to big problems for the `oil` tenant because Alice can deliberately create ingress with host: `oil.bigorg.com`.

To avoid this kind of problems, Bill can deny the use of wildcard hostnames in the following way:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: gas
  annotations:
    capsule.clastix.io/deny-wildcard: true
spec:
  owners:
  - name: alice
    kind: User
EOF
```

Doing this, Alice will not be able to use `oil.bigorg.com`, being the tenant-owner of `gas`.

# What’s next
See how Bill, the cluster admin can protect specific labels and annotations on Nodes from modifications by Tenant Owners. [Denying specific user-defined labels or annotations on Nodes](./node-labels-and-annotations.md).
