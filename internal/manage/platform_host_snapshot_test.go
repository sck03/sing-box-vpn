package manage

import "testing"

func TestDeploymentStatesRequiresCompleteSettledSnapshot(t *testing.T) {
	states := map[string]Object{}
	for _, name := range platformServices {
		states[name] = Object{"LoadState": "loaded", "ActiveState": "active", "UnitFileState": "enabled"}
	}
	states["argo"] = Object{"LoadState": "not-found", "ActiveState": "inactive"}
	active, enabled, err := deploymentStates(states, true)
	if err != nil || len(active) != 4 || len(enabled) != 4 {
		t.Fatal(active, enabled, err)
	}
	for _, unit := range []Object{nil, {"LoadState": "not-found", "ActiveState": "inactive"}, {"LoadState": "loaded", "ActiveState": "activating"}, {"LoadState": "loaded", "ActiveState": "inactive", "UnitFileState": "masked"}} {
		states["sing-box"] = unit
		if _, _, err := deploymentStates(states, true); err == nil {
			t.Fatal("unsafe rollback snapshot", unit)
		}
	}
}
