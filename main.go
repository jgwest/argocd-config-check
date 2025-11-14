package main

import (
	"context"
	"fmt"

	"github.com/argoproj-labs/argocd-operator/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func main() {

	fmt.Println("Hi")

	argoCD := v1beta1.ArgoCD{}

	k8sClient, _ := GetK8sClient()

	ctx := context.Background()

	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(&argoCD), &argoCD); err != nil {
		FailWithError("unable to locate argo cd", err)
	}

}
