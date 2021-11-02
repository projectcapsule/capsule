# Control hostname collision in ingresses
In a multi-tenant environment, as more and more ingresses are defined, there is a chance of collision on the hostname leading to unpredictable behavior of the Ingress Controller. Bill, the cluster admin, can enforce hostname collision detection at different scope levels:

1. Cluster
2. Tenant
3. Namespace
4. Disabled (default)

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  - name: joe
    kind: User
  ingressOptions:
    hostnameCollisionScope: Tenant
EOF
```

When a tenant owner creates an Ingress resource, Capsule will check the collision of hostname in the current ingress with all the hostnames already used, depending on the defined scope.

For example, Alice, one of the tenant owners, creates an Ingress


```yaml
kubectl -n oil-production apply -f - << EOF
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: nginx
  namespace: oil-production
spec:
  rules:
  - host: web.oil.acmecorp.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: nginx
            port:
              number: 80
EOF
```

Another user, Joe creates an Ingress having the same hostname

```yaml
kubectl -n oil-development apply -f - << EOF
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: nginx
  namespace: oil-development
spec:
  rules:
  - host: web.oil.acmecorp.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: nginx
            port:
              number: 80
EOF
```

When a collision is detected at scope defined by `spec.ingressOptions.hostnameCollisionScope`, the creation of the Ingress resource will be rejected by the Validation Webhook enforcing it. When `hostnameCollisionScope=Disabled`, no collision detection is made at all.

# What’s next
See how Bill, the cluster admin, can assign a Storage Class to Alice's tenant. [Assign Storage Classes](/docs/operator/use-cases/storage-classes).
