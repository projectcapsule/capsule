#!/usr/bin/env python3
# Copyright 2020-2026 Project Capsule Authors
# SPDX-License-Identifier: Apache-2.0
"""Install the test collector using credentials supplied through the environment."""

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import time
from urllib.parse import urlencode
import uuid


NAMESPACE = 'capsule-observability'
RELEASE = 'capsule-alloy'
CHART_VERSION = '1.12.1'
DIRECTORY = Path(__file__).resolve().parent


def command(args, *, manifest=None, sensitive=False):
    result = subprocess.run(
        args,
        input=json.dumps(manifest) if manifest is not None else None,
        text=True,
        capture_output=True,
        timeout=360,
        check=False,
    )
    if result.returncode:
        # kubectl error messages can contain a rejected manifest. Never echo the
        # Secret request body (including on an API/RBAC/validation failure).
        detail = 'credential provisioning failed' if sensitive else result.stderr.strip()
        raise RuntimeError(f"{args[0]} failed: {detail}")
    return result.stdout


def apply(manifest, *, sensitive=False):
    return command(
        ['kubectl', 'apply', '--server-side', '--field-manager=capsule-observability', '-f', '-'],
        manifest=manifest,
        sensitive=sensitive,
    )


def output(key, value):
    path = os.environ.get('GITHUB_OUTPUT')
    if path:
        with open(path, 'a', encoding='utf-8') as stream:
            stream.write(f"{key}={value}\n")


def run_metadata(env):
    run_id = env.get('OBSERVABILITY_RUN_ID') or f"local-{uuid.uuid4()}"
    if len(run_id) > 200 or any(c.isspace() for c in run_id):
        raise ValueError('OBSERVABILITY_RUN_ID must be at most 200 characters, without whitespace')
    return {
        'RUN_ID': run_id,
        'REPOSITORY': env.get('OBSERVABILITY_REPOSITORY') or 'projectcapsule/capsule',
        'REVISION': env.get('OBSERVABILITY_REVISION') or 'local',
        'RUN_URL': env.get('OBSERVABILITY_RUN_URL') or '',
    }


def install(*, openshift=False):
    output('enabled', 'false')
    # Remove credentials from child-process environments. Only the Secret request
    # sent over kubectl's stdin contains them, never Helm arguments or values.
    username = os.environ.pop('MONITORING_USERNAME', '')
    password = os.environ.pop('MONITORING_PASSWORD', '')
    if not username and not password:
        print('Observability disabled: monitoring credentials are unavailable.')
        return
    if not username or not password:
        raise ValueError('Set both MONITORING_USERNAME and MONITORING_PASSWORD')
    metadata = run_metadata(os.environ)
    apply({
        'apiVersion': 'v1', 'kind': 'Namespace',
        'metadata': {'name': NAMESPACE, 'labels': {
            'pod-security.kubernetes.io/enforce': 'restricted',
            'pod-security.kubernetes.io/audit': 'restricted',
            'pod-security.kubernetes.io/warn': 'restricted',
        }},
    })
    output('started', 'true')
    apply({
        'apiVersion': 'v1', 'kind': 'Secret', 'type': 'Opaque',
        'metadata': {'name': 'monitoring-credentials', 'namespace': NAMESPACE},
        'stringData': {'username': username, 'password': password},
    }, sensitive=True)
    apply({
        'apiVersion': 'v1', 'kind': 'ConfigMap',
        'metadata': {'name': 'observability-run', 'namespace': NAMESPACE},
        'data': metadata,
    })
    args = [
        'helm', 'upgrade', '--install', RELEASE, 'alloy',
        '--repo', 'https://grafana.github.io/helm-charts',
        '--version', CHART_VERSION, '--namespace', NAMESPACE,
        '--values', str(DIRECTORY / 'values.yaml'),
        '--set-file', f"alloy.configMap.content={DIRECTORY / 'config.alloy'}",
        '--wait', '--timeout', '3m',
    ]
    if openshift:
        args += ['--values', str(DIRECTORY / 'openshift-values.yaml')]
    print(command(args), end='')
    # A new run gets a fresh process/environment even if Helm values did not
    # change. This avoids retaining the previous ConfigMap's run_id on a rerun.
    command(['kubectl', '-n', NAMESPACE, 'rollout', 'restart', f"deployment/{RELEASE}"])
    command(['kubectl', '-n', NAMESPACE, 'rollout', 'status', f"deployment/{RELEASE}", '--timeout=180s'])
    output('enabled', 'true')
    query = urlencode({'var-run_id': metadata['RUN_ID'], 'from': int(time.time() * 1000) - 60000, 'to': 'now'})
    url = f"https://monitoring.dev.projectcapsule.dev/d/capsule-runs/capsule-runs?{query}"
    print(f"Observability run: {metadata['RUN_ID']}\nGrafana: {url}")
    summary = os.environ.get('GITHUB_STEP_SUMMARY')
    if summary:
        with open(summary, 'a', encoding='utf-8') as stream:
            stream.write(f"\nCapsule observability: [open this run in Grafana]({url}).\n")


def stop():
    # Kubernetes sends SIGTERM and waits for Alloy to shut down before Helm
    # returns. Keep the cluster alive for the collector's 120s grace period.
    print(command([
        'helm', 'uninstall', RELEASE, '--namespace', NAMESPACE,
        '--ignore-not-found', '--wait', '--timeout', '3m',
    ]), end='')
    command([
        'kubectl', '-n', NAMESPACE, 'delete', 'secret', 'monitoring-credentials',
        '--ignore-not-found',
    ])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('install', 'stop'))
    parser.add_argument('--openshift', action='store_true')
    args = parser.parse_args()
    try:
        if args.action == 'install':
            install(openshift=args.openshift)
        else:
            stop()
    except (RuntimeError, ValueError, OSError, subprocess.TimeoutExpired) as error:
        print(f"Observability: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
