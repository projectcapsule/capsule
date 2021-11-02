# Assign Ingress Classes
An Ingress Controller is used in Kubernetes to publish services and applications outside of the cluster. An Ingress Controller can be provisioned to accept only Ingresses with a given Ingress Class.

Bill can assign a set of dedicated Ingress Classes to the `oil` tenant to force the applications in the `oil` tenant to be published only by the assigned Ingress Controller: 

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
    allowedClasses:
      allowed:
      - default
      allowedRegex: ^\w+-lb$
EOF
```

Capsule assures that all Ingresses created in the tenant can use only one of the valid Ingress Classes.

Alice can create an Ingress using only an allowed Ingress Class:

```yaml
kubectl -n oil-production apply -f - << EOF
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: nginx
  namespace: oil-production
  annotations:
    kubernetes.io/ingress.class: default
spec:
  rules:
  - host: oil.acmecorp.com
    http:
      paths:
      - backend:
          serviceName: nginx
          servicePort: 80
        path: /
EOF
```

Any attempt of Alice to use a non-valid Ingress Class, or missing it, is denied by the Validation Webhook enforcing it.

# What’s next
See how Bill, the cluster admin, can assign a set of dedicated ingress hostnames to Alice's tenant. [Assign Ingress Hostnames](/docs/operator/use-cases/ingress-hostnames).
