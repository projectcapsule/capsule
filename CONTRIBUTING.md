# Contributing

All contributions are welcome! If you find a bug or have a feature request, please open an issue or submit a pull request.


## Guidelines


## Pull Requests


## Commits

Commit messages should indicate the change and it's impact. The general format for commit messages is the following:

    feat(ui): Add `Button` component
    ^    ^    ^
    |    |    |__ Subject
    |    |_______ Scope
    |____________ Type

 The commits are checked on pull-request. If the commit message does not follow the format, the workflow will fail. See the [Types](#types) and [Scopes](#scopes) sections for more information.

## Types

The following types are allowed for commits and pull requests:

  * `ci` or `build`: changes to buillding process/workflows
  * `docs`: changes to documentation
  * `feat`: new features
  * `fix`: bug fixes

## Scopes

The following types are allowed for commits and pull requests:

  * `all`: changes that affect all components
  * `chart`: changes to the Helm chart
  * `operator`: changes to the operator
  * `docs`: changes to the documentation
  * `website`: changes to the website
  * `ci`: changes to the CI/CD workflows
  * `build`: changes to the build process
  * `test`: changes to the testing process
  * `release`: changes to the release process
  * `deps`: dependency updates

### Sign-Off

Developer Certificate of Origin (DCO) Sign off
For contributors to certify that they wrote or otherwise have the right to submit the code they are contributing to the project, we are requiring everyone to acknowledge this by signing their work which indicates you agree to the DCO found here.

To sign your work, just add a line like this at the end of your commit message:

Signed-off-by: Random J Developer <random@developer.example.org>
This can easily be done with the -s command line option to append this automatically to your commit message.

git commit -s -m 'This is my commit message'
