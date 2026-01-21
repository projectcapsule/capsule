// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package errors

type RunningInOutOfClusterModeError struct{}

func (r RunningInOutOfClusterModeError) Error() string {
	return "cannot retrieve the leader Pod, probably running in out of the cluster mode"
}

type CaNotYetValidError struct{}

func (CaNotYetValidError) Error() string {
	return "The current CA is not yet valid"
}

type CaExpiredError struct{}

func (CaExpiredError) Error() string {
	return "The current CA is expired"
}
