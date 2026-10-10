// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestCheckedInAssessment(t *testing.T) {
	require.NoError(t, validateDirectory("../.."))
}

func TestCatalogRelationships(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*catalog, *catalog)
		error  string
	}{
		{"valid", func(_, _ *catalog) {}, ""},
		{"wrong type", func(c, _ *catalog) { c.Metadata.Type = "ThreatCatalog" }, "requires a CapabilityCatalog"},
		{"same catalog IDs", func(c, r *catalog) { r.Metadata.ID = c.Metadata.ID }, "distinct"},
		{"schema drift", func(c, _ *catalog) { c.Metadata.GemaraVersion = "1.0.0" }, "pinned Gemara"},
		{"version mismatch", func(c, _ *catalog) { c.Metadata.Version = "2.0.0" }, "versions"},
		{"empty capabilities", func(c, _ *catalog) { c.Capabilities = nil }, "must define"},
		{"empty threats", func(_, r *catalog) { r.Threats = nil }, "must define"},
		{"duplicate capability", func(c, _ *catalog) { c.Capabilities = append(c.Capabilities, c.Capabilities[0]) }, "duplicate capability ID"},
		{"duplicate threat", func(_, r *catalog) { r.Threats = append(r.Threats, r.Threats[0]) }, "duplicate threat ID"},
		{"missing catalog reference", func(_, r *catalog) { r.Metadata.MappingReferences = nil }, "companion"},
		{"wrong catalog reference", func(_, r *catalog) { r.Metadata.MappingReferences[0].ID = "other" }, "mapping reference"},
		{"wrong catalog version", func(_, r *catalog) { r.Metadata.MappingReferences[0].Version = "2.0.0" }, "mapping reference"},
		{"wrong catalog URL", func(_, r *catalog) { r.Metadata.MappingReferences[0].URL = threatsURL }, "mapping reference"},
		{"unknown import", func(_, r *catalog) { r.Imports[0].Entries[0].ReferenceID = "missing" }, "unresolved capability"},
		{"unimported capability", func(_, r *catalog) { r.Imports[0].Entries = r.Imports[0].Entries[1:] }, "unresolved capability"},
		{"duplicate import", func(_, r *catalog) { r.Imports[0].Entries = append(r.Imports[0].Entries, r.Imports[0].Entries[0]) }, "duplicate capability mapping"},
		{"unmapped threat", func(_, r *catalog) { r.Threats[0].Capabilities = nil }, "must map"},
		{"wrong threat reference", func(_, r *catalog) { r.Threats[0].Capabilities[0].ReferenceID = "other" }, "must reference"},
		{"empty mapping", func(_, r *catalog) { r.Threats[0].Capabilities[0].Entries = nil }, "contain entries"},
		{"unknown threat capability", func(_, r *catalog) { r.Threats[0].Capabilities[0].Entries[0].ReferenceID = "missing" }, "unresolved capability"},
		{"unassessed capability", func(c, _ *catalog) { c.Capabilities = append(c.Capabilities, entry{ID: "unassessed"}) }, "no assessed threat"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var capabilities, threats catalog
			require.NoError(t, readYAML("../../security/capabilities.yaml", &capabilities))
			require.NoError(t, readYAML("../../security/threats.yaml", &threats))
			tc.change(&capabilities, &threats)
			before, err := yaml.Marshal([]catalog{capabilities, threats})
			require.NoError(t, err)
			err = validateCatalogs(capabilities, threats)
			if tc.error == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.error)
			}
			after, err := yaml.Marshal([]catalog{capabilities, threats})
			require.NoError(t, err)
			require.Equal(t, before, after, "validation must not mutate its inputs")
		})
	}
}

func TestReadYAML(t *testing.T) {
	for _, tc := range []struct {
		name, content, error string
	}{
		{"single document", "metadata:\n  id: test\n", ""},
		{"malformed", "metadata: [", "decode"},
		{"duplicate keys", "metadata:\n  id: first\n  id: second\n", "already defined"},
		{"multiple documents", "metadata: {}\n---\nmetadata: {}\n", "exactly one"},
		{"empty", "", "decode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "catalog.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.content), 0o600))
			var got catalog
			err := readYAML(path, &got)
			if tc.error == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.error)
			}
		})
	}
	var got catalog
	require.ErrorContains(t, readYAML(filepath.Join(t.TempDir(), "missing.yaml"), &got), "read")
}

func TestAssessmentEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*catalog, *securityInsights)
		error  string
	}{
		{"valid", func(_ *catalog, _ *securityInsights) {}, ""},
		{"schema drift", func(_ *catalog, s *securityInsights) {
			s.Header.SchemaVersion = "1.0.0"
		}, "pinned schema version"},
		{"wrong assessment link", func(_ *catalog, s *securityInsights) {
			s.Repository.Security.Assessments.Self.Evidence = capabilitiesURL
		}, "self assessment must reference"},
		{"no evidence", func(c *catalog, _ *securityInsights) {
			c.Capabilities[0].Description = "unsupported claim"
		}, "no local evidence"},
		{"missing evidence", func(c *catalog, _ *securityInsights) {
			c.Capabilities[0].Description = "Evidence: `missing.go`"
		}, "missing.go"},
		{"directory evidence", func(c *catalog, _ *securityInsights) {
			c.Capabilities[0].Description = "Evidence: `security`"
		}, "must identify a file"},
		{"escaping evidence", func(c *catalog, _ *securityInsights) {
			c.Capabilities[0].Description = "Evidence: `../outside.go`"
		}, "within the repository"},
		{"absolute evidence", func(c *catalog, _ *securityInsights) {
			c.Capabilities[0].Description = "Evidence: `/etc/passwd`"
		}, "within the repository"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.Mkdir(filepath.Join(root, "security"), 0o700))
			var capabilities, threats catalog
			require.NoError(t, readYAML("../../security/capabilities.yaml", &capabilities))
			require.NoError(t, readYAML("../../security/threats.yaml", &threats))
			for i := range capabilities.Capabilities {
				capabilities.Capabilities[i].Description = "Evidence: `evidence.go`"
			}
			for i := range threats.Threats {
				threats.Threats[i].Description = "Evidence: `evidence.go`"
			}
			var insights securityInsights
			insights.Header.SchemaVersion = "2.2.0"
			insights.Repository.Security.Assessments.Self.Evidence = threatsURL
			require.NoError(t, os.WriteFile(filepath.Join(root, "evidence.go"), []byte("evidence"), 0o600))
			tc.change(&capabilities, &insights)
			for path, value := range map[string]any{
				"security/capabilities.yaml": capabilities,
				"security/threats.yaml":      threats,
				"security-insights.yml":      insights,
			} {
				data, err := yaml.Marshal(value)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(root, path), data, 0o600))
			}
			err := validateDirectory(root)
			if tc.error == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.error)
			}
		})
	}

	for _, path := range []string{"security/capabilities.yaml", "security/threats.yaml", "security-insights.yml"} {
		t.Run("missing "+path, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.Mkdir(filepath.Join(root, "security"), 0o700))
			for _, fixture := range []string{"security/capabilities.yaml", "security/threats.yaml", "security-insights.yml"} {
				if fixture == path {
					continue
				}
				data, err := os.ReadFile(filepath.Join("../..", fixture))
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(root, fixture), data, 0o600))
			}
			require.ErrorContains(t, validateDirectory(root), "read "+filepath.Join(root, path))
		})
	}
}
