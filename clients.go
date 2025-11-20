package main

import (
	"context"
	"fmt"
	"os/exec"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

type AbstractK8sClient interface {
	ListFromAllNamespaces(ctx context.Context, list client.ObjectList) error
	ListFromSingleNamespace(ctx context.Context, list client.ObjectList, namespace string) error
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

// omcClient is a wrapper for the OMC CLI tool (https://github.com/gmeghnag/omc). It is used to read must-gathers from K8s (OpenShift) .
type omcClient struct {
	omcPath string
}

func newOMCClient(path string) (*omcClient, error) {

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
		outputStatusMessage("Output from OMC: " + k8sResourceListYAML)
		FailWithError("unable to retrieve "+typeFromList+" from all namespaces", err)
	}

	if err := yaml.Unmarshal([]byte(k8sResourceListYAML), list); err != nil {
		return fmt.Errorf("failed to unmarshal YAML to %T: %w", list, err)
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
		outputStatusMessage("Output from OMC: " + k8sResourceListYAML)
		FailWithError("unable to retrieve "+typeFromList+" from all namespaces", err)
	}

	if err := yaml.Unmarshal([]byte(k8sResourceListYAML), list); err != nil {
		return fmt.Errorf("failed to unmarshal YAML to %T: %w", list, err)
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
