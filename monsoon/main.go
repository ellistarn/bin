package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/Pallinder/go-randomdata"
	"github.com/samber/lo"
	"github.com/samber/lo/parallel"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// User Inputs
const (
	NumPods = 5555
)

// Tunables
const (
	PodsPerDeployment       = 100
	DeploymentsPerNamespace = 10
	PodsPerNamespace        = PodsPerDeployment * DeploymentsPerNamespace
)

// Cleanup w/ `k delete namespaces -l app.kubernetes.io/managed-by=monsoon`
func main() {
	lo.Must0(execute())
}

func execute() error {
	ctx := context.Background()

	config, err := controllerruntime.GetConfig()
	if err != nil {
		return err
	}
	config.QPS = 1000
	config.Burst = 1000
	kubeClient, err := client.New(config, client.Options{})
	if err != nil {
		return err
	}

	deployments := DeploymentsFor(NumPods)
	namespaces := NamespacesFor(deployments)

	fmt.Printf("creating %d namespaces\n", len(namespaces))
	var errs []error
	errs = parallel.Map(namespaces, func(namespace *corev1.Namespace, _ int) error {
		err := kubeClient.Create(ctx, namespace)
		fmt.Printf("created namespace %q\n", namespace.Name)
		return err
	})
	if err := errors.Join(errs...); err != nil {
		return err
	}

	fmt.Printf("creating %d deployments\n", len(deployments))
	errs = parallel.Map(deployments, func(deployment *v1.Deployment, _ int) error {
		err := kubeClient.Create(ctx, deployment)
		fmt.Printf("created deployment %q with %d replicas\n", client.ObjectKeyFromObject(deployment).String(), *deployment.Spec.Replicas)
		return err
	})
	if err := errors.Join(errs...); err != nil {
		return err
	}
	return nil
}

func NamespacesFor(deployments []*v1.Deployment) []*corev1.Namespace {
	return lo.Map(
		lo.Uniq(lo.Map(deployments, func(deployment *v1.Deployment, _ int) string { return deployment.Namespace })),
		func(namespace string, _ int) *corev1.Namespace {
			return &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   namespace,
					Labels: labels(),
				},
			}
		})
}

func DeploymentsFor(podCount int) []*v1.Deployment {
	deployments := []*v1.Deployment{}
	for _, namespace := range RandomStrings(podCount / PodsPerNamespace) {
		for _, name := range RandomStrings(DeploymentsPerNamespace) {
			deployments = append(deployments, Deployment(namespace, name, PodsPerDeployment))
		}
	}

	remainderNamespacePods := math.Mod(float64(podCount), PodsPerNamespace)

	// Misshapen final namespace
	remainderNamespaceName := RandomStrings(1)[0]
	for _, name := range RandomStrings(int(remainderNamespacePods) / PodsPerDeployment) {
		deployments = append(deployments, Deployment(remainderNamespaceName, name, PodsPerDeployment))
	}
	// Misshapen final deployment
	remainderDeploymentName := RandomStrings(1)[0]
	if remainder := math.Mod(remainderNamespacePods, PodsPerDeployment); remainder > 0 {
		deployments = append(deployments, Deployment(remainderDeploymentName, RandomStrings(1)[0], int32(remainder)))
	}
	return deployments
}

func Deployment(namespace string, name string, replicas int32) *v1.Deployment {
	return &v1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels(),
		},
		Spec: v1.DeploymentSpec{
			Replicas: lo.ToPtr(replicas),
			Selector: &metav1.LabelSelector{MatchLabels: labels()},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels()},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "monsoon",
							Image: "public.ecr.aws/eks-distro/kubernetes/pause:3.2",
						},
					},
				},
			},
		},
	}
}

func RandomStrings(count int) []string {
	return lo.Map(lo.Range(count), func(i int, _ int) string { return fmt.Sprintf("%d-%s", i+1, strings.ToLower(randomdata.SillyName())) })
}

func labels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "monsoon",
	}
}
