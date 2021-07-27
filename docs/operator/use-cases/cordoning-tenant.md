# Cordoning a Tenant

Bill needs to cordon a Tenant and its Namespaces for several reasons:

- Avoid accidental resource modification(s) including deletion during a Production Freeze Window
- During Kubernetes upgrade, to prevent any workload updates
- During incidents or outages
- During planned maintenance of a dedicated nodes pool in a BYOD scenario

With this said, the Tenant Owner and the related Service Account living into managed Namespaces, cannot proceed to any update, create or delete action.

This is possible just labelling the Tenant as follows:

```shell
kubectl label tenant oil capsule.clastix.io/cordon=enabled
tenant oil labeled
```

Any operation performed by Alice, the Tenant Owner, will be rejected.

```shell
$ kubectl --as alice --as-group capsule.clastix.io -n oil-dev create deployment nginx --image nginx
error: failed to create deployment: admission webhook "cordoning.tenant.capsule.clastix.io" denied the request: tenant oil is freezed: please, reach out to the system administrator

$ kubectl --as alice --as-group capsule.clastix.io -n oil-dev delete ingress,deployment,serviceaccount --all
error: failed to create deployment: admission webhook "cordoning.tenant.capsule.clastix.io" denied the request: tenant oil is freezed: please, reach out to the system administrator
```

Uncordoning can be done removing the said label:

```shell
$ kubectl label tenant oil capsule.clastix.io/cordon-
tenant.capsule.clastix.io/oil labeled

$ kubectl --as alice --as-group capsule.clastix.io -n oil-dev create deployment nginx --image nginx
deployment.apps/nginx created
```

Status of cordoning is also reported in the `state` of the tenant:

```shell
kubectl get tenants
NAME     STATE    NAMESPACE QUOTA   NAMESPACE COUNT   NODE SELECTOR    AGE
bronze   Active                     2                                  3d13h
gold     Active                     2                                  3d13h
oil      Cordoned                   4                                  2d11h
silver   Active                     2                                  3d13h
```

# What’s next

See how Bill, the cluster admin, can prevent creating services with specific service types. [Disabling Service Types](./service-type.md).