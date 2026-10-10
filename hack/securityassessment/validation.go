// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

const (
	capabilitiesURL = "https://raw.githubusercontent.com/projectcapsule/capsule/main/security/capabilities.yaml"
	threatsURL      = "https://raw.githubusercontent.com/projectcapsule/capsule/main/security/threats.yaml"
)

// Descriptions use backticks to identify local evidence paths. CUE validates
// the full artifact shape; these projections only check relationships and files.
var evidencePath = regexp.MustCompile("`([^`]+)`")

type catalog struct {
	Metadata     catalogMetadata `yaml:"metadata"`
	Imports      []mapping       `yaml:"imports"`
	Capabilities []entry         `yaml:"capabilities"`
	Threats      []entry         `yaml:"threats"`
}

//nolint:tagliatelle // Match the hyphenated YAML keys in the upstream Gemara schema.
type catalogMetadata struct {
	ID                string             `yaml:"id"`
	Type              string             `yaml:"type"`
	Version           string             `yaml:"version"`
	GemaraVersion     string             `yaml:"gemara-version"`
	MappingReferences []mappingReference `yaml:"mapping-references"`
}

type mappingReference struct {
	ID      string `yaml:"id"`
	Version string `yaml:"version"`
	URL     string `yaml:"url"`
}

//nolint:tagliatelle // Match the hyphenated YAML keys in the upstream Gemara schema.
type mapping struct {
	ReferenceID string         `yaml:"reference-id"`
	Entries     []mappingEntry `yaml:"entries"`
}

//nolint:tagliatelle // Match the hyphenated YAML keys in the upstream Gemara schema.
type mappingEntry struct {
	ReferenceID string `yaml:"reference-id"`
}

type entry struct {
	ID           string    `yaml:"id"`
	Description  string    `yaml:"description"`
	Capabilities []mapping `yaml:"capabilities"`
}

type securityInsights struct {
	Header struct {
		//nolint:tagliatelle // Match the Security Insights schema key.
		SchemaVersion string `yaml:"schema-version"`
	} `yaml:"header"`
	Repository struct {
		Security struct {
			Assessments struct {
				Self struct {
					Evidence string `yaml:"evidence"`
				} `yaml:"self"`
			} `yaml:"assessments"`
		} `yaml:"security"`
	} `yaml:"repository"`
}

func readYAML(path string, target any) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	defer f.Close()

	decoder := yaml.NewDecoder(f)
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s must contain exactly one YAML document", path)
	}

	return nil
}

func validateDirectory(root string) error {
	var capabilities, threats catalog
	if err := readYAML(filepath.Join(root, "security/capabilities.yaml"), &capabilities); err != nil {
		return err
	}

	if err := readYAML(filepath.Join(root, "security/threats.yaml"), &threats); err != nil {
		return err
	}

	if err := validateCatalogs(capabilities, threats); err != nil {
		return err
	}

	var insights securityInsights
	if err := readYAML(filepath.Join(root, "security-insights.yml"), &insights); err != nil {
		return err
	}

	if insights.Header.SchemaVersion != "2.2.0" {
		return fmt.Errorf("security insights must declare the pinned schema version 2.2.0")
	}

	if insights.Repository.Security.Assessments.Self.Evidence != threatsURL {
		return fmt.Errorf("security insights self assessment must reference %s", threatsURL)
	}

	paths := make(map[string]struct{})

	for _, entries := range [][]entry{capabilities.Capabilities, threats.Threats} {
		for _, item := range entries {
			matches := evidencePath.FindAllStringSubmatch(item.Description, -1)
			if len(matches) == 0 {
				return fmt.Errorf("%s has no local evidence paths", item.ID)
			}

			for _, match := range matches {
				path := match[1]
				if !filepath.IsLocal(path) {
					return fmt.Errorf("%s evidence path %q must stay within the repository", item.ID, path)
				}

				if _, checked := paths[path]; checked {
					continue
				}

				info, err := os.Stat(filepath.Join(root, path))
				if err != nil {
					return fmt.Errorf("%s evidence path %q: %w", item.ID, path, err)
				}

				if !info.Mode().IsRegular() {
					return fmt.Errorf("%s evidence path %q must identify a file", item.ID, path)
				}

				paths[path] = struct{}{}
			}
		}
	}

	return nil
}

func validateCatalogs(capabilities, threats catalog) error {
	if err := validateCatalogMetadata(capabilities, threats); err != nil {
		return err
	}

	ids := make(map[string]bool, len(capabilities.Capabilities))
	for _, capability := range capabilities.Capabilities {
		if _, exists := ids[capability.ID]; capability.ID == "" || exists {
			return fmt.Errorf("empty or duplicate capability ID %q", capability.ID)
		}

		ids[capability.ID] = false
	}

	imports := make(map[string]bool, len(ids))
	if err := resolveMappings(threats.Imports, capabilities.Metadata.ID, ids, imports); err != nil {
		return fmt.Errorf("threat catalog imports: %w", err)
	}

	threatIDs := make(map[string]struct{}, len(threats.Threats))
	for _, threat := range threats.Threats {
		if _, exists := threatIDs[threat.ID]; threat.ID == "" || exists {
			return fmt.Errorf("empty or duplicate threat ID %q", threat.ID)
		}

		threatIDs[threat.ID] = struct{}{}

		if len(threat.Capabilities) == 0 {
			return fmt.Errorf("%s must map at least one capability", threat.ID)
		}

		if err := resolveMappings(threat.Capabilities, capabilities.Metadata.ID, imports, ids); err != nil {
			return fmt.Errorf("%s: %w", threat.ID, err)
		}
	}

	for _, capability := range capabilities.Capabilities {
		if !ids[capability.ID] {
			return fmt.Errorf("capability %s has no assessed threat", capability.ID)
		}
	}

	return nil
}

func validateCatalogMetadata(capabilities, threats catalog) error {
	if capabilities.Metadata.Type != "CapabilityCatalog" || threats.Metadata.Type != "ThreatCatalog" {
		return fmt.Errorf("assessment requires a CapabilityCatalog and a ThreatCatalog")
	}

	if capabilities.Metadata.ID == "" || threats.Metadata.ID == "" || capabilities.Metadata.ID == threats.Metadata.ID {
		return fmt.Errorf("catalog IDs must be nonempty and distinct")
	}

	if capabilities.Metadata.GemaraVersion != "1.6.0" || threats.Metadata.GemaraVersion != "1.6.0" {
		return fmt.Errorf("catalogs must declare the pinned Gemara version 1.6.0")
	}

	if capabilities.Metadata.Version == "" || capabilities.Metadata.Version != threats.Metadata.Version {
		return fmt.Errorf("catalog versions must be nonempty and match")
	}

	if len(capabilities.Capabilities) == 0 || len(threats.Threats) == 0 {
		return fmt.Errorf("assessment must define capabilities and threats")
	}
	// This assessment is self-contained. Adding external imports requires a
	// local, pinned catalog and extending this check to resolve those entries.
	if len(capabilities.Imports) != 0 || len(threats.Metadata.MappingReferences) != 1 {
		return fmt.Errorf("assessment must resolve imports against its companion capability catalog")
	}

	ref := threats.Metadata.MappingReferences[0]
	if ref.ID != capabilities.Metadata.ID || ref.Version != capabilities.Metadata.Version || ref.URL != capabilitiesURL {
		return fmt.Errorf("threat catalog mapping reference must match the capability catalog ID, version, and URL")
	}

	return nil
}

func resolveMappings(mappings []mapping, catalogID string, available, referenced map[string]bool) error {
	seen := make(map[string]struct{})

	for _, m := range mappings {
		if m.ReferenceID != catalogID || len(m.Entries) == 0 {
			return fmt.Errorf("capability mapping must reference %s and contain entries", catalogID)
		}

		for _, item := range m.Entries {
			if _, exists := available[item.ReferenceID]; !exists {
				return fmt.Errorf("unresolved capability %q", item.ReferenceID)
			}

			if _, duplicate := seen[item.ReferenceID]; duplicate {
				return fmt.Errorf("duplicate capability mapping %q", item.ReferenceID)
			}

			seen[item.ReferenceID] = struct{}{}
			referenced[item.ReferenceID] = true
		}
	}

	return nil
}
