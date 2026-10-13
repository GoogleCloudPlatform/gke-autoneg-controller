package controllers

import (
	"context"
	"encoding/json"
	"testing"

	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReconcilePreferredRegions(t *testing.T) {
	for _, tt := range []struct {
		name      string
		namespace string
		service   string
		region    string
		want      string
	}{
		{"preferred region", "payments", "checkout", "europe-west1", "PREFERRED"},
		{"other region", "payments", "checkout", "us-central1", "DEFAULT"},
		{"different service policy", "payments", "refunds", "us-central1", "PREFERRED"},
		{"unmapped service", "payments", "other", "europe-west1", ""},
		{"different namespace", "other", "checkout", "europe-west1", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			annotation := `{"backend_services":{"80":[{"name":"automatic","max_rate_per_endpoint":100},{"name":"explicit-default","max_rate_per_endpoint":100,"preference":"DEFAULT"}],"443":[{"name":"explicit-preferred","max_connections_per_endpoint":100,"preference":"PREFERRED"},{"name":"automatic-tls","max_connections_per_endpoint":100}]}}`
			svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
				Name: tt.service, Namespace: tt.namespace,
				Annotations: map[string]string{autonegAnnotation: annotation},
			}}
			r := &ServiceReconciler{
				Client:            fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(svc).Build(),
				BackendController: stubBackendController{}, Recorder: record.NewFakeRecorder(10),
				AllowServiceName: true, ErrorCount: map[string]int{}, LocalRegion: tt.region,
				PreferredRegions: map[string]map[string]string{"payments": {"checkout": "europe-west1", "refunds": "us-central1"}},
			}
			key := types.NamespacedName{Namespace: tt.namespace, Name: tt.service}
			if _, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: key}); err != nil {
				t.Fatal(err)
			}
			if err := r.Get(t.Context(), key, svc); err != nil {
				t.Fatal(err)
			}
			if svc.Annotations[autonegAnnotation] != annotation {
				t.Fatal("input annotation changed")
			}
			var status AutonegStatus
			if err := json.Unmarshal([]byte(svc.Annotations[autonegStatusAnnotation]), &status); err != nil {
				t.Fatal(err)
			}
			for _, expected := range []struct{ port, backend, preference string }{
				{"80", "automatic", tt.want}, {"443", "automatic-tls", tt.want},
				{"80", "explicit-default", "DEFAULT"}, {"443", "explicit-preferred", "PREFERRED"},
			} {
				if got := status.BackendServices[expected.port][expected.backend].Preference; got != expected.preference {
					t.Errorf("%s preference = %q; want %q", expected.backend, got, expected.preference)
				}
			}
		})
	}
}

func TestGetStatusesPreferredRegionRequiresLocalRegion(t *testing.T) {
	r := &ServiceReconciler{PreferredRegions: map[string]map[string]string{"ns": {"svc": "us-central1"}}}
	_, _, err := getStatuses(t.Context(), "ns", "svc", map[string]string{
		autonegAnnotation: `{"backend_services":{"80":[{"max_rate_per_endpoint":100}]}}`,
	}, r)
	if err == nil {
		t.Fatal("automatic preference without local region must fail")
	}
}

func TestReconcileBackendsPreference(t *testing.T) {
	for _, tt := range []struct{ name, old, intended, want string }{
		{"enable", "", "PREFERRED", "PREFERRED"},
		{"promote", "DEFAULT", "PREFERRED", "PREFERRED"},
		{"demote", "PREFERRED", "DEFAULT", "DEFAULT"},
		{"remove configuration", "PREFERRED", "", "DEFAULT"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			makeStatus := func(preference string) AutonegStatus {
				return AutonegStatus{
					AutonegConfig: AutonegConfig{BackendServices: map[string]map[string]AutonegNEGConfig{
						"80": {"fake": {Name: "fake", Rate: 100, Preference: preference}},
					}},
					NEGStatus: NEGStatus{NEGs: map[string]string{"80": "neg"}, Zones: []string{"us-central1-a"}},
				}
			}
			actual, intended := makeStatus(tt.old), makeStatus(tt.intended)
			backend := actual.Backend("fake", "80", getGroup("project", "us-central1-a", "neg"))
			other := &compute.Backend{Group: "another-cluster-neg", BalancingMode: "RATE", MaxRatePerEndpoint: 200, CapacityScaler: 1, Preference: "PREFERRED"}
			server := newFakeBackendServiceServer(
				map[string]*compute.BackendService{"fake": {Name: "fake", Backends: []*compute.Backend{&backend, other}}},
				map[string][][2]string{"fake": {{"GET", "backendServices"}, {"PATCH", "backendServices"}, {"GET", "operations"}}},
				map[string][]string{"fake": {computeOperationStatusDone}}, t,
			)
			t.Cleanup(server.Close)
			service, err := compute.NewService(t.Context(), option.WithEndpoint(server.URL), option.WithoutAuthentication())
			if err != nil {
				t.Fatal(err)
			}
			controller := NewBackendController("project", service)
			if err := controller.ReconcileBackends(context.Background(), actual, intended, false); err != nil {
				t.Fatal(err)
			}
			if remaining := server.bsExpectedCalls["fake"]; len(remaining) != 0 {
				t.Fatalf("missing API calls: %v", remaining)
			}
			backends := server.bss["fake"].Backends
			if len(backends) != 2 || backends[0].Preference != tt.want || backends[1].Preference != other.Preference {
				t.Fatalf("unexpected backends after preference update: %+v", backends)
			}
			// The API can return DEFAULT for an omitted preference; that must not trigger another patch.
			server.bsExpectedCalls["fake"] = [][2]string{{"GET", "backendServices"}}
			if err := controller.ReconcileBackends(t.Context(), intended, intended, false); err != nil {
				t.Fatal(err)
			}
			if len(server.bsExpectedCalls["fake"]) != 0 {
				t.Fatal("second reconciliation did not read backend service")
			}
		})
	}
}
