#!/bin/bash

set -o errexit
set -o pipefail
set -o nounset

resources=(
  daemonset.apps/kube-proxy
  service/kube-dns
  serviceaccount/coredns
  configmap/coredns
  deployment.apps/coredns
  clusterrole.rbac.authorization.k8s.io/system:coredns
  clusterrolebinding.rbac.authorization.k8s.io/system:coredns
  daemonset.apps/aws-node
  crd.apiextensions.k8s.io/eniconfigs.crd.k8s.amazonaws.com
  clusterrole.rbac.authorization.k8s.io/aws-node
  clusterrolebinding.rbac.authorization.k8s.io/aws-node
)

## it turns out `--export` is going away, and it doesn't support
## RBAC objects, so here is a custom stripper for all of our needs
kubectl \
  --namespace=kube-system \
  --output=json \
    get "${resources[@]}" \
      | jq -S . \
      | jq '
            del(.metadata) |
              del(.items[].metadata.uid) |
              del(.items[].metadata.selfLink) |
              del(.items[].metadata.generation) |
              del(.items[].metadata.resourceVersion) |
              del(.items[].metadata.creationTimestamp) |
              del(.items[].metadata.annotations["kubectl.kubernetes.io/last-applied-configuration"]) |
              del(.items[].metadata.annotations["deployment.kubernetes.io/revision"]) |
              del(.items[].secrets) |
              del(.items[].status)
           '
