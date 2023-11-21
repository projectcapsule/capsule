// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"crypto/md5" //#nosec
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	// Annotation name part must be no more than 63 characters.
	maxAnnotationLength = 63

	HardCapsuleQuotaAnnotation = "quota.capsule.clastix.io/hard-"
	UsedCapsuleQuotaAnnotation = "quota.capsule.clastix.io/used-"
)

func createAnnotation(format string, resource fmt.Stringer) (string, error) {
	resourceStr := strings.ReplaceAll(resource.String(), "/", "_")

	hash := md5.Sum([]byte(resourceStr)) //#nosec

	hashed := hex.EncodeToString(hash[:])
	capsuleHashed := format + hashed
	capsuleAnnotation := format + resourceStr

	switch {
	case len(capsuleAnnotation) <= maxAnnotationLength:
		return capsuleAnnotation, nil
	case len(capsuleHashed) <= maxAnnotationLength:
		return capsuleHashed, nil
	case len(hashed) <= maxAnnotationLength:
		return hashed, nil
	default:
		return "", fmt.Errorf("the annotation name would exceed the maximum supported length (%d), skipping", maxAnnotationLength)
	}
}

func UsedQuotaFor(resource fmt.Stringer) (string, error) {
	return createAnnotation(UsedCapsuleQuotaAnnotation, resource)
}

func HardQuotaFor(resource fmt.Stringer) (string, error) {
	return createAnnotation(HardCapsuleQuotaAnnotation, resource)
}
