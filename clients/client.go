package clients

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type AbstractK8sClient interface {

	// These functions largely mirror client.Client from controller-runtime (slightly simplified in some cases)

	ListFromAllNamespaces(ctx context.Context, list client.ObjectList) error
	ListFromSingleNamespace(ctx context.Context, list client.ObjectList, namespace string) error
	Get(ctx context.Context, key client.ObjectKey, obj client.Object) error

	// IncompleteControlPlaneData is true if the control plane (list/get data from client) does not necessarily represent the full set of K8s resources on the  K8s cluster.
	// - An example of this would be if the client only returns data from a 'openshift-gitops' namespace, but not from any other namespaces.
	// - This is usually due to using OMC client with must-gather data: with must-gather, users have the ability to only export a single namespace into must-gather, and this means that other namespaces on the cluster are not included/available to be read.
	//
	// TL;DR: return true if client is OMC-based.
	IncompleteControlPlaneData() bool
}
