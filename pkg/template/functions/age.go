// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package functions

import (
	"filippo.io/age"
)

type AgeKeyPair struct {
	// Identity is the private key, e.g. AGE-SECRET-KEY-1...
	Identity string `json:"identity" yaml:"identity"`

	// Recipient is the public key, e.g. age1...
	Recipient string `json:"recipient" yaml:"recipient"`
}

func generateAgeKey() any {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return map[string]any{
			"Error": err.Error(),
		}
	}

	return AgeKeyPair{
		Identity:  identity.String(),
		Recipient: identity.Recipient().String(),
	}
}

func generateAgePQKey() any {
	identity, err := age.GenerateHybridIdentity()
	if err != nil {
		return map[string]any{
			"Error": err.Error(),
		}
	}

	return AgeKeyPair{
		Identity:  identity.String(),
		Recipient: identity.Recipient().String(),
	}
}
