// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsInCapsuleGroups(t *testing.T) {
	groups := []string{
		"DPS-QQ-DeparEE-Upload_RW",
		"vsphere - glonqqqq-devopsq",
		"OSAAA-WOO",
		"crownuser-qq4",
		"kubernetes-abilitytologin",
		"waaazzz-prod-user",
		"Zaxxxq_Global_Team_Leader_Automation",
	}

	capsuleGroup := "kubernetes-abilitytologin"

	assert.True(t, NewUserGroupList(groups).Find(capsuleGroup), nil)
}
