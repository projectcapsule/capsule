// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package cert

type CaNotYetValidError struct{}

func (CaNotYetValidError) Error() string {
	return "The current CA is not yet valid"
}

type CaExpiredError struct{}

func (CaExpiredError) Error() string {
	return "The current CA is expired"
}
