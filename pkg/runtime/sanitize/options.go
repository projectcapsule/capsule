// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package sanitize

type SanitizeOptions struct {
	StripResourceVersion bool
	StripOwnerreferences bool
	StripGeneration      bool
	StripUID             bool
	StripManagedFields   bool
	StripLastApplied     bool
	StripStatus          bool
}

func DefaultSanitizeOptions() SanitizeOptions {
	return SanitizeOptions{
		StripResourceVersion: true,
		StripOwnerreferences: true,
		StripGeneration:      true,
		StripUID:             true,
		StripManagedFields:   true,
		StripLastApplied:     true,
		StripStatus:          true,
	}
}
