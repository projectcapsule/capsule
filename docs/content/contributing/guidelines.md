# Contributing Guidelines

Thank you for your interest in contributing to Capsule. Whether it's a bug report, new feature, correction, or additional documentation, we greatly value feedback and contributions from our community.

Please read through this document before submitting any issues or pull requests to ensure we have all the necessary information to effectively respond to your bug report or contribution.

## Pull Requests

Contributions via pull requests are much appreciated. Before sending us a pull request, please ensure that:

1. You are working against the latest source on the *master* branch.
1. You check existing open, and recently merged, pull requests to make sure someone else hasn't addressed the problem already.
1. You open an issue to discuss any significant work: we would hate for your time to be wasted.

To send us a pull request, please:

1. Fork the repository.
1. Modify the source; please focus on the specific change you are contributing. If you also reformat all the code, it
   will be hard for us to focus on your change.
1. Ensure local tests pass.
1. Commit to your fork using clear commit messages.
1. Send us a pull request, answering any default questions in the pull request interface.
1. Pay attention to any automated CI failures reported in the pull request, and stay involved in the conversation.

GitHub provides additional document on [forking a repository](https://help.github.com/articles/fork-a-repo/) and
[creating a pull request](https://help.github.com/articles/creating-a-pull-request/).

Make sure to keep Pull Requests small and functional to make them easier to review, understand, and look up in commit history. This repository uses "Squash and Commit" to keep our history clean and make it easier to revert changes based on PR.

Adding the appropriate documentation, unit tests and e2e tests as part of a feature is the responsibility of the
feature owner, whether it is done in the same Pull Request or not.

All the Pull Requests must refer to an already open issue: this is the first phase to contribute also for informing maintainers about the issue.

## Commits

Commit's first line should not exceed 50 columns.

A commit description is welcomed to explain more the changes: just ensure
to put a blank line and an arbitrary number of maximum 72 characters long
lines, at most one blank line between them.

Please, split changes into several and documented small commits: this will help us to perform a better review. Commits must follow the Conventional Commits Specification, a lightweight convention on top of commit messages. It provides an easy set of rules for creating an explicit commit history; which makes it easier to write automated tools on top of. This convention dovetails with Semantic Versioning, by describing the features, fixes, and breaking changes made in commit messages. See [Conventional Commits Specification](https://www.conventionalcommits.org) to learn about Conventional Commits.

> In case of errors or need of changes to previous commits,
> fix them squashing to make changes atomic.

## Code convention

Capsule is written in Golang. The changes must follow the Pull Request method where a _GitHub Action_ will
check the `golangci-lint`, so ensure your changes respect the coding standard.

### golint

You can easily check them issuing the _Make_ recipe `golint`.

```
# make golint
golangci-lint run -c .golangci.yml
```

> Enabled linters and related options are defined in the [.golanci.yml file](https://github.com/clastix/capsule/blob/master/.golangci.yml)

### goimports

Also, the Go import statements must be sorted following the best practice:

```
<STANDARD LIBRARY>

<EXTERNAL PACKAGES>

<LOCAL PACKAGES>
```

To help you out you can use the _Make_ recipe `goimports`

```
# make goimports
goimports -w -l -local "github.com/clastix/capsule" .
```

## Finding contributions to work on
Looking at the existing issues is a great way to find something to contribute on. As our projects, by default, use the
default GitHub issue labels (enhancement/bug/duplicate/help wanted/invalid/question/wontfix), looking at any 'help wanted'
and 'good first issue' issues are a great place to start.

## Design Docs

A contributor proposes a design with a PR on the repository to allow for revisions and discussions.
If a design needs to be discussed before formulating a document for it, make use of GitHub Discussions to
involve the community on the discussion.

## GitHub Issues

GitHub Issues are used to file bugs, work items, and feature requests with actionable items/issues.

When filing an issue, please check existing open, or recently closed, issues to make sure somebody else hasn't already reported the issue. Please try to include as much information as you can. Details like these are incredibly useful:

* A reproducible test case or series of steps
* The version of the code being used
* Any modifications you've made relevant to the bug
* Anything unusual about your environment or deployment

## Miscellanea

Please, add a new single line at end of any file as the current coding style.

## Licensing

See the [LICENSE](https://github.com/clastix/capsule/blob/master/LICENSE) file for our project's licensing. We can ask you to confirm the licensing of your contribution.
