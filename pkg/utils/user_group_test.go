package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsInCapsuleGroup(t *testing.T) {
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
