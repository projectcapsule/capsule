# Configure OIDC authentication with Keycloak

## Pre-requisites

- Keycloak realm for Rancher
- Rancher OIDC authentication provider

## Keycloak realm for Rancher

These instructions is specific to a setup made with Keycloak as an OIDC identity provider.

### Mappers

- Add to userinfo Group Membership type, claim name `groups`
- Add to userinfo Audience type, claim name `client audience`
- Add to userinfo, full group path, Group Membership type, claim name `full_group_path`

More on this on the [official guide](https://capsule.clastix.io/docs/guides/oidc-auth/#configuring-oidc-server).

## Rancher OIDC authentication provider

Configure an OIDC authentication provider, with Client with issuer, return URLs specific to the Keycloak setup.

> Use old and Rancher-standard paths with `/auth` subpath (see issues below).
>
> Add custom paths, remove `/auth` subpath in return and issuer URLs.

## Configuration

### Configure Tenant users

1. In Rancher, configure OIDC authentication with Keycloak to use [with Rancher](https://ranchermanager.docs.rancher.com/how-to-guides/new-user-guides/authentication-permissions-and-global-configuration/authentication-config/configure-keycloak-oidc).
1. In Keycloak, Create a Group in the rancher Realm: *capsule.clastix.io*.
1. In Keycloak, Create a User in the rancher Realm, member of *capsule.clastix.io* Group.
1. In the Kubernetes target cluster, update the `CapsuleConfiguration` by adding the `"keycloakoidc_group://capsule.clastix.io"` Kubernetes `Group`.
1. Login to Rancher with Keycloak with the new user.
1. In Rancher as an administrator, set the user  custom role with `get` of Cluster.
1. In Rancher as an administrator, add the Rancher user ID of the just-logged in user as Owner of a `Tenant`.
1. (optional) configure `proxySettings` for the `Tenant` to enable tenant users to access cluster-wide resources.

