// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

// Command securityassessment checks the local assessment graph after CUE schema validation.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := validateDirectory("."); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Fprintln(os.Stdout, "Security assessment references, capability coverage, and local evidence are valid")
}
