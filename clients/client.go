package clients

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type AbstractK8sClient interface {
	ListFromAllNamespaces(ctx context.Context, list client.ObjectList) error
	ListFromSingleNamespace(ctx context.Context, list client.ObjectList, namespace string) error
}
