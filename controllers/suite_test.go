/*
Copyright 2021 Google LLC.

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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var k8sClient client.Client
var k8sManager ctrl.Manager
var testEnv *envtest.Environment
var cancel context.CancelFunc
var backendController *TestBackendController
var fakeServer *fakeBackendServiceServer
var projectTestName = "ctrl-test-project"

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	var ctx context.Context
	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: false,
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = corev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	k8sManager, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	fakeServer = newFakeBackendServiceServer(nil, nil, nil, GinkgoT())
	service, _ := compute.NewService(ctx,
		option.WithEndpoint(fakeServer.URL), option.WithoutAuthentication())

	backendController = &TestBackendController{Counter: 0,
		BackendController: NewBackendController(projectTestName, service),
	}
	duration := 1 * time.Second

	sr := &ServiceReconciler{
		Client:              k8sManager.GetClient(),
		BackendController:   backendController,
		Recorder:            k8sManager.GetEventRecorderFor("autoneg-controller"),
		ServiceNameTemplate: serviceNameTemplate,
		AllowServiceName:    true,
		AlwaysReconcile:     true,
		ReconcileDuration:   &duration,
	}
	sr.RegisterMetrics()
	err = sr.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred())
	}()
}, 60)

type TestBackendController struct {
	BackendController
	Counter int
}

func (t *TestBackendController) ReconcileBackends(ctx context.Context, as AutonegStatus, is AutonegStatus, deleting bool) error {
	t.Counter++
	// Use controller logger for better test output control
	logf.Log.WithName("test-backend-controller").Info("ReconcileBackends called", "counter", t.Counter)
	if t.BackendController != nil {
		return t.BackendController.ReconcileBackends(ctx, as, is, deleting)
	}
	return nil
}

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

type fakeBackendServiceServer struct {
	*httptest.Server
	sync.RWMutex
	bss                 map[string]*compute.BackendService
	bsExpectedCalls     map[string][][2]string
	bsOperationStatuses map[string][]string
	t                   GinkgoTInterface
}

func (fbss *fakeBackendServiceServer) setTestingT(t *testing.T) { fbss.t = t }
func (fbss *fakeBackendServiceServer) setBackendServicesExpectedCalls(m map[string][][2]string) {
	fbss.bsExpectedCalls = m
}

func (fbss *fakeBackendServiceServer) setBackendServicesExpectedCallsFor(bs string, calls [][2]string) {
	fbss.Lock()
	defer fbss.Unlock()
	if fbss.bsExpectedCalls == nil {
		fbss.bsExpectedCalls = make(map[string][][2]string)
	}
	fbss.bsExpectedCalls[bs] = calls
}

func (fbss *fakeBackendServiceServer) getBackendServicesExpectedCallsFor(bs string) [][2]string {
	fbss.Lock()
	defer fbss.Unlock()
	return fbss.bsExpectedCalls[bs]
}

func (fbss *fakeBackendServiceServer) setBackendServicesOperations(m map[string][]string) {
	fbss.bsOperationStatuses = m
}
func (fbss *fakeBackendServiceServer) setBackendServicesOperationsFor(bs string, ops []string) {
	fbss.Lock()
	defer fbss.Unlock()
	if fbss.bsOperationStatuses == nil {
		fbss.bsOperationStatuses = make(map[string][]string)
	}
	fbss.bsOperationStatuses[bs] = ops
}

func (fbss *fakeBackendServiceServer) getBackendServicesOperationsFor(bs string) []string {
	fbss.Lock()
	defer fbss.Unlock()
	return fbss.bsOperationStatuses[bs]
}

func (fbss *fakeBackendServiceServer) usedBackendServiceIds_unlocked() []uint64 {
	return slices.Collect(func(yield func(uint64) bool) {
		for _, bs := range fbss.bss {
			if !yield(bs.Id) {
				return
			}
		}
	})
}

func (fbss *fakeBackendServiceServer) usedBackendServiceIds() []uint64 {
	fbss.Lock()
	defer fbss.Unlock()
	return fbss.usedBackendServiceIds_unlocked()
}

func (fbss *fakeBackendServiceServer) addEmptyBackendService_unlocked(name string) *compute.BackendService {
	bs, ok := fbss.bss[name]
	if !ok {
		usedIds := fbss.usedBackendServiceIds_unlocked()
		lid := uint64(1)
		for ; slices.Contains(usedIds, lid); lid++ {
		}
		bs = &compute.BackendService{
			Kind:            "compute#backendService",
			Id:              lid,
			Name:            name,
			ForceSendFields: []string{"Backends"},
		}
		if fbss.bss == nil {
			fbss.bss = make(map[string]*compute.BackendService)
		}
		fbss.bss[name] = bs
	}
	return bs
}

func (fbss *fakeBackendServiceServer) addEmptyBackendService(name string) *compute.BackendService {
	fbss.Lock()
	defer fbss.Unlock()
	return fbss.addEmptyBackendService_unlocked(name)
}

func (fbss *fakeBackendServiceServer) removeBackendService(name string) {
	fbss.Lock()
	defer fbss.Unlock()
	delete(fbss.bss, name)
}

func (fbss *fakeBackendServiceServer) addBackendToBackendService(name string, b *compute.Backend) {
	fbss.Lock()
	defer fbss.Unlock()
	bs, ok := fbss.bss[name]
	if !ok {
		bs = fbss.addEmptyBackendService_unlocked(name)
	}
	if !slices.ContainsFunc(bs.Backends, func(a *compute.Backend) bool {
		return a.Group == b.Group
	}) {
		bs.Backends = append(bs.Backends, b)
	}
}

func (fbss *fakeBackendServiceServer) removeBackendFromBackendService(name string, b *compute.Backend) {
	fbss.Lock()
	defer fbss.Unlock()
	bs, ok := fbss.bss[name]
	if !ok {
		return
	}
	bs.Backends = slices.DeleteFunc(bs.Backends, func(a *compute.Backend) bool {
		return a.Group == b.Group
	})
}

func (fbss *fakeBackendServiceServer) getRequestDetails(req *http.Request) (string, string, string, error) {
	if req.URL == nil {
		return "", "", "", fmt.Errorf("invalid request, URL is missing: %v", req)
	}
	parts := strings.Split(req.URL.Path, "/")
	if len(parts) < 2 {
		return "", "", "", fmt.Errorf("invalid request path: %s", req.URL.Path)
	}
	bsName := parts[len(parts)-1]
	resType := parts[len(parts)-2]
	return req.Method, resType, bsName, nil
}

func (fbss *fakeBackendServiceServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var fatalf, logf func(format string, args ...any)
	if fbss.t != nil {
		fatalf = fbss.t.Fatalf
		logf = fbss.t.Logf
	} else {
		fatalf = func(format string, args ...any) {}
		logf = func(format string, args ...any) { fmt.Printf(format, args...) }
	}

	met, typ, name, err := fbss.getRequestDetails(r)
	logf("ServeHTTP: %s %s %s - %s - err: %v\n",
		met, typ, name, r.URL.String(), err)

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		logf("ServeHTTP: response code: %v\n", http.StatusBadRequest)
		return
	}
	if typ != "backendServices" && typ != "operations" {
		w.WriteHeader(http.StatusBadRequest)
		logf("ServeHTTP: response code: %v\n", http.StatusBadRequest)
		return
	}

	fbss.Lock()
	defer fbss.Unlock()

	if expectedCalls, ok := fbss.bsExpectedCalls[name]; ok {
		if len(expectedCalls) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			logf("ServeHTTP: response code: %v\n", http.StatusBadRequest)
			fatalf("api calls exceeded expected ones: %s %s %s : req: %s",
				met, typ, name, r.URL.String())
			return
		}
		expectedCall := expectedCalls[0]
		if met != expectedCall[0] || typ != expectedCall[1] {
			w.WriteHeader(http.StatusBadRequest)
			logf("ServeHTTP: response code: %v\n", http.StatusBadRequest)
			fatalf("unexpected API call: expected: %v, got %v",
				expectedCall, [2]string{met, typ})
			return
		}
		fbss.bsExpectedCalls[name] = expectedCalls[1:]
	}

	if typ == "operations" {
		switch met {
		case http.MethodGet:
			opStatus := computeOperationStatusDone
			if ops, ok := fbss.bsOperationStatuses[name]; ok {
				if len(ops) > 0 {
					opStatus = ops[0]
					fbss.bsOperationStatuses[name] = ops[1:]
				}
			}
			logf("ServeHTTP: response code: %v\n", http.StatusOK)
			json.NewEncoder(w).Encode(compute.Operation{Status: opStatus})
			return
		default:
			w.WriteHeader(http.StatusBadRequest)
			logf("ServeHTTP: response code: %v\n", http.StatusBadRequest)
			return
		}
	}

	bs, ok := fbss.bss[name]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		logf("ServeHTTP: response code: %v\n", http.StatusNotFound)
		return
	}

	switch met {
	case http.MethodGet:
		if err := json.NewEncoder(w).Encode(bs); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			logf("ServeHTTP: response code: %v\n", http.StatusInternalServerError)
			fatalf("json encode failed: %v", err)
		}
		logf("ServeHTTP: response code: %v\n", http.StatusOK)
		return
	case http.MethodPatch:
		defer r.Body.Close()
		patchBody := compute.BackendService{}
		if err := json.NewDecoder(r.Body).Decode(&patchBody); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			logf("ServeHTTP: response code: %v\n", http.StatusBadRequest)
			fatalf("json decode failed: %v", err)
			return
		}

		var body strings.Builder
		json.NewEncoder(&body).Encode(patchBody)
		logf("ServeHTTP patch received: %+v\n%s\n", patchBody.Backends, body.String())
		bs.Backends = patchBody.Backends

		if err := json.NewEncoder(w).Encode(bs); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			logf("ServeHTTP: response code: %v\n", http.StatusInternalServerError)
			fatalf("json encode failed: %v", err)
			return
		}
		logf("ServeHTTP: response code: %v\n", http.StatusOK)

	default:
		w.WriteHeader(http.StatusBadRequest)
		logf("ServeHTTP: response code: %v\n", http.StatusInternalServerError)
		fatalf("unexpected %s api call: %s", met, r.URL.String())
	}
}

func newFakeBackendServiceServer(
	bServices map[string]*compute.BackendService,
	expectedCalls map[string][][2]string,
	bsOperations map[string][]string,
	t GinkgoTInterface,
) *fakeBackendServiceServer {
	var fbss = fakeBackendServiceServer{
		bss:                 bServices,
		bsExpectedCalls:     expectedCalls,
		bsOperationStatuses: bsOperations,
		t:                   t,
	}
	fbss.Server = httptest.NewServer(&fbss)
	return &fbss
}
