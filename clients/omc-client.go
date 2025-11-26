package clients

import (
	"context"
	"fmt"
	"os/exec"

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
		return err
	}

	cmd := exec.Command("omc", "get", typeFromList, "-A", "-o", "yaml")
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

func convertObjectListToOMCType(list client.ObjectList) (string, error) {
	listType := fmt.Sprintf("%T", list)
	switch listType {
	case "*v1beta1.ArgoCDList":
		return "argocds", nil
	default:
		return "", fmt.Errorf("unrecognized type: %s", listType)
	}

}
