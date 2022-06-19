// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tls

type RunningInOutOfClusterModeError struct{}

func (r RunningInOutOfClusterModeError) Error() string {
	return "cannot retrieve the leader Pod, probably running in out of the cluster mode"
}
