package controllers

import (
	"context"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

var _ = Describe("Run autoneg Controller (for custom metrics)", func() {

	ctx := context.Background()
	nsName := "namespace-other"
	svcName := "old-service"
	// portStr := "4242"
	// nameString := strings.Join([]string{nsName, svcName, portStr}, ";")
	// hash := fmt.Sprintf("%x", sha256.Sum256([]byte(nameString)))[:8]
	// hash here is eb0436f4
	serviceKey := client.ObjectKey{
		Name:      svcName,
		Namespace: nsName,
	}

	Context("Create a service resource with autoneg annotations", func() {

		It("should succeed", func() {

			namespace := &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{
					Name: nsName,
				},
			}

			err := k8sClient.Create(ctx, namespace)
			Expect(err).NotTo(HaveOccurred())

			annotations := make(map[string]string)
			annotations[negAnnotation] = "{\"exposed_ports\":{\"4242\":{}}}"
			annotations[autonegAnnotation] = "{\"backend_services\":{\"4242\":[{\"custom_metrics\":[{\"name\":\"orca.named_metrics.super-cool\", \"max_utilization\": 0.8}]}]}}"

			ports := make([]corev1.ServicePort, 1)
			ports[0] = corev1.ServicePort{
				Port:     4242,
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

			Eventually(func() string {
				err = k8sClient.Get(ctx, serviceKey, createdService)
				Expect(err).NotTo(HaveOccurred(), "failed to retrieve service resource")
				annos := createdService.Annotations
				autonegStatus := annos[autonegStatusAnnotation]
				return autonegStatus
			}, time.Second*5, time.Second).ShouldNot(BeEmpty())

			updatedAnnos := createdService.Annotations

			Expect(updatedAnnos[autonegStatusAnnotation]).To(Equal(
				"{\"backend_services\":{\"4242\":{\"namespace-other-old-service-4242-eb0436f4\":" +
					"{\"name\":\"namespace-other-old-service-4242-eb0436f4\",\"custom_metrics\":[{\"max_utilization\":0.8,\"name\":\"orca.named_metrics.super-cool\"}]}}}," +
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
