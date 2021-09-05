# Tenant Ownership

## OIDC Prefix 

The Kube API Server might have oidc prefix flags set. Make sure include those prefixes when declaring tenant owners. The prefixes are always appended for each group or user (depending on the configuration). So even if your token originally looks like this coming from the IDP:

``` 
"users_groups": [
  "capsule.clastix.io"
]
```

Assuming you have these flags set on your Kube API Server:

```
...
    - --oidc-username-prefix=idp_
    - --oidc-groups-prefix=idp_
```

You need to prepend the prefix in your tenant configuration:

```
apiVersion: capsule.clastix.io/v1alpha1
kind: CapsuleConfiguration
metadata:
  name: default
spec:
  userGroups:
    - idp_capsule.clastix.io
```

Same for tenant Owners:

```
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
    - kind: Group
      name: idp_kubernetes/applications
    - kind: User
      name: idp_Bob
```

[Read More](https://kubernetes.io/docs/reference/access-authn-authz/authentication/)


## ServiceAccount 

You can delegate the ownership of a tenant to a ServiceAccount. Make sure to correctly reference the serviceAccount according to the pattern `{ServiceAccountName}:{ServiceAccountNamespace}`. Make sure to include the `:` which splits the the name of the serviceAccount with it's namespace:

``` 
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
    - kind: ServiceAccount
      name: system:serviceaccount:default
```