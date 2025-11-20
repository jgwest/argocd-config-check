package main

import (
	"context"
	"fmt"
	"os"

	"github.com/argoproj-labs/argocd-operator/api/v1beta1"
)

func main() {

	var abstractK8sClient AbstractK8sClient

	if len(os.Args) == 1 {
		k8sClientFromSystem, _ := GetK8sClient()

		abstractK8sClient = &traditionalK8sClient{
			client: k8sClientFromSystem,
		}
		outputStatusMessage("Using default K8s client configuration from '.kube/config'")

	} else if len(os.Args) == 2 {

		pathToOMCDirectory := os.Args[1]

		var err error
		abstractK8sClient, err = newOMCClient(pathToOMCDirectory)
		if err != nil {
			FailWithError("unable to retrieve OMC client data from '"+pathToOMCDirectory+"'", err)
		}
	} else {
		outputStatusMessage("Unexpected number of arguments. Valid parameters are:")
		outputStatusMessage("")
		outputStatusMessage("Validate Argo CD configuration using system K8s configuration (`~/.kube/config`)")
		outputStatusMessage("- argocd-config-check")
		outputStatusMessage("")
		outputStatusMessage("Validate Argo CD configuration using must-gather output")
		outputStatusMessage("- argocd-config-check (path to must-gather directory for omc)")
		outputStatusMessage("")
		FailWithError("Unexpected number of arguments.", nil)
	}

	ctx := context.Background()

	runChecks(ctx, abstractK8sClient)

}

func runChecks(ctx context.Context, k8sClient AbstractK8sClient) {

	var argoCDList v1beta1.ArgoCDList
	if err := k8sClient.ListFromAllNamespaces(ctx, &argoCDList); err != nil {
		FailWithError("unable to list ArgoCDs", err)
	}

	for _, argoCD := range argoCDList.Items {
		issues := checkIndividualArgoCDCR(argoCD)

		if len(*issues) == 0 {
			continue
		}

		outputStatusMessage("Namespace " + argoCD.Namespace + " -> ArgoCD " + argoCD.Name + ":")

		for _, issue := range *issues {
			reportIssue(issue)
			fmt.Println()
		}

	}
}

func outputStatusMessage(str string) {
	fmt.Println("*", str)
}

func reportIssue(i issue) {
	fmt.Println("[" + i.field + "]")
	fmt.Println("-", i.message)
}

type issue struct {
	field   string
	message string
}

func checkIndividualArgoCDCR(argoCD v1beta1.ArgoCD) *[]issue {

	issues := []issue{}

	checkArgoCDCRForDeprecatedFields(argoCD, &issues)

	return &issues

}

func checkArgoCDCRForDeprecatedFields(argoCD v1beta1.ArgoCD, issues *[]issue) {

	if len(argoCD.Spec.ConfigManagementPlugins) > 0 {
		*issues = append(*issues, issue{
			field:   ".spec.configMapPlugins",
			message: "ConfigManagementPlugins field is no longer supported. Argo CD now requires plugins to be defined as sidecar containers of repo server component. See '.spec.repo.sidecarContainers'. ConfigManagementPlugins was previously used to specify additional config management plugins.",
		})
	}

}
