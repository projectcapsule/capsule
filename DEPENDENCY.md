# Environment Dependencies Policy

## Purpose

This policy describes how Capsule maintainers consume third-party packages.

## Scope

This policy applies to all Capsule maintainers and all third-party packages used in the Capsule project.

## Policy

Capsule maintainers must follow these guidelines when consuming third-party packages:

- Only use third-party packages that are necessary for the functionality of Capsule.
- Use the latest version of all third-party packages whenever possible.
- Avoid using third-party packages that are known to have security vulnerabilities.
- Pin all third-party packages to specific versions in the Capsule codebase.
- Use a dependency management tool, such as Go modules, to manage third-party dependencies.
- Dependencies must pass all automated tests before being merged into the Capsule codebase.

## Procedure

When adding a new third-party package to Capsule, maintainers must follow these steps:

1. Evaluate the need for the package. Is it necessary for the functionality of Capsule? 
2. Research the package. Is it well-maintained? Does it have a good reputation? 
3. Choose a version of the package. Use the latest version whenever possible. 
4. Pin the package to the specific version in the Capsule codebase. 
5. Update the Capsule documentation to reflect the new dependency.

## Archive/Deprecation

When a third-party package is discontinued, the Capsule maintainers must fensure to replace the package with a suitable alternative.

## Enforcement

This policy is enforced by the Capsule maintainers.
Maintainers are expected to review each other's code changes to ensure that they comply with this policy.

## Exceptions

Exceptions to this policy may be granted by the Capsule project lead on a case-by-case basis.

## Credits

This policy was adapted from the [Kubescape Community](https://github.com/kubescape/kubescape/blob/master/docs/environment-dependencies-policy.md)
