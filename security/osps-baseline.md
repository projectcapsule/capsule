# OSPS Baseline Level 1 evidence

Evidence assembled on 2026-10-17 for Capsule's Security Slam Defender submission.

## Scope and maturity

Capsule is a [CNCF Sandbox project](https://www.cncf.io/projects/capsule/).
The [Fall 2026 Defender criteria](https://securityslam.com/library/defender)
evaluate CNCF Sandbox projects at OSPS Baseline Level 1. This submission uses
the `osps-baseline-2026-08` catalog, version `2026.08.28`.

The scope is the [Capsule repository](https://github.com/projectcapsule/capsule),
which contains the controller, namespace profiling rules, admission webhooks,
and Helm chart. Capsule Proxy is developed and released separately.

## Published evaluation

- [Live target and result history](https://grc.store/targets/capsule/capsule).
- [Evaluation published on 2026-10-17](https://grc.store/capsule/capsule-osps-baseline-2026-08/versions/631ed367c96cf838f7fddefa31be27d70f0a8646-20261017T081858Z).
- [Producing GitHub Actions run](https://github.com/projectcapsule/capsule/actions/runs/39016867920).
- [Evaluated source revision](https://github.com/projectcapsule/capsule/tree/631ed367c96cf838f7fddefa31be27d70f0a8646).

The result is **Passed**: all **24 active Level 1 assessment requirements passed**,
with no failed or needs-review requirements at that level. The evaluator was
`openssf/github-repo` version `0.32.2`, at index digest
`sha256:1db2ff826a2f99dddffb8980f5f2111ad3a233e13ecc13417e6922ff0b47fa48`.
The result records the trusted publisher
`revanite-io/pvtr-publish-results/.github/workflows/publish.yml@refs/tags/v1`.

The catalog also retains the retired Level 1 identifier `OSPS-BR-01.02`.
Its assessment log explicitly identifies it as retired and records `Not Run`.
The active replacement, `OSPS-BR-01.03`, passed. Requirements exclusive to
Levels 2 and 3 are outside this submission and remain `Not Run` in this scan.
Control counts on the target page group multiple assessment requirements;
the table below lists the individual requirement identifiers.

The [publishing workflow](../.github/workflows/osps-baseline-publish.yml) runs
daily at 10:00 Europe/Zurich and supports manual runs. Normal runs publish both
passing and failing evaluations, so the live target remains the source for the
current verdict. The README badge links to that target rather than embedding a
fixed passing status.

## Level 1 requirement evidence

Each active requirement below has a `Passed` assessment log in the dated
evaluation above. Repository settings in that result were observed by the
scanner at the evaluated revision; policy and implementation links provide
context for reviewers.

| Requirement | Evidence and practice |
| --- | --- |
| OSPS-AC-01.01 | The published evaluation reports GitHub-enforced multifactor authentication. Project roles and access responsibilities are documented in [governance](../GOVERNANCE.md#maintainers). |
| OSPS-AC-02.01 | The evaluation reports GitHub-enforced explicit collaborator permission assignment. [Governance](../GOVERNANCE.md) identifies project roles and maintainers. |
| OSPS-AC-03.01 | The scanner observed a branch protection rule requiring approving reviews. The [development pipeline](../SELF_ASSESSMENT.md#development-pipeline) documents maintainer review and pull requests. |
| OSPS-AC-03.02 | The scanner observed a ruleset protecting the default branch against deletion. See the dated evaluation for the setting observed. |
| OSPS-BR-01.01 | The scanner found no untrusted input interpolation in GitHub workflow variables. [Workflow review](#build-workflow-review) records the relevant trust boundaries. |
| OSPS-BR-01.03 | The scanner found no workflows running untrusted code in a privileged context. [Workflow review](#build-workflow-review) documents triggers, runner selection, and credential handling. |
| OSPS-BR-03.01 | The [Security Insights](../security-insights.yml) project, documentation, repository, and policy links use HTTPS; the scanner verified the declared links. |
| OSPS-BR-03.02 | [Release distribution points](../security-insights.yml) use HTTPS for GitHub Releases, GHCR, and the Helm repository. The scanner verified those distribution links. |
| OSPS-BR-07.01 | The scanner observed both GitHub secret scanning and push protection enabled for the repository. See the dated evaluation for the settings observed. |
| OSPS-DO-01.01 | The [user guide](https://projectcapsule.dev/docs/) and [quickstart](https://projectcapsule.dev/docs/quickstart/) are recorded under `project.documentation` in [Security Insights](../security-insights.yml). |
| OSPS-DO-02.01 | [SECURITY.md](../SECURITY.md#reporting-a-vulnerability) explains private vulnerability reporting. [Security Insights](../security-insights.yml) records acceptance, the policy, and the maintainer contact. |
| OSPS-GV-02.01 | [Issues](https://github.com/projectcapsule/capsule/issues) and [discussions](https://github.com/projectcapsule/capsule/discussions) provide public ways to communicate with the project; both were enabled in the evaluation. |
| OSPS-GV-03.01 | [CONTRIBUTING.md](../CONTRIBUTING.md) explains how to contribute and how changes are reviewed; the [code of conduct](../CODE_OF_CONDUCT.md) is also linked from Security Insights. |
| OSPS-LE-02.01 | The scanner found approved licenses. Capsule's [LICENSE](../LICENSE) and the license expression in [Security Insights](../security-insights.yml) identify Apache-2.0. |
| OSPS-LE-02.02 | The scanner found approved licenses. The [dependency policy](../DEPENDENCY.md) requires review and version pinning when adopting third-party packages. |
| OSPS-LE-03.01 | [LICENSE](../LICENSE) is present at the repository root and was identified by GitHub's license API in the evaluation. |
| OSPS-LE-03.02 | The evaluation confirmed Apache-2.0 at `LICENSE` in released source tag [v0.14.6](https://github.com/projectcapsule/capsule/blob/v0.14.6/LICENSE), including the automatically generated source archives. |
| OSPS-QA-01.01 | [The repository](https://github.com/projectcapsule/capsule) is public and its source is available to readers without repository membership. |
| OSPS-QA-01.02 | The evaluation reports GitHub's source version-control facilities satisfy this requirement. Changes and revisions are accessible in the [commit history](https://github.com/projectcapsule/capsule/commits/main/). |
| OSPS-QA-02.01 | The scanner found 18 dependency manifests. Examples include [go.mod](../go.mod), [go.sum](../go.sum), and the [Helm chart](../charts/capsule/Chart.yaml); the [dependency policy](../DEPENDENCY.md) explains package selection and pinning. |
| OSPS-QA-04.01 | The [repository inventory](../README.md#project-repositories) and [Security Insights](../security-insights.yml) identify the controller, proxy, and documentation repositories and their scopes. |
| OSPS-QA-05.01 | The scanner found no common binary file extensions in the repository. The [release process](../DEVELOPMENT.md#release-process) describes the generated controller binaries and OCI artifacts distributed separately. |
| OSPS-QA-05.02 | The scanner found no unreviewable binary artifacts. [Build instructions](../DEVELOPMENT.md#building) and [release workflows](../DEVELOPMENT.md#release-process) show how the controller artifacts are produced from source. |
| OSPS-VM-02.01 | [SECURITY.md](../SECURITY.md#reporting-a-vulnerability) provides the reporting process; [Security Insights](../security-insights.yml) identifies the maintainer contact and private reporting URL. |

## Build-workflow review

The review for `OSPS-BR-01.01` and `OSPS-BR-01.03` covers the workflow definitions
at source revision
[`9b658a9ad17624eb615d963a248406611e9b4e37`](https://github.com/projectcapsule/capsule/tree/9b658a9ad17624eb615d963a248406611e9b4e37/.github/workflows),
in addition to the scanner's observations at its evaluated revision.

- No workflow uses `pull_request_target`, `workflow_run`, or a self-hosted runner.
  Pull-request checks use `pull_request`; release publishing uses tag pushes.
- [PR title validation](../.github/workflows/check-pr.yml) reads metadata with
  `pull-requests: read`, uses a SHA-pinned action, and does not check out PR code,
  restore caches, download artifacts, or build contributions.
- Checkout steps set `persist-credentials: false`. Fork pull-request runs use
  GitHub's restricted token permissions and do not receive repository secrets.
- The [baseline publisher](../.github/workflows/osps-baseline-publish.yml) is
  restricted to `refs/heads/main`; its scanner PAT is passed through the reusable
  workflow's config secret. The trusted reusable workflow verifies the plugin
  before execution, runs the plugin without an OIDC token, and publishes on a
  separate runner. Its `@v1` reference is required by the hub's signer identity.

Review these boundaries again when workflow triggers, permissions, runner types,
checkout refs, caches, artifact consumers, or executable build steps change.

## Submission

Submit [the public target page](https://grc.store/targets/capsule/capsule) for
Defender after the evidence links in this change are merged. Reviewers can follow
the target's latest result and the links in [Security Insights](../security-insights.yml).
This document records the evidence for the submission; badge approval is recorded
by Security Slam.
