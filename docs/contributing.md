# How to contribute to Capsule

First, thanks for your interest in Capsule, any contribution is welcome!

## Development environment setup

The first step is to set up your local development environment.

Please follow the [Capsule Development Guide](dev-guide.md) for details.

## Code convention

The changes must follow the Pull Request method where a _GitHub Action_ will
check the `golangci-lint`, so ensure your changes respect the coding standard.

### golint

You can easily check them issuing the _Make_ recipe `golint`.

```
# make golint
golangci-lint run -c .golangci.yml
```

> Enabled linters and related options are defined in the [.golanci.yml file](../../.golangci.yml)

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

### Commits

All the Pull Requests must refer to an already open issue: this is the first phase to contribute also for informing maintainers about the issue.

Commit's first line should not exceed 50 columns.

A commit description is welcomed to explain more the changes: just ensure
to put a blank line and an arbitrary number of maximum 72 characters long
lines, at most one blank line between them.

Please, split changes into several and documented small commits: this will help us to perform a better review. Commits must follow the Conventional Commits Specification, a lightweight convention on top of commit messages. It provides an easy set of rules for creating an explicit commit history; which makes it easier to write automated tools on top of. This convention dovetails with Semantic Versioning, by describing the features, fixes, and breaking changes made in commit messages. See [Conventional Commits Specification](https://www.conventionalcommits.org) to learn about Conventional Commits.

> In case of errors or need of changes to previous commits,
> fix them squashing to make changes atomic.

### Miscellanea

Please, add a new single line at end of any file as the current coding style.
