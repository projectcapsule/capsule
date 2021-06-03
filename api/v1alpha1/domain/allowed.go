// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package domain

type AllowedList interface {
	ExactMatch(value string) bool
	RegexMatch(value string) bool
}
