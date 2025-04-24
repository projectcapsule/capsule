// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package resourcepools

import (
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func Add(
	log logr.Logger,
	mgr manager.Manager,
	Recorder record.EventRecorder,
) (err error) {
	if err = (&resourcePoolController{
		Client:   mgr.GetClient(),
		Log:      log.WithName("Pools"),
		Recorder: Recorder,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create pool controller: %w", err)
	}

	if err = (&resourceClaimController{
		Client:   mgr.GetClient(),
		Log:      log.WithName("Claims"),
		Recorder: Recorder,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create claim controller: %w", err)
	}

	return nil
}
