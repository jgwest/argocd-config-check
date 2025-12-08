package main

import (
	"context"
	"fmt"
	"os"

	"github.com/argoproj-labs/argocd-operator/api/v1beta1"
	"github.com/jgwest/argocd-config-check/clients"
)

func main() {

	var abstractK8sClient clients.AbstractK8sClient

	// TODO: Attempt to determine the operator version, and output it. Then output reference to openshift gitops lifecycle list.

	if len(os.Args) == 1 {
		var err error
		abstractK8sClient, err = clients.SystemK8sClient()
		if err != nil {
			failWithError("unable to retrieve system K8s client configuration", err)
		}
		outputStatusMessage("Using default K8s client configuration from '.kube/config'")

	} else if len(os.Args) == 2 {
		var err error
		pathToOMCDirectory := os.Args[1]
		abstractK8sClient, err = clients.OMCClient(pathToOMCDirectory)
		if err != nil {
			failWithError("unable to retrieve OMC client data from '"+pathToOMCDirectory+"'", err)
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
		failWithError("Unexpected number of arguments.", nil)
	}

	ctx := context.Background()

	runChecks(ctx, abstractK8sClient)

}

func runChecks(ctx context.Context, k8sClient clients.AbstractK8sClient) {

	var argoCDList v1beta1.ArgoCDList
	if err := k8sClient.ListFromAllNamespaces(ctx, &argoCDList); err != nil {
		failWithError("unable to list ArgoCDs", err)
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
	checkArgoCDCRForUnsupportedCustomImages(argoCD, &issues)

	return &issues

}

// checkArgoCDCRForUnsupportedCustomImages identifies the use of custom container images for components where that is not supported.
func checkArgoCDCRForUnsupportedCustomImages(argoCD v1beta1.ArgoCD, issues *[]issue) {

	if argoCD.Spec.ApplicationSet != nil && len(argoCD.Spec.ApplicationSet.Image) > 0 {

		*issues = append(*issues, issue{
			field:   ".spec.applicationSet.image",
			message: "The image field is used to provide custom container images for Argo CD components. However, specifying custom images for essential Argo CD components is not supported.",
		})

	}

	if argoCD.Spec.SSO != nil && argoCD.Spec.SSO.Dex != nil && len(argoCD.Spec.SSO.Dex.Image) > 0 {

		*issues = append(*issues, issue{
			field:   ".spec.sso.dex.image",
			message: "The image field is used to provide custom container images for Argo CD components. However, specifying custom images for essential Argo CD components is not supported.",
		})

	}

	if len(argoCD.Spec.HA.RedisProxyImage) > 0 {

		*issues = append(*issues, issue{
			field:   ".spec.ha.redisProxyImage",
			message: "The image field is used to provide custom container images for Argo CD components. However, specifying custom images for essential Argo CD components is not supported.",
		})

	}

	if argoCD.Spec.ArgoCDAgent != nil {

		if argoCD.Spec.ArgoCDAgent.Agent != nil && len(argoCD.Spec.ArgoCDAgent.Agent.Image) > 0 {
			*issues = append(*issues, issue{
				field:   ".spec.argoCDAgent.agent.image",
				message: "The image field is used to provide custom container images for Argo CD components. However, specifying custom images for essential Argo CD components is not supported.",
			})
		}

		if argoCD.Spec.ArgoCDAgent.Principal != nil && len(argoCD.Spec.ArgoCDAgent.Principal.Image) > 0 {

			*issues = append(*issues, issue{
				field:   ".spec.argoCDAgent.principal.image",
				message: "The image field is used to provide custom container images for Argo CD components. However, specifying custom images for essential Argo CD components is not supported.",
			})

		}

	}

	if len(argoCD.Spec.Notifications.Image) > 0 {
		*issues = append(*issues, issue{
			field:   ".spec.notifications.image",
			message: "The image field is used to provide custom container images for Argo CD components. However, specifying custom images for essential Argo CD components is not supported.",
		})
	}

	if len(argoCD.Spec.Redis.Image) > 0 {
		*issues = append(*issues, issue{
			field:   ".spec.redis.image",
			message: "The image field is used to provide custom container images for Argo CD components. However, specifying custom images for essential Argo CD components is not supported.",
		})
	}

	if len(argoCD.Spec.Repo.Image) > 0 {
		*issues = append(*issues, issue{
			field:   ".spec.repo.image",
			message: "The image field is used to provide custom container images for Argo CD components. However, specifying custom images for essential Argo CD components is not supported.",
		})
	}

	if len(argoCD.Spec.Image) > 0 {
		*issues = append(*issues, issue{
			field:   ".spec.image",
			message: "The image field is used to provide custom container images for Argo CD components. However, specifying custom images for essential Argo CD components is not supported.",
		})
	}

}

// checkArgoCDCRForDeprecatedFields identifies fields that are deprecated and no longer supported by ArgoCD operator.
func checkArgoCDCRForDeprecatedFields(argoCD v1beta1.ArgoCD, issues *[]issue) {

	if len(argoCD.Spec.ConfigManagementPlugins) > 0 {
		*issues = append(*issues, issue{
			field:   ".spec.configMapPlugins",
			message: "ConfigManagementPlugins field is no longer supported. Argo CD now requires plugins to be defined as sidecar containers of repo server component. See '.spec.repo.sidecarContainers'. ConfigManagementPlugins was previously used to specify additional config management plugins.",
		})
	}

	if argoCD.Spec.Grafana.Enabled {
		*issues = append(*issues, issue{
			field:   ".spec.grafana",
			message: "grafana field is deprecated from ArgoCD CR: this field will be ignored by operator, and any remaining Grafana resources will be removed.",
		})
	}

	if len(argoCD.Spec.InitialRepositories) > 0 {
		*issues = append(*issues, issue{
			field:   ".spec.initialRepositories",
			message: "initialRepositories field is deprecated from ArgoCD CR. The field will be ignored by operator.",
		})
	}

	if len(argoCD.Spec.RepositoryCredentials) > 0 {
		*issues = append(*issues, issue{
			field:   ".spec.repositoryCredentials",
			message: "repositoryCredentials field is deprecated from ArgoCD CR. The field will be ignored by operator.",
		})
	}

	if argoCD.Spec.SSO != nil && argoCD.Spec.SSO.Keycloak != nil {
		*issues = append(*issues, issue{
			field:   ".spec.sso.keycloak",
			message: "keycloak field is no longer supported. ArgoCD operator will no longer create and manage a keycloak instance on the users behalf. Users may instead manage their own keycloak instance (using e.g. keycloak operator) and configure Argo CD to use it.",
		})
	}

}
