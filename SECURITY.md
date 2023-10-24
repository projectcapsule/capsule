# Security Policy

The Capsule community has adopted this security disclosures and response policy to ensure we responsibly handle critical issues.

## Bulletins

For information regarding the security of this project please join our [slack channel](https://kubernetes.slack.com/archives/C03GETTJQRL).


## Covered Repositories and Issues

When we say "a security vulnerability in capsule" we mean a security issue
in any repository under the [projectcapsule GitHub organization](https://github.com/projectcapsule/).

This reporting process is intended only for security issues in the capsule
project itself, and doesn't apply to applications _using_ capsule or to
issues which do not affect security.

Don't use this process if:

  * You have issues with your capsule installation or configuration
  * Your issue is not security related


### Explicitly Not Covered: Vulnerability Scanner Reports

We do not accept reports which amount to copy and pasted output from a vulnerability
scanning tool **unless** work has specifically been done to confirm that a vulnerability
reported by the tool _actually exists_ in capsule.

## Reporting a Vulnerability

To report a security issue or vulnerability, [submit a private vulnerability report via GitHub](https://github.com/projectcapsule/capsule/security/advisories/new) to the repository maintainers with a description of the issue, the steps you took to create the issue, affected versions, and, if known, mitigations for the issue.

Describe the issue in English, ideally with some example configuration or code which allows the issue to be reproduced. Explain why you believe this to be a security issue in capsule, if that's not obvious. should contain the following:

     * description of the problem
     * precise and detailed steps (include screenshots) 
     * the affected version(s). This may also include environment relevant versions.
     * any possible mitigations

If the issue is confirmed as a vulnerability, we will open a Security Advisory and acknowledge your contributions as part of it.

## Reponse

Response times could be affected by weekends, holidays, breaks or time zone differences. That said, the security response team will endeavour to reply as soon as possible, ideally within 5 working days.

## Security Contacts

[Maintainers](./github/maintainers.yaml) of this project are responsible for the security of the project as outlined in this policy.

# Release Artifacts

[See all the available artifacts](https://github.com/orgs/projectcapsule/packages?repo_name=capsule)

## Verifing

To verify artifacts you need to have [cosign installed](https://github.com/sigstore/cosign#installation). This guide assumes you are using v2.x of cosign. All of the signatures are created using [keyless signing](https://docs.sigstore.dev/verifying/verify/#keyless-verification-using-openid-connect). We have a seperate repository for all the signatures for all the artifacts released under the projectcapsule - `ghcr.io/projectcapsule/signatures`. You can set the environment variable `COSIGN_REPOSITORY` to point to this repository. For example:

    export COSIGN_REPOSITORY=ghcr.io/projectcapsule/signatures

To verify the signature of the docker image, run the following command. Replace `<release_tag>` with an [available release tag](https://github.com/projectcapsule/capsule/pkgs/container/capsule):

    COSIGN_REPOSITORY=ghcr.io/projectcapsule/signatures cosign verify ghcr.io/projectcapsule/capsule:<release_tag> \
      --certificate-identity-regexp="https://github.com/projectcapsule/capsule/.github/workflows/docker-publish.yml@refs/tags/*" \
      --certificate-oidc-issuer="https://token.actions.githubusercontent.com" | jq

To verify the signature of the helm image, run the following command. Replace `<release_tag>` with an [available release tag](https://github.com/projectcapsule/capsule/pkgs/container/charts%2Fcapsule):

    COSIGN_REPOSITORY=ghcr.io/projectcapsule/signatures cosign verify ghcr.io/projectcapsule/charts/capsule:<release_tag> \
      --certificate-identity-regexp="https://github.com/projectcapsule/capsule/.github/workflows/helm-publish.yml@refs/tags/*" \
      --certificate-oidc-issuer="https://token.actions.githubusercontent.com" | jq


## Verifying Provenance

Capsule creates and attests to the provenance of its builds using the [SLSA standard](https://slsa.dev/spec/v0.2/provenance) and meets the [SLSA Level 3](https://slsa.dev/spec/v0.1/levels) specification. The attested provenance may be verified using the cosign tool.

Verify the provenance of the docker image. Replace `<release_tag>` with an [available release tag](https://github.com/projectcapsule/capsule/pkgs/container/capsule)

```bash
cosign verify-attestation --type slsaprovenance \
  --certificate-identity-regexp="https://github.com/slsa-framework/slsa-github-generator/.github/workflows/generator_container_slsa3.yml@refs/tags/*" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  ghcr.io/projectcapsule/capsule:<release_tag> | jq .payload -r | base64 --decode | jq
```

Verify the provenance of the helm image. Replace `<release_tag>` with an [available release tag](https://github.com/projectcapsule/capsule/pkgs/container/charts%2Fcapsule)

```bash
cosign verify-attestation --type slsaprovenance \
  --certificate-identity-regexp="https://github.com/slsa-framework/slsa-github-generator/.github/workflows/generator_container_slsa3.yml@refs/tags/*" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  ghcr.io/projectcapsule/charts/capsule:<release_tag> | jq .payload -r | base64 --decode | jq
```

## Software Bill of Materials (SBOM)

An SBOM (Software Bill of Materials) in CycloneDX JSON format is published for each Kyverno release, including pre-releases. Like signatures, SBOMs are stored in a separate repository at `ghcr.io/projectcapsule/sbom`. You can set the environment variable `COSIGN_REPOSITORY` to point to this repository. For example:

    export COSIGN_REPOSITORY=ghcr.io/projectcapsule/sbom

To inspect the SBOM of the docker image, run the following command. Replace `<release_tag>` with an [available release tag](https://github.com/projectcapsule/capsule/pkgs/container/capsule):


    COSIGN_REPOSITORY=ghcr.io/projectcapsule/sbom cosign download sbom ghcr.io/projectcapsule/capsule:<release_tag>
    
To inspect the SBOM of the helm image, run the following command. Replace `<release_tag>` with an [available release tag](https://github.com/projectcapsule/capsule/pkgs/container/charts%2Fcapsule):

    COSIGN_REPOSITORY=ghcr.io/projectcapsule/sbom cosign download sbom ghcr.io/projectcapsule/charts/capsule:<release_tag>


# Credits

Our Security Policy and Workflows are based on the work of the [Kyverno](https://github.com/kyverno) and [Cert-Manager](https://github.com/cert-manager) community.
