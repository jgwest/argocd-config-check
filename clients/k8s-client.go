package clients

import (
	"context"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	argocdv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"

	osappsv1 "github.com/openshift/api/apps/v1"
	olmv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"

	argov1alpha1api "github.com/argoproj-labs/argocd-operator/api/v1alpha1"
	consolev1 "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	securityv1 "github.com/openshift/api/security/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	apps "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	crdv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func SystemK8sClient() (AbstractK8sClient, error) {
	k8sClientFromSystem, _, err := getSystemK8sClient()
	if err != nil {
		return nil, err
	}

	return &traditionalK8sClient{
		client: k8sClientFromSystem,
	}, nil
}

type traditionalK8sClient struct {
	client client.Client
}

func (t *traditionalK8sClient) ListFromAllNamespaces(ctx context.Context, list client.ObjectList) error {
	return t.client.List(ctx, list)
}

func (t *traditionalK8sClient) ListFromSingleNamespace(ctx context.Context, list client.ObjectList, namespace string) error {
	return t.client.List(ctx, list, client.InNamespace(namespace))
}

func getSystemK8sClient() (client.Client, *runtime.Scheme, error) {
	config, err := getSystemKubeConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("unable to get k8s config: %v", err)
	}

	k8sClient, scheme, err := getK8sClient(config)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to get k8s client: %v", err)
	}

	return k8sClient, scheme, nil
}

// getK8sClient returns a controller-runtime Client for accessing K8s API resources used by the controller.
func getK8sClient(config *rest.Config) (client.Client, *runtime.Scheme, error) {

	scheme := runtime.NewScheme()

	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := apps.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}
	if err := rbacv1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := admissionv1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := monitoringv1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := crdv1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := argov1beta1api.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := argocdv1alpha1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := olmv1alpha1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := routev1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := osappsv1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := consolev1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := argov1alpha1api.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := securityv1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := networkingv1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := autoscalingv2.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := batchv1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return nil, nil, err
	}

	return k8sClient, scheme, nil

}

// Retrieve the system-level Kubernetes config (e.g. ~/.kube/config or service account config from volume)
func getSystemKubeConfig() (*rest.Config, error) {

	overrides := clientcmd.ConfigOverrides{}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	clientConfig := clientcmd.NewInteractiveDeferredLoadingClientConfig(loadingRules, &overrides, os.Stdin)

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}
	return restConfig, nil
}
