// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package secret

type MissingCaError struct{}

func (MissingCaError) Error() string {
	return "CA has not been created yet, please generate a new"
}
