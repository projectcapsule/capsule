// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package functions

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func deterministicUUID(parts ...string) string {
	// Normalize: trim whitespace; keep empty strings if caller passes them intentionally.
	// If you prefer to skip empties, filter them out here.
	clean := make([]string, 0, len(parts))
	for _, p := range parts {
		clean = append(clean, strings.TrimSpace(p))
	}

	// Deterministic stable separator. Using a non-printable delimiter reduces accidental collisions.
	// "|" is also fine; \x1f is "unit separator" and common for this use.
	msg := strings.Join(clean, "\x1f")

	sum := sha256.Sum256([]byte(msg))
	b := sum[:16] // 128-bit UUID material

	// Set RFC4122 variant (10xxxxxx)
	b[8] = (b[8] & 0x3f) | 0x80
	// Set version 5 (0101xxxx)
	b[6] = (b[6] & 0x0f) | 0x50

	// Format as 8-4-4-4-12 hex
	hex32 := hex.EncodeToString(b) // 32 lowercase hex chars
	uuid := hex32[0:8] + "-" + hex32[8:12] + "-" + hex32[12:16] + "-" + hex32[16:20] + "-" + hex32[20:32]

	return strings.ToUpper(uuid)
}
