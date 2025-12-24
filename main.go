package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/argoproj-labs/argocd-operator/api/v1beta1"
	semver "github.com/blang/semver/v4"
	"github.com/jgwest/argocd-config-check/clients"
	olmv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func main() {

	var abstractK8sClient clients.AbstractK8sClient

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
		outputStatusMessage("A) Validate Argo CD configuration using live K8s cluster via system K8s configuration (e.g. `~/.kube/config`)")
		outputStatusMessage("- argocd-config-check")
		outputStatusMessage("")
		outputStatusMessage("B) Validate Argo CD configuration using must-gather output (requires 'omc' tool)")
		outputStatusMessage("- argocd-config-check (path to must-gather directory for use by omc)")
		outputStatusMessage("")

		failWithError("Unexpected number of arguments.", nil)
	}

	ctx := context.Background()

	runChecks(ctx, abstractK8sClient)

}

// clusterInformation contains data extracted from operator/cluster configuration that may be useful for subsequent logic
type clusterInformation struct {
	operatorVersion   *semver.Version
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

	resClusterInformation.operatorVersion = &csv.Spec.Version.Version

	return resClusterInformation, resEntries
}

func runChecks(ctx context.Context, k8sClient clients.AbstractK8sClient) {

	clusterInfo, entries := acquireInstallConfigurationData(ctx, k8sClient)

	outputEntryList(entries)

	if entryListContainsFatal(entries) {
		return
	}

	entries = []entry{} // reset list after output

	operatorVersion := "N/A"
	operatorInstallNS := "N/A"

	clusterScopedNamespaces := []string{}

	if clusterInfo.operatorVersion != nil {
		operatorVersion = clusterInfo.operatorVersion.String()
	}

	if clusterInfo.operatorInstallNS != "" {
		operatorInstallNS = clusterInfo.operatorInstallNS
	}

	if len(clusterInfo.clusterScopedNamespaces) > 0 {
		clusterScopedNamespaces = clusterInfo.clusterScopedNamespaces
	}

	outputStatusMessage("--------------------")
	outputStatusMessage("Installed operator version is: '" + operatorVersion + "'")
	outputStatusMessage("- Currently supported operator versions can be found at: https://access.redhat.com/support/policy/updates/openshift_operators")
	outputStatusMessage("")
	outputStatusMessage("Operator installed in namespace: '" + operatorInstallNS + "'")
	outputStatusMessage(fmt.Sprintf("Cluster-scoped Argo CD instance namespaces: %v", clusterScopedNamespaces))
	outputStatusMessage("--------------------")
	outputStatusMessage("")

	// TODO: list which namespaces are managed by which instances
	// TODO: list which namespaces are managed by which cluster instances (etc)

	var argoCDList v1beta1.ArgoCDList
	if err := k8sClient.ListFromAllNamespaces(ctx, &argoCDList); err != nil {
		failWithError("unable to list ArgoCDs", err)
	}

	for _, argoCD := range argoCDList.Items {
		issues := checkIndividualArgoCDCR(argoCD, clusterInfo)

		outputStatusMessage("--------------------")
		outputStatusMessage("Namespace '" + argoCD.Namespace + "' -> ArgoCD '" + argoCD.Name + "':")

		if len(*issues) == 0 {
			outputStatusMessage("No issues found.")
			continue
		}

		outputStatusMessage("")

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
	fmt.Println("Severity: " + string(i.level))
	fmt.Println("Field: " + i.field)
	fmt.Println("-", i.message)
	if i.unsupported {
		fmt.Println("! Unsupported, non-production configuration. This may be due to use of tech preview/experimental feature, or unsupported configuration. See message for details.")
	}
}

type issue struct {
	level   LogLevel
	field   string
	message string

	// unsupported should be set to true if the configuration (or particular feature) detected is not supported by the OpenShift GitOps team. For example, using tech preview features, or using custom non-Red-Hat-built container images for essential Argo CD components.
	unsupported bool
}

func checkIndividualArgoCDCR(argoCD v1beta1.ArgoCD, clusterInfo clusterInformation) *[]issue {

	issues := []issue{}

	// TODO: Return on fatals?

	checkArgoCDCRForDeprecatedFields(argoCD, &issues)
	checkArgoCDCRForUnsupportedCustomImages(argoCD, &issues)
	checkForTechPreviewOrExperimentalFeatures(argoCD, &issues)
	checkForEnvVarsOrParamsWhichOverlapWithCRFields(argoCD, &issues)
	checkForIncorrectConfigurations(argoCD, &issues)
	checkArgoCDStatusField(argoCD, &issues)

	return &issues

}

// checkArgoCDCRForUnsupportedCustomImages identifies the use of custom container images for components where that is not supported. Only official OpenShift GitOps images (built by konflux and server by Red Hat image registry) are supported.
func checkArgoCDCRForUnsupportedCustomImages(argoCD v1beta1.ArgoCD, issues *[]issue) {

	if argoCD.Spec.ApplicationSet != nil && len(argoCD.Spec.ApplicationSet.Image) > 0 {

		*issues = append(*issues, issue{
			level:       LogLevel_Error,
			field:       ".spec.applicationSet.image",
			message:     "The image field is used to provide custom container images for Argo CD components. However, specifying custom images for essential Argo CD components is not supported.",
			unsupported: true,
		})

	}

	if argoCD.Spec.SSO != nil && argoCD.Spec.SSO.Dex != nil && len(argoCD.Spec.SSO.Dex.Image) > 0 {

		*issues = append(*issues, issue{
			level:       LogLevel_Error,
			field:       ".spec.sso.dex.image",
			message:     "The image field is used to provide custom container images for Argo CD components. However, specifying custom images for essential Argo CD components is not supported.",
			unsupported: true,
		})

	}

	if len(argoCD.Spec.HA.RedisProxyImage) > 0 {

		*issues = append(*issues, issue{
			level:       LogLevel_Error,
			field:       ".spec.ha.redisProxyImage",
			message:     "The image field is used to provide custom container images for Argo CD components. However, specifying custom images for essential Argo CD components is not supported.",
			unsupported: true,
		})

	}

	if argoCD.Spec.ArgoCDAgent != nil {

		if argoCD.Spec.ArgoCDAgent.Agent != nil && len(argoCD.Spec.ArgoCDAgent.Agent.Image) > 0 {
			*issues = append(*issues, issue{
				level:       LogLevel_Error,
				field:       ".spec.argoCDAgent.agent.image",
				message:     "The image field is used to provide custom container images for Argo CD components. However, specifying custom images for essential Argo CD components is not supported.",
				unsupported: true,
			})
		}

		if argoCD.Spec.ArgoCDAgent.Principal != nil && len(argoCD.Spec.ArgoCDAgent.Principal.Image) > 0 {

			*issues = append(*issues, issue{
				level:       LogLevel_Error,
				field:       ".spec.argoCDAgent.principal.image",
				message:     "The image field is used to provide custom container images for Argo CD components. However, specifying custom images for essential Argo CD components is not supported.",
				unsupported: true,
			})

		}

	}

	if len(argoCD.Spec.Notifications.Image) > 0 {
		*issues = append(*issues, issue{
			level:       LogLevel_Error,
			field:       ".spec.notifications.image",
			message:     "The image field is used to provide custom container images for Argo CD components. However, specifying custom images for essential Argo CD components is not supported.",
			unsupported: true,
		})
	}

	if len(argoCD.Spec.Redis.Image) > 0 {
		*issues = append(*issues, issue{
			level:       LogLevel_Error,
			field:       ".spec.redis.image",
			message:     "The image field is used to provide custom container images for Argo CD components. However, specifying custom images for essential Argo CD components is not supported.",
			unsupported: true,
		})
	}

	if len(argoCD.Spec.Repo.Image) > 0 {
		*issues = append(*issues, issue{
			level:       LogLevel_Error,
			field:       ".spec.repo.image",
			message:     "The image field is used to provide custom container images for Argo CD components. However, specifying custom images for essential Argo CD components is not supported.",
			unsupported: true,
		})
	}

	if len(argoCD.Spec.Image) > 0 {
		*issues = append(*issues, issue{
			level:       LogLevel_Error,
			field:       ".spec.image",
			message:     "The image field is used to provide custom container images for Argo CD components. However, specifying custom images for essential Argo CD components is not supported.",
			unsupported: true,
		})
	}

}

// checkArgoCDCRForDeprecatedFields identifies fields that are deprecated and no longer supported by ArgoCD operator.
func checkArgoCDCRForDeprecatedFields(argoCD v1beta1.ArgoCD, issues *[]issue) {

	if len(argoCD.Spec.ConfigManagementPlugins) > 0 {
		*issues = append(*issues, issue{
			level:   LogLevel_Error,
			field:   ".spec.configMapPlugins",
			message: "ConfigManagementPlugins field is no longer supported. Argo CD now requires plugins to be defined as sidecar containers of repo server component. See '.spec.repo.sidecarContainers'. ConfigManagementPlugins was previously used to specify additional config management plugins.",
		})
	}

	if argoCD.Spec.Grafana.Enabled {
		*issues = append(*issues, issue{
			level:   LogLevel_Error,
			field:   ".spec.grafana",
			message: "grafana field is deprecated from ArgoCD CR: this field will be ignored by operator, and any remaining Grafana resources will be removed.",
		})
	}

	if len(argoCD.Spec.InitialRepositories) > 0 {
		*issues = append(*issues, issue{
			level:   LogLevel_Error,
			field:   ".spec.initialRepositories",
			message: "initialRepositories field is deprecated from ArgoCD CR. The field will be ignored by operator.",
		})
	}

	if len(argoCD.Spec.RepositoryCredentials) > 0 {
		*issues = append(*issues, issue{
			level:   LogLevel_Error,
			field:   ".spec.repositoryCredentials",
			message: "repositoryCredentials field is deprecated from ArgoCD CR. The field will be ignored by operator.",
		})
	}

	if argoCD.Spec.SSO != nil && argoCD.Spec.SSO.Keycloak != nil {
		*issues = append(*issues, issue{
			level:   LogLevel_Error,
			field:   ".spec.sso.keycloak",
			message: "keycloak field is no longer supported. ArgoCD operator will no longer create and manage a keycloak instance on the users behalf. Users may instead manage their own keycloak instance (using e.g. keycloak operator) and configure Argo CD to use it.",
		})
	}

}

func checkForTechPreviewOrExperimentalFeatures(argoCD v1beta1.ArgoCD, issues *[]issue) {

	genericTechPreviewMessage := "This field is a tech preview feature in OpenShift GitOps, which has not been GA-ed as of this writing. Tech preview features are not intended for production usage. More information on Tech Preview scope of support: https://access.redhat.com/support/offerings/techpreview"

	if argoCD.Spec.ApplicationSet != nil && argoCD.Spec.ApplicationSet.Enabled != nil && *argoCD.Spec.ApplicationSet.Enabled == true {

		appSet := argoCD.Spec.ApplicationSet

		if len(appSet.SourceNamespaces) > 0 {
			*issues = append(*issues, issue{
				level:       LogLevel_Warn,
				field:       ".spec.applicationSet.sourceNamespaces",
				message:     genericTechPreviewMessage,
				unsupported: true,
			})
		}

		if containerArgsContainsParam(appSet.ExtraCommandArgs, "enable-progressive-syncs") {
			*issues = append(*issues, issue{
				level:       LogLevel_Warn,
				field:       ".spec.applicationSet.extraCommandArgs = --enable-progressive-syncs",
				message:     genericTechPreviewMessage,
				unsupported: true,
			})
		}

		if containerEnvVarContainsKeyValue(appSet.Env, "ARGOCD_APPLICATIONSET_CONTROLLER_ENABLE_PROGRESSIVE_SYNCS", "true") {
			*issues = append(*issues, issue{
				level:       LogLevel_Warn,
				field:       ".spec.applicationSet.env[ARGOCD_APPLICATIONSET_CONTROLLER_ENABLE_PROGRESSIVE_SYNCS]=true",
				message:     genericTechPreviewMessage,
				unsupported: true,
			})
		}

	}

	if argoCD.Spec.Controller.IsEnabled() {
		appController := argoCD.Spec.Controller

		if appController.Sharding.DynamicScalingEnabled != nil && *appController.Sharding.DynamicScalingEnabled == true {

			*issues = append(*issues, issue{
				level:       LogLevel_Warn,
				field:       ".spec.controller.sharding.dynamicScalingEnabled",
				message:     genericTechPreviewMessage,
				unsupported: true,
			})
		}

		// Sharding algorithms which are tech preview (robin-robin) or experimental upstream (consistent-hashing)
		experimentalShardingAlgorithms := []string{"round-robin", "consistent-hashing"}

		for _, experimentalShardingAlgorithm := range experimentalShardingAlgorithms {

			if containerEnvVarContainsKeyValue(appController.Env, "ARGOCD_CONTROLLER_SHARDING_ALGORITHM", experimentalShardingAlgorithm) {
				*issues = append(*issues, issue{
					level:       LogLevel_Warn,
					field:       ".spec.controller.env[ARGOCD_CONTROLLER_SHARDING_ALGORITHM]=" + experimentalShardingAlgorithm,
					message:     genericTechPreviewMessage,
					unsupported: true,
				})
			}

			if containerArgsContainsParamKV(appController.ExtraCommandArgs, "sharding-method", experimentalShardingAlgorithm) {
				*issues = append(*issues, issue{
					level:       LogLevel_Warn,
					field:       ".spec.controller.extraCommandArgs: --sharding-method=" + experimentalShardingAlgorithm,
					message:     genericTechPreviewMessage,
					unsupported: true,
				})
				break
			}
		}
	}
}

// TODO: .spec.extraConfig can be used to inject into argocd-cm

// checkForEnvVarsOrParamsWhichOverlapWithCRFields is designed to check for cases where a user has specified env var or container arg that overrides another field within the ArgoCD CR.
// For example, if a user attempts to enable ApplicationSets in any namespace feature, via:
// - 'ARGOCD_APPLICATIONSET_CONTROLLER_NAMESPACES=(...)'
// - '--applicationset-namespaces=(...)
// This is incorrect, as the correct mechanism to enable ApplicationSets in any namespace is via '.spec.applicationSet.sourceNamespaces' field in the CR
func checkForEnvVarsOrParamsWhichOverlapWithCRFields(argoCD v1beta1.ArgoCD, issues *[]issue) {

	{
		extraConfig := argoCD.Spec.ExtraConfig

		if extraConfig["timeout.reconciliation"] != "" {
			*issues = append(*issues, issue{
				level:   LogLevel_Warn,
				field:   ".spec.extraConfig[timeout.reconciliation] = (...)",
				message: "The 'timeout.reconciliation' value in extraConfig is supported, but it is preferable to use '.spec.controller.appSync' ArgoCD CR field for this.",
			})
		}

		if extraConfig["admin.enabled"] != "" {
			*issues = append(*issues, issue{
				level:   LogLevel_Warn,
				field:   ".spec.extraConfig[admin.enabled] = (...)",
				message: "The 'admin.enabled' value in extraConfig is supported, but it is preferable to use '.spec.disableAdmin' ArgoCD CR field for this.",
			})
		}

		if extraConfig["statusbadge.enabled"] != "" {
			*issues = append(*issues, issue{
				level:   LogLevel_Warn,
				field:   ".spec.extraConfig[statusbadge.enabled] = (...)",
				message: "The 'statusbadge.enabled' value in extraConfig is supported, but it is preferable to use '.spec.statusBadgeEnabled' ArgoCD CR field for this.",
			})
		}

	}

	if argoCD.Spec.ApplicationSet != nil && argoCD.Spec.ApplicationSet.Enabled != nil && *argoCD.Spec.ApplicationSet.Enabled == true {

		appSet := *argoCD.Spec.ApplicationSet

		if containerEnvVarContainsName(appSet.Env, "ARGOCD_APPLICATIONSET_CONTROLLER_NAMESPACES") {
			*issues = append(*issues, issue{
				level:   LogLevel_Error,
				field:   ".spec.applicationSet.env[ARGOCD_APPLICATIONSET_CONTROLLER_NAMESPACES] = (...)",
				message: "The 'ARGOCD_APPLICATIONSET_CONTROLLER_NAMESPACES' environment variable should not be set directly. Use '.spec.applicationSet.sourceNamespaces' field instead to enable ApplicationSets in any namespace.",
			})
		}

		if containerArgsContainsParam(appSet.ExtraCommandArgs, "applicationset-namespaces") {
			*issues = append(*issues, issue{
				level:   LogLevel_Error,
				field:   ".spec.applicationSet.extraCommandArgs: --applicationset-namespaces",
				message: "The '--applicationset-namespaces' argument should not be set directly. Use '.spec.applicationSet.sourceNamespaces' field instead to enable ApplicationSets in any namespace.",
			})
		}

	}

	if argoCD.Spec.Controller.IsEnabled() {
		appController := argoCD.Spec.Controller

		if containerArgsContainsParam(appController.ExtraCommandArgs, "status-processors") {
			*issues = append(*issues, issue{
				level:   LogLevel_Warn,
				field:   ".spec.controller.extraCommandArgs: --status-processors",
				message: "While specifying --status-processors via extraCommandArgs is supported, it is preferable to use '.spec.controller.processors.status' ArgoCD CR field for this.",
			})
		}

		if containerEnvVarContainsName(appController.Env, "ARGOCD_APPLICATION_CONTROLLER_STATUS_PROCESSORS") {
			*issues = append(*issues, issue{
				level:   LogLevel_Error,
				field:   ".spec.controller.env[ARGOCD_APPLICATION_CONTROLLER_STATUS_PROCESSORS] = (...)",
				message: "Specifying ARGOCD_APPLICATION_CONTROLLER_STATUS_PROCESSORS is not guaranteed to be supported. Use '.spec.controller.processors.status' ArgoCD CR field for this.",
			})
		}

		if containerArgsContainsParam(appController.ExtraCommandArgs, "operation-processors") {
			*issues = append(*issues, issue{
				level:   LogLevel_Warn,
				field:   ".spec.controller.extraCommandArgs: --operation-processors",
				message: "While specifying --operation-processors via extraCommandArgs is supported, it is preferable to use '.spec.controller.processors.operation' ArgoCD CR field for this.",
			})
		}

		if containerEnvVarContainsName(appController.Env, "ARGOCD_APPLICATION_CONTROLLER_OPERATION_PROCESSORS") {
			*issues = append(*issues, issue{
				level:   LogLevel_Error,
				field:   ".spec.controller.env[ARGOCD_APPLICATION_CONTROLLER_OPERATION_PROCESSORS] = (...)",
				message: "Specifying ARGOCD_APPLICATION_CONTROLLER_OPERATION_PROCESSORS is not guaranteed to be supported. Use '.spec.controller.processors.operation' ArgoCD CR field for this.",
			})
		}

		if containerEnvVarContainsName(appController.Env, "ARGOCD_CONTROLLER_REPLICAS") {
			*issues = append(*issues, issue{
				level:   LogLevel_Error,
				field:   ".spec.controller.env[ARGOCD_CONTROLLER_REPLICAS] = (...)",
				message: "Specifying ARGOCD_CONTROLLER_REPLICAS is not supported. Use '.spec.controller.sharding.replicas' ArgoCD CR field for this.",
			})
		}

	}

	if argoCD.Spec.Repo.Enabled != nil && *argoCD.Spec.Repo.Enabled {
		repo := argoCD.Spec.Repo

		if containerEnvVarContainsName(repo.Env, "ARGOCD_EXEC_TIMEOUT") {
			*issues = append(*issues, issue{
				level:   LogLevel_Warn,
				field:   ".spec.repo.env[ARGOCD_EXEC_TIMEOUT] = (...)",
				message: "Specifying ARGOCD_EXEC_TIMEOUT is supported, but it is preferable to use '.spec.repo.execTimeout' ArgoCD CR field for this.",
			})
		}
	}

	if argoCD.Spec.Server.Enabled != nil && *argoCD.Spec.Server.Enabled {
		server := argoCD.Spec.Server

		if containerEnvVarContainsName(server.Env, "ARGOCD_API_SERVER_REPLICAS") {
			*issues = append(*issues, issue{
				level:   LogLevel_Error,
				field:   ".spec.server.env[ARGOCD_API_SERVER_REPLICAS] = (...)",
				message: "Specifying ARGOCD_API_SERVER_REPLICAS env is not supported. Instead use ArgoCD CR '.spec.server.replicas'.",
			})
		}
	}
}

func checkForIncorrectConfigurations(argoCD v1beta1.ArgoCD, issues *[]issue) {

	if argoCD.Spec.Controller.IsEnabled() {
		appController := argoCD.Spec.Controller

		if appController.Sharding.Enabled {

			// Detect the case where dynamic scaling is disabled, but clusterPerShard is enabled, which is incorrect.
			if appController.Sharding.DynamicScalingEnabled == nil || *appController.Sharding.DynamicScalingEnabled == false {
				if appController.Sharding.ClustersPerShard != 0 {
					*issues = append(*issues, issue{
						level:   LogLevel_Error,
						field:   ".spec.controller.sharding.clustersPerShard",
						message: "'clusterPerShard' is specified, but this value is not used because dynamic scaling is disabled. The 'clusterPerShard' field is only used when dynamic scaling is ENABLED. Enable dynamic scaling, or remove the 'clustersPerShard' field.",
					})
				}
			}
		}

	}

}

func checkArgoCDStatusField(argoCD v1beta1.ArgoCD, issues *[]issue) {
	if argoCD.Status.Phase != "Available" {
		*issues = append(*issues, issue{
			level:   LogLevel_Error,
			field:   ".status.phase",
			message: "The '.status.phase' field is not currently available. This implies that one or more Argo CD components are not currently running.",
		})
	}

	for _, condition := range argoCD.Status.Conditions {
		if condition.Type == "Reconciled" && condition.Status != "True" {
			*issues = append(*issues, issue{
				level:   LogLevel_Error,
				field:   ".status.conditions[].type = Reconciled",
				message: "The 'Reconciled' .status.conditions condition is currently not 'true'. This implies the ArgoCD CR has been reconciled by the operator, but not successfully. E.g. an error occured during reconciliation",
			})
		}
	}
}
