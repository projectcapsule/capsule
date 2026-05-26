// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"crypto/sha256"
	"strconv"

	"encoding/hex"
	"hash/fnv"
	"strings"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func getFieldOwner(name string, namespace string) string {
	if namespace == "" {
		namespace = "Cluster"
	}

	h := fnv.New64a()
	_, _ = h.Write([]byte(namespace))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(name))

	return strconv.FormatUint(h.Sum64(), 36)
}

func shortHash(value string, length int) string {
	sum := sha256.Sum256([]byte(value))
	encoded := hex.EncodeToString(sum[:])

	if length > len(encoded) {
		return encoded
	}

	return encoded[:length]
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}

	return strings.TrimRight(value[:max], "-_.")
}

func getSelectorForCreatedResourcesExclusion() (labels.Selector, error) {
	selector := labels.NewSelector()

	req, err := labels.NewRequirement(
		meta.CreatedByCapsuleLabel,
		selection.NotIn,
		[]string{meta.ValueControllerResources},
	)
	if err != nil {
		return nil, err
	}

	selector.Add(*req)

	return selector, nil
}
