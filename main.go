package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/argoproj-labs/argocd-operator/api/v1beta1"
	"github.com/jgwest/argocd-config-check/clients"
	olmv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// clusterInformation contains data extracted from operator/cluster configuration that may be useful for subsequent logic
type clusterInformation struct {
	operatorVersion   string
	operatorInstallNS string

	// from Subscription 'ARGOCD_CLUSTER_CONFIG_NAMESPACES' env
	clusterScopedNamespaces []string
}

type LogLevel string

const (
	// LogLevel_Fatal should be used if subsequent logic after that point is no longer guaranteed to be accurate (e.g. broken invariant). An example of a fatal case would be if there exist multiple gitops Subscriptions objects (with different versions) on the cluster.
	LogLevel_Fatal LogLevel = "Fatal"

	// LogLevel_Error should be used in cases where there is a high chance of this being an incorrect configuration
	LogLevel_Error LogLevel = "Error"

	// LogLevel_Warn should be used in cases where there is a mild/moderate chance of this being an incorrect configuration.
	LogLevel_Warn LogLevel = "Warn"
)

type entry struct {
	level   LogLevel
	message string
}

func (e entry) string() string {
	return fmt.Sprintf("[%s] %s", e.level, e.message)
}

func outputEntryList(entries []entry) {
	for _, entry := range entries {
		outputStatusMessage(entry.string())
	}
}

func entryListContainsFatal(entries []entry) bool {
	if entries == nil {
		return false
	}

	for _, entry := range entries {
		if entry.level == LogLevel_Fatal {
			return true
		}
	}

	return false
}

func acquireInstallConfigurationData(ctx context.Context, k8sClient clients.AbstractK8sClient) (clusterInformation, []entry) {

	var resClusterInformation clusterInformation
	resEntries := []entry{}

	// Locate OpenShift GitOps subscription

	var subscriptionList olmv1alpha1.SubscriptionList
	if err := k8sClient.ListFromAllNamespaces(ctx, &subscriptionList); err != nil {

		if k8sClient.IncompleteControlPlaneData() {
			resEntries = append(resEntries, entry{
				level:   LogLevel_Warn,
				message: "Unable to locate operator install Subscription. BUT, this may be expected if because the cluster data is incomplete (for example, if using must-gather, the must-gather may not contain full cluster output of all relevant namespaces). Error: " + err.Error(),
			})
		} else {
			resEntries = append(resEntries, entry{
				level:   LogLevel_Error,
				message: "Unable to locate operator install Subscription in any namespace. Error: " + err.Error(),
			})
		}

		return resClusterInformation, resEntries
	}

	var gitopsSubscription *olmv1alpha1.Subscription
	for idx := range subscriptionList.Items {

		sub := subscriptionList.Items[idx]

		if sub.Spec == nil { // TECHNICALLY this field is nullable
			continue
		}

		if sub.Spec.Package != "openshift-gitops-operator" {
			continue
		}

		if gitopsSubscription != nil { // If we already found a gitops subscription in a previous iteration of the loop. This REALLY shouldn't happen.
			resEntries = append(resEntries, entry{
				level:   LogLevel_Fatal,
				message: fmt.Sprintf("unexpected number of gitops subscriptions found: one in '%s' and one in '%s'", gitopsSubscription.Namespace+"/"+gitopsSubscription.Name, sub.Namespace+"/"+sub.Name),
			})
			return resClusterInformation, resEntries
		}

		gitopsSubscription = &sub
	}

	if gitopsSubscription == nil {
		if k8sClient.IncompleteControlPlaneData() {
			resEntries = append(resEntries, entry{
				level:   LogLevel_Warn,
				message: "Subscription could not be located, but this may be because cluster data is incomplete (for example, namespace was not included in what was exported to must-gather)",
			})
		} else {
			resEntries = append(resEntries, entry{
				level:   LogLevel_Error,
				message: "Subscription could not be located",
			})
		}

		return resClusterInformation, resEntries
	}

	resClusterInformation.operatorInstallNS = gitopsSubscription.Namespace

	if gitopsSubscription.Namespace != "openshift-gitops-operator" {
		resEntries = append(resEntries, entry{
			level:   LogLevel_Warn, // Warn and continue
			message: "operator was installed into an unexpected namespace '" + gitopsSubscription.Namespace + "'. The default is 'openshift-gitops-operator'",
		})
	}

	// Look at Subscription values

	// Example Subscription:
	//   spec:
	//     channel: latest
	//     config:
	//       env:
	//       - name: ARGOCD_CLUSTER_CONFIG_NAMESPACES
	//         value: openshift-gitops,argocd-prod,argod-staging
	//     installPlanApproval: Manual
	//     name: openshift-gitops-operator
	//     source: redhat-operators
	//     sourceNamespace: openshift-marketplace
	//   status:
	//     conditions:
	//     currentCSV: openshift-gitops-operator.v1.19.0
	//     installedCSV: openshift-gitops-operator.v1.19.0

	currentCSV := gitopsSubscription.Status.CurrentCSV
	installedCSV := gitopsSubscription.Status.InstalledCSV

	if installedCSV != currentCSV {
		resEntries = append(resEntries, entry{
			level:   LogLevel_Error, // Error and return
			message: "the '.status.currentCSV' field of operator != '.status.installedCSV' of operator, indicating installation may be in progress or stalled.",
		})
		return resClusterInformation, resEntries
	}

	// Parse ARGOCD_CLUSTER_CONFIG_NAMESPACES env var from Subscription config
	if gitopsSubscription.Spec.Config != nil {

		clusterConfigNamespacesEnvVarCount := 0 // This should be either 0 or 1

		for _, envVar := range gitopsSubscription.Spec.Config.Env {
			if envVar.Name == "ARGOCD_CLUSTER_CONFIG_NAMESPACES" {
				// Split by comma and trim whitespace from each namespace
				rawNamespaceList := strings.SplitSeq(envVar.Value, ",")
				for ns := range rawNamespaceList {
					trimmed := strings.TrimSpace(ns)
					if trimmed != "" {
						resClusterInformation.clusterScopedNamespaces = append(resClusterInformation.clusterScopedNamespaces, trimmed)
					}
				}
				clusterConfigNamespacesEnvVarCount++
			}
		}

		if clusterConfigNamespacesEnvVarCount > 1 {
			resEntries = append(resEntries, entry{
				level:   LogLevel_Fatal,
				message: "multiple ARGOCD_CLUSTER_CONFIG_NAMESPACES env entries were found in Subscription's .spec.config.env, which is not valid",
			})
			return resClusterInformation, resEntries
		}

	} else {
		// TODO: Handle the DISABLE DEFAULT env var

		resClusterInformation.clusterScopedNamespaces = []string{"openshift-gitops"} // Just assume the default
	}

	csv := olmv1alpha1.ClusterServiceVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name:      installedCSV,
			Namespace: gitopsSubscription.Namespace, // always in the same NS as the Subscription
		},
	}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(&csv), &csv); err != nil {

		resEntry := entry{
			message: fmt.Sprintf("Subscription exists, and points to ClusterServiceVersion, but ClusterServiceVersion could not be retrieved: %v", err),
		}

		if k8sClient.IncompleteControlPlaneData() {
			resEntry.level = LogLevel_Error
		} else {
			resEntry.level = LogLevel_Fatal
		}

		resEntries = append(resEntries, resEntry)
		return resClusterInformation, resEntries
	}

	if csv.Status.Phase != "Succeeded" || csv.Status.Reason != "InstallSucceeded" {
		resEntries = append(resEntries, entry{
			level:   LogLevel_Error,
			message: fmt.Sprintf("unexpected values found in ClusterServiceVersion: .status.phase: %s, .status.reason: %s", csv.Status.Phase, csv.Status.Reason),
		})
		return resClusterInformation, resEntries
	}

	resClusterInformation.operatorVersion = csv.Spec.Version.String()

	return resClusterInformation, resEntries
}

func runChecks(ctx context.Context, k8sClient clients.AbstractK8sClient) {

	clusterInfo, entries := acquireInstallConfigurationData(ctx, k8sClient)

	outputEntryList(entries)

	operatorVersion := "N/A"
	operatorInstallNS := "N/A"

	clusterScopedNamespaces := []string{}

	if clusterInfo.operatorVersion != "" {
		operatorVersion = clusterInfo.operatorVersion
	}

	if clusterInfo.operatorInstallNS != "" {
		operatorInstallNS = clusterInfo.operatorInstallNS
	}

	if len(clusterInfo.clusterScopedNamespaces) > 0 {
		clusterScopedNamespaces = clusterInfo.clusterScopedNamespaces
	}

	outputStatusMessage("--------------------")
	outputStatusMessage("Installed operator version is: '" + operatorVersion + "'")
	outputStatusMessage("Operator installed in namespace: '" + operatorInstallNS + "'")
	outputStatusMessage(fmt.Sprintf("Cluster-scoped Argo CD instance namespaces: %v", clusterScopedNamespaces))
	outputStatusMessage("--------------------")

	if entryListContainsFatal(entries) {

		return
	}

	var argoCDList v1beta1.ArgoCDList
	if err := k8sClient.ListFromAllNamespaces(ctx, &argoCDList); err != nil {
		failWithError("unable to list ArgoCDs", err)
	}

	for _, argoCD := range argoCDList.Items {
		issues := checkIndividualArgoCDCR(argoCD)

		if len(*issues) == 0 {
			continue
		}

		outputStatusMessage("Namespace '" + argoCD.Namespace + "' -> ArgoCD '" + argoCD.Name + "':")

		for _, issue := range *issues {
			reportIssue(issue)
			fmt.Println()
		}

	}
}

func outputStatusMessage(str string) {
	fmt.Println(str)
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
