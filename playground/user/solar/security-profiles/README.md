# Security profile rules

Apply the updated platform Tenant manifests first. These examples are manual
so the namespace selection and the deliberately rejected Pod are explicit.

As the platform administrator, enable seccomp in the test namespace:

```sh
kubectl label namespace solar-test security-profile=confined --overwrite
```

Wait for the namespace's RuleStatus to include `seccompProfiles`, then create
`default.yaml` as the solar tenant owner. Its persisted Pod security context
contains `seccompProfile.type: RuntimeDefault`. A container inherits this default.
The scheduling gate keeps this admission example from starting on a node.

`override-denied.yaml` explicitly requests Unconfined at container level and
must be rejected. The playground's Pod Security baseline also prohibits
Unconfined; the Capsule unit test checks the profile-specific denial directly.

On an AppArmor-capable node pool, the platform administrator can additionally
label the namespace `apparmor=enabled`. New Pods then receive and enforce an
AppArmor RuntimeDefault profile. This setting does not install AppArmor or
load profiles on nodes. Recreate the example Pod to observe the new default.

Changing namespace labels or rules affects subsequent admissions, not the
security context of already running containers. Remove both labels to disable
these optional rules in this namespace.
