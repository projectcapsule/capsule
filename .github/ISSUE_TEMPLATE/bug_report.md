---
name: Bug report
about: Create a report to help us improve Capsule
title: ''
labels: blocked-needs-validation, bug
assignees: ''

---

<!--
Thanks for taking time reporting a Capsule bug!

We do our best to keep it reliable and working, so don't hesitate adding
as many information as you can and keep in mind you can reach us on our
Clastix Slack workspace: https://clastix.slack.com, #capsule channel.   
-->

# Bug description

A clear and concise description of what the bug is.

# How to reproduce

Steps to reproduce the behavior:

1. Provide the Capsule Tenant YAML definitions
2. Provide all managed Kubernetes resources

# Expected behavior

A clear and concise description of what you expected to happen.

# Logs

If applicable, please provide logs of `capsule`.

In a standard stand-alone installation of Capsule,
you'd get this by running `kubectl -n capsule-system logs deploy/capsule-controller-manager`.

# Additional context

- Capsule version: (`capsule --version`)
- Helm Chart version: (`helm list -n capsule-system`)
- Kubernetes version: (`kubectl version`)
