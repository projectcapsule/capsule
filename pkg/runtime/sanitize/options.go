// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package sanitize

type SanitizeOptions struct {
	StripUID           bool
	StripManagedFields bool
	StripLastApplied   bool
	StripStatus        bool
}

func DefaultSanitizeOptions() SanitizeOptions {
	return SanitizeOptions{
		StripUID:           true,
		StripManagedFields: true,
		StripLastApplied:   true,
		StripStatus:        true,
	}
}
