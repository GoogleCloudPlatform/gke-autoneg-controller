package controllers

import (
	"context"
	"log"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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

			err := k8sClient.Create(ctx, service)
			Expect(err).NotTo(HaveOccurred(), "failed to create service resource")

			time.Sleep(time.Second * 5)

			createdService := &corev1.Service{}
			err = k8sClient.Get(ctx, serviceKey, createdService)
			Expect(err).NotTo(HaveOccurred(), "failed to retrieve service resource")

			updatedAnnos := createdService.Annotations
			log.Printf("%v", updatedAnnos)
			Expect(updatedAnnos[autonegStatusAnnotation]).To(Equal(
				"{\"backend_services\":{\"4242\":{\"namespace-old-service-4242-de64ba2d\":" +
					"{\"name\":\"namespace-old-service-4242-de64ba2d\",\"max_rate_per_endpoint\":4242}}}," +
					"\"network_endpoint_groups\":null,\"zones\":null}"))
			Expect(updatedAnnos[negStatusAnnotation]).To(BeEmpty())
		})

		Context("Remove the service", func() {

			It("should succeed", func() {
				createdService := &corev1.Service{}
				err := k8sClient.Get(ctx, serviceKey, createdService)
				Expect(err).NotTo(HaveOccurred(), "failed to retrieve service resource")

				err = k8sClient.Delete(ctx, createdService)
				Expect(err).NotTo(HaveOccurred(), "failed to delete service resource")

				time.Sleep(time.Second * 5)

				err = k8sClient.Get(ctx, serviceKey, &corev1.Service{})

				var e *errNotFound
				Expect(err).To(HaveOccurred())
				Expect(reflect.TypeOf(err).Kind()).To(Equal(reflect.TypeOf(e).Kind()))
			})

		})
	})
})

func getResourceFunc(ctx context.Context, key client.ObjectKey, obj runtime.Object) func() error {
	return func() error {
		return k8sClient.Get(ctx, key, obj)
	}
}

func getDeploymentReplicasFunc(ctx context.Context, key client.ObjectKey) func() int32 {
	return func() int32 {
		depl := &apps.Deployment{}
		err := k8sClient.Get(ctx, key, depl)
		Expect(err).NotTo(HaveOccurred(), "failed to get Deployment resource")

		return *depl.Spec.Replicas
	}
}
