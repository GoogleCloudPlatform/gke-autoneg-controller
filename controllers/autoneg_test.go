/*
Copyright 2019 Google LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"reflect"
	"testing"

	"google.golang.org/api/compute/v1"
)

var (
	malformedJSON  = `{`
	validConfig    = `{"name":"test", "max_rate_per_endpoint":100}`
	brokenConfig   = `{"name":"test", "max_rate_per_endpoint":"100"}`
	validStatus    = `{}`
	validNegConfig = `{}`
)

var statusTests = []struct {
	name        string
	annotations map[string]string
	valid       bool
	err         bool
}{
	{
		"not using autoneg",
		map[string]string{},
		false,
		false,
	},
	{
		"autoneg with malformed config",
		map[string]string{
			autonegAnnotation: malformedJSON,
		},
		true,
		true,
	},
	{
		"autoneg with broken config",
		map[string]string{
			autonegAnnotation: brokenConfig,
		},
		true,
		true,
	},
	{
		"valid autoneg",
		map[string]string{
			autonegAnnotation: validConfig,
		},
		true,
		false,
	},
	{
		"valid autoneg with invalid status",
		map[string]string{
			autonegAnnotation:       validConfig,
			autonegStatusAnnotation: malformedJSON,
		},
		true,
		true,
	},
	{
		"valid autoneg with valid status",
		map[string]string{
			autonegAnnotation:       validConfig,
			autonegStatusAnnotation: validStatus,
		},
		true,
		false,
	},
	{
		"valid autoneg with invalid neg config",
		map[string]string{
			autonegAnnotation:   validConfig,
			negStatusAnnotation: malformedJSON,
		},
		true,
		true,
	},
	{
		"valid autoneg with valid neg config",
		map[string]string{
			autonegAnnotation:   validConfig,
			negStatusAnnotation: validNegConfig,
		},
		true,
		false,
	},
}

func TestGetStatuses(t *testing.T) {
	for _, st := range statusTests {
		_, valid, err := getStatuses("test", st.annotations)
		if err != nil && !st.err {
			t.Errorf("Set %q: expected no error, got one: %v", st.name, err)
		}
		if err == nil && st.err {
			t.Errorf("Set %q: expected error, got none", st.name)
		}
		if !valid && st.valid {
			t.Errorf("Set %q: expected autoneg config, got none", st.name)
		}
		if valid && !st.valid {
			t.Errorf("Set %q: expected no autoneg config, got one", st.name)
		}
	}
}

var configTests = []struct {
	name   string
	config AutonegConfig
	err    bool
}{
	{
		"default config",
		AutonegConfig{},
		false,
	},
}

func TestValidateConfig(t *testing.T) {
	for _, ct := range configTests {
		err := validateConfig(ct.config)
		if err == nil && ct.err {
			t.Errorf("Set %q: expected error, got none", ct.name)
		}
		if err != nil && !ct.err {
			t.Errorf("Set %q: expected no error, got one: %v", ct.name, err)
		}
	}
}

func relevantCopy(a compute.Backend) compute.Backend {
	b := compute.Backend{}
	b.Group = a.Group
	b.MaxRatePerEndpoint = a.MaxRatePerEndpoint
	return b
}

func (b Backends) isEqual(ob Backends) bool {
	if b.name != ob.name {
		return false
	}
	newB := []compute.Backend{}
	for _, be := range b.backends {
		newB = append(newB, relevantCopy(be))
	}
	newOB := []compute.Backend{}
	for _, be := range ob.backends {
		newOB = append(newOB, relevantCopy(be))
	}
	return reflect.DeepEqual(newB, newOB)
}

var (
	fakeNeg     = "neg_name"
	fakeProject = "project"
	negStatus   = NEGStatus{
		NEGs:  map[string]string{"80": fakeNeg},
		Zones: []string{"zone1", "zone2"},
	}

	// initial state
	statusInitial = AutonegStatus{AutonegConfig: configBasic}
	backendsNone  = Backends{name: "test"}

	// basic state
	configBasic         = AutonegConfig{Name: "test", Rate: 100}
	statusBasicWithNEGs = AutonegStatus{
		AutonegConfig: configBasic,
		NEGStatus:     negStatus,
	}
	backendsBasicWithNEGs = Backends{name: "test", backends: []compute.Backend{
		statusBasicWithNEGs.Backend(getGroup(fakeProject, "zone1", fakeNeg)),
		statusBasicWithNEGs.Backend(getGroup(fakeProject, "zone2", fakeNeg)),
	}}

	// value changed state
	configValueChange         = AutonegConfig{Name: "test", Rate: 200}
	statusValueChangeWithNEGs = AutonegStatus{
		AutonegConfig: configValueChange,
		NEGStatus:     negStatus,
	}
	backendsValueChangeWithNEGs = Backends{name: "test", backends: []compute.Backend{
		statusValueChangeWithNEGs.Backend(getGroup(fakeProject, "zone1", fakeNeg)),
		statusValueChangeWithNEGs.Backend(getGroup(fakeProject, "zone2", fakeNeg)),
	}}

	// named changed state
	configNameChange         = AutonegConfig{Name: "changed", Rate: 100}
	statusNameChangeWithNEGs = AutonegStatus{
		AutonegConfig: configNameChange,
		NEGStatus:     negStatus,
	}
	backendsNameChangeWithNEGs = Backends{name: "changed", backends: []compute.Backend{
		statusNameChangeWithNEGs.Backend(getGroup(fakeProject, "zone1", fakeNeg)),
		statusNameChangeWithNEGs.Backend(getGroup(fakeProject, "zone2", fakeNeg)),
	}}
)

var reconcileTests = []struct {
	name     string
	actual   AutonegStatus
	intended AutonegStatus
	remove   Backends
	upsert   Backends
}{
	{
		"inital to basic",
		statusInitial,
		statusBasicWithNEGs,
		backendsNone,
		backendsBasicWithNEGs,
	},
	{
		"basic to value changed",
		statusBasicWithNEGs,
		statusValueChangeWithNEGs,
		backendsNone,
		backendsValueChangeWithNEGs,
	},
	// {
	// 	"basic to name changed",
	// 	statusBasicWithNEGs,
	// 	statusNameChangeWithNEGs,
	// 	backendsBasicWithNEGs,
	// 	backendsNameChangeWithNEGs,
	// },
}

func TestReconcileStatuses(t *testing.T) {
	for _, rt := range reconcileTests {
		remove, upsert := ReconcileStatus(fakeProject, rt.actual, rt.intended)
		if !rt.remove.isEqual(remove) {
			t.Errorf("Set %q: Removed backends: expected:\n%+v\n got:\n%+v", rt.name, rt.remove, remove)
		}
		if !rt.upsert.isEqual(upsert) {
			t.Errorf("Set %q: Upserted backends: expected:\n%+v\n got:\n%+v", rt.name, rt.upsert, upsert)
		}
	}
}

func Test_checkOperation(t *testing.T) {
	type test struct {
		noErr bool
		op    *compute.Operation
	}

	tests := []test{
		{
			noErr: false,
			op: &compute.Operation{
				Status: "invalid",
			},
		},
		{
			noErr: false,
			op: &compute.Operation{
				Status: computeOperationStatusPending,
			},
		},
		{
			noErr: false,
			op: &compute.Operation{
				Status: computeOperationStatusRunning,
			},
		},
		{
			noErr: false,
			op: &compute.Operation{
				Status: computeOperationStatusDone,
				Error:  &compute.OperationError{},
			},
		},
		{
			noErr: true,
			op: &compute.Operation{
				Status: computeOperationStatusDone,
			},
		},
	}
	for i, tt := range tests {
		err := checkOperation(tt.op)
		if (err == nil && !tt.noErr) || (err != nil && tt.noErr) {
			t.Errorf("%d: failed.", i+1)
		}
	}
}
