package application

import "testing"

func TestTaskControlSupportedRuntimeVersions(t *testing.T) {
	for _, version := range []string{"1.2.0", "1.2.1", "1.2.2", "1.2.3", "1.2.4"} {
		if !taskControlRuntimeV2Version(version) {
			t.Errorf("supported runtime %s rejected", version)
		}
	}
	for _, version := range []string{"", "1.1.4", "1.2.5", "1.2.4-beta", "01.2.4", "2.0.0"} {
		if taskControlRuntimeV2Version(version) {
			t.Errorf("unverified runtime %s accepted", version)
		}
	}
}
