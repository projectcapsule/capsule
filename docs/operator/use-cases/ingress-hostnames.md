# Assign Ingress Hostnames
Bill can control ingress hostnames to the `oil` tenant to force the applications to be published only using the given hostname or set of hostnames: 

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
  ingressOptions:
    allowedHostnames:
      allowed:
        - oil.acmecorp.com
      allowedRegex: ^.*acmecorp.com$
EOF
```

The Capsule controller assures that all Ingresses created in the tenant can use only one of the valid hostnames.

Alice can create an Ingress using any allowed hostname

```yaml
kubectl apply -f - << EOF
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: nginx
  namespace: oil-production
  annotations:
    kubernetes.io/ingress.class: oil
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

Any attempt of Alice to use a non-valid hostname is denied by the Validation Webhook enforcing it.

# What’s next
See how Bill, the cluster admin, can control the hostname collision in Ingresses. [Control hostname collision in ingresses](./hostname-collision.md).
