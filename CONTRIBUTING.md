# Contributing

All contributions are welcome! If you find a bug or have a feature request, please open an issue or submit a pull request.

## Ways to contribute

### 1. Report Issues

Issues to Capsule help improve the project in multiple ways including the following:

  * Report potential bugs
  * Request a feature
  * Request a sample policy

### 2. Engagement
Engage with the community on [Slack](https://kubernetes.slack.com/archives/C03GETTJQRL) and help new users with questions or issues they may have.

### 3. Submit changes
Submit technical changes via pull requests. New contributors may easily view all open issues labeled as [good first issues](https://github.com/projectcapsule/capsule/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22) allowing you to get started in an approachable manner.

Once you wish to get started contributing to the code base, please refer to our [development guide](DEVELOPMENT.md) for a how-to. **[We accept pull requests from forks only](#create-a-pull-request)**.

Before creating a pull request, please ensure that your changes are tested and that the documentation is updated accordingly.

When creating a pull request, please visit:

  * [commits](#commits)

## Guidelines

The following guidelines outline the semantics and processes which apply to technical contributions to the project.

## Supported Versions
Versions follow [Semantic Versioning](https://semver.org/) terminology and are expressed as `x.y.z`:

- where x is the major version
- y is the minor version
- and z is the patch version

Security fixes, may be backported to the three most recent minor releases, depending on severity and feasibility.

Prereleases are marked as `-rc.x` (release candidate) and may refere to any type of version bump.

## Pull Requests

The pull request title is checked according to the described [semantics](#semantics) (pull requests don't require a scope). However pull requests are currently not used to generate the changelog. Check if your pull requests body meets the following criteria:

- reference a previously opened issue: https://docs.github.com/en/github/writing-on-github/autolinked-references-and-urls#issues-and-pull-requests 
- splitting changes into several and documented small commits
- limit the git subject to 50 characters and write as the continuation of the
  sentence "If applied, this commit will ..."
- explain what and why in the body, if more than a trivial change, wrapping at
  72 characters

If your pull request in a draft state and not ready yet for review, you can prefix the title with `[WIP]`. This will indicate that work is still ongoing:

    [WIP] feat(controller): new cool feature

### Create a Pull Request

Head over to the project repository on GitHub and click the **"Fork"** button. With the forked copy, you can try new ideas and implement changes to the project.

1. **Clone the repository to your device:**

Get the link of your forked repository, paste it in your device terminal and clone it using the command.

```sh
git clone https://hostname/YOUR-USERNAME/YOUR-REPOSITORY
```

2. **Create a branch:**

Create a new brach and navigate to the branch using this command.

```sh
git checkout -b <new-branch>
```

3. **Stage, Commit, and Push changes:**

Now that we have implemented the required changes, use the command below to stage the changes and commit them.

```sh
git add .
```

```sh
git commit -s -m "Commit message"
```

Go ahead and push your changes to GitHub using this command.

```sh
git push
```

## Commits

The commit message is checked according to the described [semantics](#semantics). Commits are used to generate the changelog and their author will be referenced in the changelog.

### Reorganising commits

To reorganise your commits, do the following (or use your way of doing it):


1. Pull upstream changes
   
```bash
git remote add upstream git@github.com:projectcapsule/capsule.git
git pull upstream main
```

2. Pick the current upstream HEAD (the commit is marked with `(remote/main, main)`)

```bash
git log
....
commit 10bbf39ac1ac3ad4f8485422e54faa9aadf03315 (remote/main, main)
Author: Oliver Bähler <oliverbaehler@hotmail.com>
Date:   Mon Oct 23 10:24:44 2023 +0200

    docs(repo): add sbom reference

    Signed-off-by: Oliver Bähler <oliverbaehler@hotmail.com>
```

3. Soft reset to the commit of the upstream HEAD

```bash
git reset --soft 10bbf39ac1ac3ad4f8485422e54faa9aadf03315
```

4. Remove staged files (if any)

```bash
git restore --staged .
```

5. Add files manually and create new [commits](#commits), until all files are included

```bash
git add charts/capsule/
git commit -s -m "feat(chart): add nodeselector value"

...
```

6. Force push the changes to your fork

```bash
git push origin main -f
```

### Sign-Off

Developer Certificate of Origin (DCO) Sign off
For contributors to certify that they wrote or otherwise have the right to submit the code they are contributing to the project, we are requiring everyone to acknowledge this by signing their work which indicates you agree to the DCO found here.

To sign your work, just add a line like this at the end of your commit message:

Signed-off-by: Random J Developer <random@developer.example.org>
This can easily be done with the -s command line option to append this automatically to your commit message.

    git commit -s -m 'This is my commit message'

## Semantics

The semantics should indicate the change and it's impact. The general format for commit messages and pull requests is the following:

    feat(ui): Add `Button` component
    ^    ^    ^
    |    |    |__ Subject
    |    |_______ Scope
    |____________ Type

 The commits are checked on pull-request. If the commit message does not follow the format, the workflow will fail. See the [Types](#types) for the supported types. The scope is not required but helps to provide more context for your changes. Try to use a scope if possible.

### Types

The following types are allowed for commits and pull requests:

  * `chore`: housekeeping changes, no production code change
  * `ci`: changes to buillding process/workflows
  * `docs`: changes to documentation
  * `feat`: new features
  * `fix`: bug fixes
  * `test`: test related changes
  * `sec`: security related changes

