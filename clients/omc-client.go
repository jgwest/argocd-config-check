package clients

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// omcClient is a wrapper for the OMC CLI tool (https://github.com/gmeghnag/omc). It is used to read must-gathers from K8s (OpenShift) .
type omcClient struct {
	omcPath string
}

func OMCClient(path string) (*omcClient, error) {

	cmd := exec.Command("omc", "use", path)
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to run 'omc use %s': %w", path, err)
	}

	return &omcClient{
		omcPath: path,
	}, nil

}

func (o *omcClient) ListFromAllNamespaces(ctx context.Context, list client.ObjectList) error {

	typeFromList, err := convertObjectListToOMCType(list)
	if err != nil {
		return fmt.Errorf("unable to convert objectListToOMCType: %v", err)
	}

	cmd := exec.Command("omc", "get", typeFromList, "-A", "-o", "yaml")
	outBytes, err := cmd.CombinedOutput()
	k8sResourceListYAML := (string)(outBytes)

	// omc returns yaml EXCEPT when (e.g.) this error occurs. Note that when this error occurs, error code from omc is 0.
	if strings.HasPrefix(k8sResourceListYAML, "No resources ") && strings.HasSuffix(strings.TrimSpace(k8sResourceListYAML), "found.") {
		return nil
	}

	if err != nil {
		return fmt.Errorf("Output from OMC: %s\nUnable to retrieve '%s' from all namespaces: %v", k8sResourceListYAML, typeFromList, err)
	}

	if err := yaml.Unmarshal([]byte(k8sResourceListYAML), list); err != nil {
		return fmt.Errorf("Output from OMC: %s\nFailed to unmarshal YAML to %T: %w", k8sResourceListYAML, list, err)
	}

	return nil
}

func (o *omcClient) ListFromSingleNamespace(ctx context.Context, list client.ObjectList, namespace string) error {
	typeFromList, err := convertObjectListToOMCType(list)
	if err != nil {
		return err
	}

	cmd := exec.Command("omc", "get", typeFromList, "-n", namespace, "-o", "yaml")
	outBytes, err := cmd.CombinedOutput()
	k8sResourceListYAML := (string)(outBytes)

	if err != nil {
		return fmt.Errorf("Output from OMC: %s\nUnable to retrieve '%s' from all namespaces: %v", k8sResourceListYAML, typeFromList, err)
	}

	if err := yaml.Unmarshal([]byte(k8sResourceListYAML), list); err != nil {
		return fmt.Errorf("Output from OMC: %s\nFailed to unmarshal YAML to %T: %w", k8sResourceListYAML, list, err)
	}

	return nil
}

func (o *omcClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object) error {
	typeFromObj, err := convertObjectToOMCType(obj)
	if err != nil {
		return err
	}

	cmd := exec.Command("omc", "get", typeFromObj, key.Name, "-n", key.Namespace, "-o", "yaml")
	outBytes, err := cmd.CombinedOutput()
	k8sResourceYAML := (string)(outBytes)

	if err != nil {
		return fmt.Errorf("Output from OMC: %s\nUnable to retrieve '%s/%s' from namespace '%s': %v", k8sResourceYAML, typeFromObj, key.Name, key.Namespace, err)
	}

	if err := yaml.Unmarshal([]byte(k8sResourceYAML), obj); err != nil {
		return fmt.Errorf("Output from OMC: %s\nFailed to unmarshal YAML to %T: %w", k8sResourceYAML, obj, err)
	}

	return nil
}

func (o *omcClient) IncompleteControlPlaneData() bool {
	return true
}

func convertObjectListToOMCType(list client.ObjectList) (string, error) {
	listType := fmt.Sprintf("%T", list)
	switch listType {
	case "*v1beta1.ArgoCDList":
		return "argocds", nil
	case "*v1alpha1.SubscriptionList":
		return "subscriptions", nil
	case "*v1alpha1.ClusterServiceVersion":
		return "clusterserviceversions", nil
	case "*v1.NamespaceList":
		return "namespaces", nil

	default:
		return "", fmt.Errorf("unrecognized type: %s", listType)
	}
}

func convertObjectToOMCType(obj client.Object) (string, error) {
	objType := fmt.Sprintf("%T", obj)
	switch objType {
	case "*v1beta1.ArgoCD":
		return "argocd", nil
	case "*v1alpha1.SubscriptionList":
		return "subscriptions", nil
	case "*v1alpha1.ClusterServiceVersion":
		return "clusterserviceversions", nil
	case "*v1.Namespace":
		return "namespaces", nil
	default:
		return "", fmt.Errorf("unrecognized type: %s", objType)
	}
}
