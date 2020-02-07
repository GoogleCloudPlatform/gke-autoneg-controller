/*
Copyright 2019-2021 Google LLC.

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
	malformedJSON      = `{`
	validConfig        = `{"backend_services":{"80":{"name":"http-be","max_rate_per_endpoint":100},"443":{"name":"https-be","max_connections_per_endpoint":1000}}}`
	brokenConfig       = `{"backend_services":{"80":{"name":"http-be","max_rate_per_endpoint":"100"},"443":{"name":"https-be","max_connections_per_endpoint":1000}}}`
	validStatus        = `{}`
	validAutonegConfig = `{}`
	validAutonegStatus = `{}`
	invalidStatus      = `{`
	oldValidConfig     = `{"name":"test", "max_rate_per_endpoint":100}`
	oldBrokenConfig    = `{"name":"test", "max_rate_per_endpoint":"100"}`

	validNegConfig   = `{"exposed_ports": {"80":{"name":"test"}}}`
	wrongNegConfig   = `{"exposed_ports": {}}`
	tooManyNegConfig = `{"exposed_ports": {"80":{"name":"test"},"443":{"name":"tls"}}}`
	brokenNegConfig  = `{"exposed_ports": {}`
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
		"valid autoneg with valid neg status",
		map[string]string{
			autonegAnnotation:   validConfig,
			negStatusAnnotation: validStatus,
		},
		true,
		false,
	},
}

var oldStatusTests = []struct {
	name        string
	annotations map[string]string
	valid       bool
	err         bool
}{
	{
		"(legacy) not using autoneg",
		map[string]string{},
		false,
		false,
	},
	{
		"(legacy) autoneg with malformed config",
		map[string]string{
			oldAutonegAnnotation: malformedJSON,
			negAnnotation:        validNegConfig,
		},
		true,
		true,
	},
	{
		"(legacy) autoneg with broken config",
		map[string]string{
			oldAutonegAnnotation: oldBrokenConfig,
			negAnnotation:        validNegConfig,
		},
		true,
		true,
	},
	{
		"(legacy) valid autoneg",
		map[string]string{
			oldAutonegAnnotation: oldValidConfig,
			negAnnotation:        validNegConfig,
		},
		true,
		false,
	},
	{
		"(legacy) valid autoneg with too many ports",
		map[string]string{
			oldAutonegAnnotation: oldValidConfig,
			negAnnotation:        tooManyNegConfig,
		},
		true,
		true,
	},
	{
		"(legacy) valid autoneg with invalid status",
		map[string]string{
			oldAutonegAnnotation:       oldValidConfig,
			oldAutonegStatusAnnotation: malformedJSON,
			negAnnotation:              validNegConfig,
		},
		true,
		true,
	},
	{
		"(legacy) valid autoneg with valid status",
		map[string]string{
			oldAutonegAnnotation:       oldValidConfig,
			oldAutonegStatusAnnotation: validStatus,
			negAnnotation:              validNegConfig,
		},
		true,
		false,
	},
	{
		"(legacy) valid autoneg with invalid neg status",
		map[string]string{
			oldAutonegAnnotation: oldValidConfig,
			negStatusAnnotation:  malformedJSON,
			negAnnotation:        validNegConfig,
		},
		true,
		true,
	},
	{
		"(legacy) valid autoneg with valid neg status",
		map[string]string{
			oldAutonegAnnotation: oldValidConfig,
			negStatusAnnotation:  validAutonegStatus,
			negAnnotation:        validNegConfig,
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

func TestGetOldStatuses(t *testing.T) {
	for _, st := range oldStatusTests {
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
	config OldAutonegConfig
	err    bool
}{
	{
		"default config",
		OldAutonegConfig{},
		false,
	},
}

func TestValidateConfig(t *testing.T) {
	for _, ct := range configTests {
		err := validateOldConfig(ct.config)
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
	b.MaxConnectionsPerEndpoint = a.MaxConnectionsPerEndpoint
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
	backendsNone  = map[string]Backends{"80": Backends{name: "test"}}

	// basic state
	configBasicPort80 = AutonegNEGConfig{Name: "test", Rate: 100}
	configBasic       = AutonegConfig{BackendServices: map[string]AutonegNEGConfig{"80": configBasicPort80}}

	statusBasicWithNEGs = AutonegStatus{
		AutonegConfig: configBasic,
		NEGStatus:     negStatus,
	}
	backendsBasicWithNEGs = map[string]Backends{"80": Backends{name: "test", backends: []compute.Backend{
		statusBasicWithNEGs.Backend("80", getGroup(fakeProject, "zone1", fakeNeg)),
		statusBasicWithNEGs.Backend("80", getGroup(fakeProject, "zone2", fakeNeg)),
	}}}

	// value changed state
	configValueChangePort80   = AutonegNEGConfig{Name: "test", Rate: 200}
	configValueChange         = AutonegConfig{BackendServices: map[string]AutonegNEGConfig{"80": configValueChangePort80}}
	statusValueChangeWithNEGs = AutonegStatus{
		AutonegConfig: configValueChange,
		NEGStatus:     negStatus,
	}
	backendsValueChangeWithNEGs = map[string]Backends{"80": Backends{name: "test", backends: []compute.Backend{
		statusValueChangeWithNEGs.Backend("80", getGroup(fakeProject, "zone1", fakeNeg)),
		statusValueChangeWithNEGs.Backend("80", getGroup(fakeProject, "zone2", fakeNeg)),
	}}}

	// named changed state
	configNameChangePort80   = AutonegNEGConfig{Name: "changed", Rate: 100}
	configNameChange         = AutonegConfig{BackendServices: map[string]AutonegNEGConfig{"80": configNameChangePort80}}
	statusNameChangeWithNEGs = AutonegStatus{
		AutonegConfig: configNameChange,
		NEGStatus:     negStatus,
	}
	backendsNameChangeWithNEGs = map[string]Backends{"80": Backends{name: "changed", backends: []compute.Backend{
		statusNameChangeWithNEGs.Backend("80", getGroup(fakeProject, "zone1", fakeNeg)),
		statusNameChangeWithNEGs.Backend("80", getGroup(fakeProject, "zone2", fakeNeg)),
	}}}
)

var reconcileTests = []struct {
	name     string
	actual   AutonegStatus
	intended AutonegStatus
	removes  map[string]Backends
	upserts  map[string]Backends
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
		removes, upserts := ReconcileStatus(fakeProject, rt.actual, rt.intended)
		for port := range rt.removes {
			if !rt.removes[port].isEqual(removes[port]) {
				t.Errorf("Set %q: Removed port %s backends: expected:\n%+v\n got:\n%+v", rt.name, port, rt.removes[port], removes[port])
			}
			if !rt.upserts[port].isEqual(upserts[port]) {
				t.Errorf("Set %q: Upserted port %s backends: expected:\n%+v\n got:\n%+v", rt.name, port, rt.upserts[port], upserts[port])
			}
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
