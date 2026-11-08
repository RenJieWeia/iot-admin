package admin

import "testing"

func TestValidateBatchAction(t *testing.T) {
	cases := []struct {
		entity string
		action string
		ok     bool
	}{
		{"terminals", "enable", true},
		{"gateways", "disable", true},
		{"quality-rules", "enable", true},
		{"data-domains", "delete", true},
		{"thing-models", "delete", true},
		{"data-domains", "enable", false},
		{"thing-models", "disable", false},
		{"snapshot-bindings", "delete", false},
		{"terminals", "destroy", false},
	}
	for _, c := range cases {
		err := validateBatchAction(Entity{Name: c.entity}, c.action)
		if c.ok && err != nil {
			t.Fatalf("%s/%s 应允许，实际 %v", c.entity, c.action, err)
		}
		if !c.ok && err == nil {
			t.Fatalf("%s/%s 应拒绝", c.entity, c.action)
		}
	}
}
