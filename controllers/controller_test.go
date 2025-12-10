package controllers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Run autoneg Controller", func() {

	ctx := context.Background()

	serviceKey := client.ObjectKey{
		Name:      "old-service",
		Namespace: "namespace",
	}

	Context("Create a service resource with autoneg annotations", func() {

		It("should succeed", func() {

			namespace := &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{
					Name: "namespace",
				},
			}

			err := k8sClient.Create(ctx, namespace)
			Expect(err).NotTo(HaveOccurred())

			annotations := make(map[string]string)
			annotations[negAnnotation] = "{\"exposed_ports\":{\"4242\":{}}}"
			annotations[autonegAnnotation] = "{\"backend_services\":{\"4242\":[{\"max_rate_per_endpoint\":4242}]}}"

			ports := make([]corev1.ServicePort, 1)
			ports[0] = corev1.ServicePort{
				Port:     4242,
				Protocol: corev1.ProtocolTCP,
			}

			service := &corev1.Service{
				ObjectMeta: v1.ObjectMeta{
					Name:        "old-service",
					Namespace:   "namespace",
					Annotations: annotations,
				},
				Spec: corev1.ServiceSpec{
					Ports: ports,
				},
			}

			err = k8sClient.Create(ctx, service)
			Expect(err).NotTo(HaveOccurred(), "failed to create service resource")

			createdService := &corev1.Service{}

			Eventually(func() string {
				err = k8sClient.Get(ctx, serviceKey, createdService)
				Expect(err).NotTo(HaveOccurred(), "failed to retrieve service resource")
				annos := createdService.Annotations
				autonegStatus := annos[autonegStatusAnnotation]
				return autonegStatus
			}, time.Second*5, time.Second).ShouldNot(BeEmpty())

			updatedAnnos := createdService.Annotations

			Expect(updatedAnnos[autonegStatusAnnotation]).To(Equal(
				"{\"backend_services\":{\"4242\":{\"namespace-old-service-4242-de64ba2d\":" +
					"{\"name\":\"namespace-old-service-4242-de64ba2d\",\"max_rate_per_endpoint\":4242}}}," +
					"\"network_endpoint_groups\":null,\"zones\":null}"))
			Expect(updatedAnnos[negStatusAnnotation]).To(BeEmpty())
		})

		Context("Reconciles periodically", func() {

			It("should reconcile", func() {
				timesReconciled := backendController.Counter
				time.Sleep(2 * time.Second)
				Expect(backendController.Counter-timesReconciled > 0).To(BeTrue(), "should have at least reconciled once.")
			})

		})

		Context("Remove the service", func() {

			It("should succeed", func() {
				createdService := &corev1.Service{}
				err := k8sClient.Get(ctx, serviceKey, createdService)
				Expect(err).NotTo(HaveOccurred(), "failed to retrieve service resource")

				err = k8sClient.Delete(ctx, createdService)
				Expect(err).NotTo(HaveOccurred(), "failed to delete service resource")

				Eventually(func() error {
					err = k8sClient.Get(ctx, serviceKey, &corev1.Service{})
					return err
				}, time.Second*5, time.Second).ShouldNot(BeNil())

				var e *errNotFound
				Expect(err).To(HaveOccurred())
				Expect(reflect.TypeOf(err).Kind()).To(Equal(reflect.TypeOf(e).Kind()))
			})

		})
	})
})

type M = map[string]any

func waitForAutonegStatusAnnotationChanged(ctx context.Context, c client.Client, obj client.Object, old string) {
	Eventually(func() bool {
		err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj)
		Expect(err).NotTo(HaveOccurred(), "failed to retrieve resource")
		return obj.GetAnnotations()[autonegStatusAnnotation] != old
	}, 15*time.Second, time.Second).Should(BeTrue())
}

var _ = Describe("Run autoneg Controller for balancing modes", func() {
	ctx := context.Background()
	nsName := "namespace-other"
	svcName := "old-service"
	port := int32(4242)
	portStr := fmt.Sprintf("%d", port)
	nameString := strings.Join([]string{nsName, svcName, portStr}, ";")
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(nameString)))[:8]
	// hash here is eb0436f4
	negName := fmt.Sprintf("%s-%s-%s-%s", nsName, svcName, portStr, hash)
	serviceKey := client.ObjectKey{
		Name:      svcName,
		Namespace: nsName,
	}
	bsName := fmt.Sprintf("%s-lb-be-%s",
		serviceKey.Namespace, serviceKey.Name)

	// initially there is a backend service with a backend with NEG,
	// but nothing configured by Autoneg
	as := AutonegStatus{
		NEGStatus: NEGStatus{
			NEGs:  map[string]string{portStr: negName},
			Zones: []string{"zone1"},
		},
	}
	negConf := NEGConfig{ExposedPorts: M{portStr: M{}}}
	var negAnnotVal, negStatusAnnotVal strings.Builder
	json.NewEncoder(&negAnnotVal).Encode(negConf)
	json.NewEncoder(&negStatusAnnotVal).Encode(as.NEGStatus)

	ab := as.Backend(bsName, portStr, getGroup(projectTestName, as.Zones[0], as.NEGs[portStr]))

	BeforeEach(func() {
		fakeServer.addBackendToBackendService(bsName, &ab)
		// the below cannot works with AlwaysReconcile
		// fakeServer.setBackendServicesExpectedCallsFor(bsName, [][2]string{
		// 	{"GET", "backendServices"}, {"PATCH", "backendServices"}, {"GET", "operations"}, {"GET", "operations"},
		// 	{"GET", "backendServices"}, {"PATCH", "backendServices"}, {"GET", "operations"}, {"GET", "operations"},
		// 	{"GET", "backendServices"}, {"PATCH", "backendServices"}, {"GET", "operations"}, {"GET", "operations"},
		// })
		// fakeServer.setBackendServicesOperationsFor(bsName, []string{
		// 	computeOperationStatusPending, computeOperationStatusDone,
		// 	computeOperationStatusPending, computeOperationStatusDone,
		// 	computeOperationStatusPending, computeOperationStatusDone,
		// })
	})

	Context("Create a service without autoneg annotations", func() {

		It("should succeed", func() {

			namespace := &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{
					Name: nsName,
				},
			}

			err := k8sClient.Create(ctx, namespace)
			Expect(err).NotTo(HaveOccurred())

			annotations := make(map[string]string)
			annotations[negAnnotation] = negAnnotVal.String()
			annotations[negStatusAnnotation] = negStatusAnnotVal.String()

			ports := make([]corev1.ServicePort, 1)
			ports[0] = corev1.ServicePort{
				Port:     port,
				Protocol: corev1.ProtocolTCP,
			}

			service := &corev1.Service{
				ObjectMeta: v1.ObjectMeta{
					Name:        svcName,
					Namespace:   nsName,
					Annotations: annotations,
				},
				Spec: corev1.ServiceSpec{
					Ports: ports,
				},
			}

			err = k8sClient.Create(ctx, service)
			Expect(err).NotTo(HaveOccurred(), "failed to create service resource")

			createdService := &corev1.Service{}
			Eventually(func() error {
				return k8sClient.Get(ctx, serviceKey, createdService)
			}, time.Second*5, time.Second).ShouldNot(HaveOccurred())
		})

		Context("Update Service with autoneg annotations for RATE balancing mode", func() {
			It("reacts on service patch", func() {
				service := &corev1.Service{}
				service.SetNamespace(serviceKey.Namespace)
				service.SetName(serviceKey.Name)
				var autonegAnnotVal strings.Builder
				autonegAnnot := M{"backend_services": M{
					portStr: []M{{"name": bsName, "max_rate_per_endpoint": port}},
				}}
				json.NewEncoder(&autonegAnnotVal).Encode(autonegAnnot)
				autonegAnnotationEscaped := "controller.autoneg.dev~1neg"
				patchOp := []struct {
					Op    string `json:"op"`
					Path  string `json:"path"`
					Value string `json:"value"`
				}{{
					Op:    "replace",
					Path:  "/metadata/annotations/" + autonegAnnotationEscaped,
					Value: autonegAnnotVal.String(),
				}}
				var patchBytes bytes.Buffer
				json.NewEncoder(&patchBytes).Encode(patchOp)

				Expect(k8sClient.Get(ctx, serviceKey, service)).ToNot(HaveOccurred())
				autonegStatusAnnotationValue := service.GetAnnotations()[autonegStatusAnnotation]
				Expect(k8sClient.Patch(ctx, service, client.RawPatch(types.JSONPatchType, patchBytes.Bytes()))).NotTo(HaveOccurred(), "failed to patch service resource")

				waitForAutonegStatusAnnotationChanged(ctx, k8sClient, service, autonegStatusAnnotationValue)

				annots := service.GetAnnotations()
				// Expect(annots[autonegStatusAnnotation]).ToNot(BeEmpty())
				expectedAutonegStatusAnnotVal := M{"backend_services": M{portStr: M{
					bsName: M{"name": bsName, "max_rate_per_endpoint": port}},
				}, "network_endpoint_groups": as.NEGs, "zones": as.Zones}
				var actualAutonegStatusAnnot, expectedAutonegStatusAnnot map[string]any
				var expectedAutonegStatusAnnotBuffer strings.Builder
				json.NewEncoder(&expectedAutonegStatusAnnotBuffer).Encode(expectedAutonegStatusAnnotVal)
				json.NewDecoder(strings.NewReader(annots[autonegStatusAnnotation])).Decode(&actualAutonegStatusAnnot)
				json.NewDecoder(strings.NewReader(expectedAutonegStatusAnnotBuffer.String())).Decode(&expectedAutonegStatusAnnot)
				Expect(actualAutonegStatusAnnot).To(Equal(expectedAutonegStatusAnnot))

				// the corresponding backend in backend service have to have RATE as balancing mode
				Expect(fakeServer.bss[bsName]).NotTo(BeNil())
				Expect(fakeServer.bss[bsName].Backends).NotTo(BeNil())
				Expect(len(fakeServer.bss[bsName].Backends)).To(Equal(1))
				Expect(fakeServer.bss[bsName].Backends[0].BalancingMode).To(Equal("RATE"))
			})
		})

		Context("Update Service with autoneg annotations for CONNECTION balancing mode", func() {
			It("reacts on service patch", func() {
				service := &corev1.Service{}
				service.SetNamespace(serviceKey.Namespace)
				service.SetName(serviceKey.Name)
				var autonegAnnotVal strings.Builder
				autonegAnnot := M{"backend_services": M{
					portStr: []M{{"name": bsName, "max_connections_per_endpoint": port}},
				}}
				json.NewEncoder(&autonegAnnotVal).Encode(autonegAnnot)
				autonegAnnotationEscaped := "controller.autoneg.dev~1neg"
				patchOp := []struct {
					Op    string `json:"op"`
					Path  string `json:"path"`
					Value string `json:"value"`
				}{{
					Op:    "replace",
					Path:  "/metadata/annotations/" + autonegAnnotationEscaped,
					Value: autonegAnnotVal.String(),
				}}
				var patchBytes bytes.Buffer
				json.NewEncoder(&patchBytes).Encode(patchOp)

				Expect(k8sClient.Get(ctx, serviceKey, service)).ToNot(HaveOccurred())
				autonegStatusAnnotationValue := service.GetAnnotations()[autonegStatusAnnotation]
				Expect(k8sClient.Patch(ctx, service, client.RawPatch(types.JSONPatchType, patchBytes.Bytes()))).NotTo(HaveOccurred(), "failed to patch service resource")

				waitForAutonegStatusAnnotationChanged(ctx, k8sClient, service, autonegStatusAnnotationValue)

				annots := service.GetAnnotations()
				// Expect(annots[autonegStatusAnnotation]).ToNot(BeEmpty())
				expectedAutonegStatusAnnotVal := M{"backend_services": M{portStr: M{
					bsName: M{"name": bsName, "max_connections_per_endpoint": 4242}},
				}, "network_endpoint_groups": as.NEGs, "zones": as.Zones}
				var actualAutonegStatusAnnot, expectedAutonegStatusAnnot map[string]any
				var expectedAutonegStatusAnnotBuffer strings.Builder
				json.NewEncoder(&expectedAutonegStatusAnnotBuffer).Encode(expectedAutonegStatusAnnotVal)
				json.NewDecoder(strings.NewReader(annots[autonegStatusAnnotation])).Decode(&actualAutonegStatusAnnot)
				json.NewDecoder(strings.NewReader(expectedAutonegStatusAnnotBuffer.String())).Decode(&expectedAutonegStatusAnnot)
				Expect(actualAutonegStatusAnnot).To(Equal(expectedAutonegStatusAnnot))

				// the corresponding backend in backend service have to have RATE as balancing mode
				Expect(fakeServer.bss[bsName]).NotTo(BeNil())
				Expect(fakeServer.bss[bsName].Backends).NotTo(BeNil())
				Expect(len(fakeServer.bss[bsName].Backends)).To(Equal(1))
				Expect(fakeServer.bss[bsName].Backends[0].BalancingMode).To(Equal("CONNECTION"))
			})
		})

		Context("Update Service with autoneg annotations for CUSTOM_METRICS balancing mode", func() {
			It("reacts on service patch", func() {
				service := &corev1.Service{}
				service.SetNamespace(serviceKey.Namespace)
				service.SetName(serviceKey.Name)
				var autonegAnnotVal strings.Builder
				autonegAnnot := M{"backend_services": M{
					portStr: []M{{"name": bsName, "custom_metrics": []M{
						{"name": "orca.named_metrics.cool", "max_utilization": 0.5},
					}}},
				}}
				json.NewEncoder(&autonegAnnotVal).Encode(autonegAnnot)
				autonegAnnotationEscaped := "controller.autoneg.dev~1neg"
				patchOp := []struct {
					Op    string `json:"op"`
					Path  string `json:"path"`
					Value string `json:"value"`
				}{{
					Op:    "replace",
					Path:  "/metadata/annotations/" + autonegAnnotationEscaped,
					Value: autonegAnnotVal.String(),
				}}
				var patchBytes bytes.Buffer
				json.NewEncoder(&patchBytes).Encode(patchOp)

				Expect(k8sClient.Get(ctx, serviceKey, service)).ToNot(HaveOccurred())
				autonegStatusAnnotationValue := service.GetAnnotations()[autonegStatusAnnotation]
				Expect(k8sClient.Patch(ctx, service, client.RawPatch(types.JSONPatchType, patchBytes.Bytes()))).NotTo(HaveOccurred(), "failed to patch service resource")

				waitForAutonegStatusAnnotationChanged(ctx, k8sClient, service, autonegStatusAnnotationValue)

				annots := service.GetAnnotations()
				expectedAutonegStatusAnnotVal := M{"backend_services": M{portStr: M{
					bsName: M{"name": bsName, "custom_metrics": []M{
						{"name": "orca.named_metrics.cool", "max_utilization": 0.5},
					}}},
				}, "network_endpoint_groups": as.NEGs, "zones": as.Zones}
				var actualAutonegStatusAnnot, expectedAutonegStatusAnnot map[string]any
				var expectedAutonegStatusAnnotBuffer strings.Builder
				json.NewEncoder(&expectedAutonegStatusAnnotBuffer).Encode(expectedAutonegStatusAnnotVal)
				json.NewDecoder(strings.NewReader(annots[autonegStatusAnnotation])).Decode(&actualAutonegStatusAnnot)
				json.NewDecoder(strings.NewReader(expectedAutonegStatusAnnotBuffer.String())).Decode(&expectedAutonegStatusAnnot)
				Expect(actualAutonegStatusAnnot).To(Equal(expectedAutonegStatusAnnot))

				// the corresponding backend in backend service have to have CUSTOM_METRICS as balancing mode
				Expect(fakeServer.bss[bsName]).NotTo(BeNil())
				Expect(fakeServer.bss[bsName].Backends).NotTo(BeNil())
				Expect(len(fakeServer.bss[bsName].Backends)).To(Equal(1))
				Expect(fakeServer.bss[bsName].Backends[0].BalancingMode).To(Equal("CUSTOM_METRICS"))
			})
		})

		Context("Remove the service", func() {

			It("should succeed", func() {
				createdService := &corev1.Service{}
				err := k8sClient.Get(ctx, serviceKey, createdService)
				Expect(err).NotTo(HaveOccurred(), "failed to retrieve service resource")

				err = k8sClient.Delete(ctx, createdService)
				Expect(err).NotTo(HaveOccurred(), "failed to delete service resource")

				Eventually(func() error {
					err = k8sClient.Get(ctx, serviceKey, &corev1.Service{})
					return err
				}, time.Second*5, time.Second).ShouldNot(BeNil())

				var e *errNotFound
				Expect(err).To(HaveOccurred())
				Expect(reflect.TypeOf(err).Kind()).To(Equal(reflect.TypeOf(e).Kind()))
			})

		})
	})
})
