# Copyright 2020-2026 Project Capsule Authors
# SPDX-License-Identifier: Apache-2.0

import contextlib
import io
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import collector


class CollectorTest(unittest.TestCase):
    def setUp(self):
        self.environment = patch.dict(os.environ, {}, clear=True)
        self.environment.start()
        self.addCleanup(self.environment.stop)
        self.output = io.StringIO()
        self.redirect = contextlib.redirect_stdout(self.output)
        self.redirect.__enter__()
        self.addCleanup(self.redirect.__exit__, None, None, None)

    def credentials(self):
        os.environ.update(MONITORING_USERNAME='test-user', MONITORING_PASSWORD='test-secret')

    @patch.object(collector, 'command')
    def test_missing_credentials_skips_without_touching_cluster(self, command):
        collector.install()
        command.assert_not_called()
        self.assertIn('disabled', self.output.getvalue())

    @patch.object(collector, 'command')
    def test_partial_credentials_fail_before_touching_cluster(self, command):
        os.environ['MONITORING_USERNAME'] = 'test-user'
        with self.assertRaisesRegex(ValueError, 'both'):
            collector.install()
        command.assert_not_called()

    @patch.object(collector, 'command', return_value='')
    def test_secret_only_appears_in_sensitive_stdin_manifest(self, command):
        self.credentials()
        os.environ['OBSERVABILITY_RUN_ID'] = 'capsule-123-2-e2e-v1.35.0'
        collector.install()
        manifests = [c.kwargs['manifest'] for c in command.call_args_list if 'manifest' in c.kwargs]
        secret = next(m for m in manifests if m['kind'] == 'Secret')
        self.assertEqual(secret['stringData'], {'username': 'test-user', 'password': 'test-secret'})
        for call in command.call_args_list:
            self.assertNotIn('test-secret', str(call.args))
            if call.kwargs.get('manifest', {}).get('kind') == 'Secret':
                self.assertTrue(call.kwargs['sensitive'])
        self.assertNotIn('MONITORING_PASSWORD', os.environ)
        self.assertNotIn('MONITORING_USERNAME', os.environ)
        self.assertNotIn('test-secret', self.output.getvalue())
        config = next(m for m in manifests if m['kind'] == 'ConfigMap')
        self.assertEqual(config['data']['RUN_ID'], 'capsule-123-2-e2e-v1.35.0')

    @patch.object(collector, 'command', return_value='')
    def test_openshift_uses_scc_overlay(self, command):
        self.credentials()
        collector.install(openshift=True)
        helm = next(c.args[0] for c in command.call_args_list if c.args[0][0] == 'helm')
        self.assertIn(str(collector.DIRECTORY / 'openshift-values.yaml'), helm)

    @patch.object(collector, 'command', return_value='')
    def test_enabled_is_only_reported_after_successful_rollout(self, command):
        self.credentials()
        def fail_rollout(args, **kwargs):
            if 'status' in args:
                raise RuntimeError('rollout failed')
            return ''
        command.side_effect = fail_rollout
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'outputs'
            os.environ['GITHUB_OUTPUT'] = str(path)
            with self.assertRaisesRegex(RuntimeError, 'rollout failed'):
                collector.install()
            self.assertIn('started=true', path.read_text())
            self.assertNotIn('enabled=true', path.read_text())

    @patch.object(collector, 'command')
    def test_invalid_run_id_does_not_mutate_cluster(self, command):
        self.credentials()
        os.environ['OBSERVABILITY_RUN_ID'] = 'bad\nenabled=true'
        with self.assertRaises(ValueError):
            collector.install()
        command.assert_not_called()

    def test_local_run_ids_are_unique(self):
        self.assertNotEqual(collector.run_metadata({})['RUN_ID'], collector.run_metadata({})['RUN_ID'])

    @patch.object(collector.subprocess, 'run')
    def test_api_failure_does_not_echo_secret_body(self, run):
        run.return_value = subprocess.CompletedProcess([], 1, 'test-secret', 'rejected: test-secret')
        with self.assertRaisesRegex(RuntimeError, 'credential provisioning failed') as error:
            collector.apply({'stringData': {'password': 'test-secret'}}, sensitive=True)
        self.assertNotIn('test-secret', str(error.exception))

    @patch.object(collector, 'command', return_value='')
    def test_stop_waits_for_collector_before_removing_credentials(self, command):
        collector.stop()
        first, second = [c.args[0] for c in command.call_args_list]
        self.assertEqual(first[:2], ['helm', 'uninstall'])
        self.assertIn('--wait', first)
        self.assertIn('secret', second)

    @patch.object(collector, 'command', side_effect=RuntimeError('still draining'))
    def test_failed_shutdown_retains_credentials_for_retry(self, command):
        with self.assertRaises(RuntimeError):
            collector.stop()
        self.assertEqual(command.call_count, 1)


if __name__ == '__main__':
    unittest.main()
