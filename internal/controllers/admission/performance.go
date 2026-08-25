// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"time"

	"github.com/go-logr/logr"
)

const admissionConfigurationEventMarker = "capsule-admission-configuration"

func logOperationDuration(logger logr.Logger, operation string, started time.Time) {
	logger.V(4).Info("admission operation completed", "operation", operation, "duration", time.Since(started))
}
