#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

readonly KIND_VERSION=v0.17.0
readonly CLUSTER_NAME=kafed

cleanup() {
    echo 'Removing kind cluster...'
    kind delete cluster --name="$CLUSTER_NAME"

    echo 'Done!'
}

create_kind_cluster() {
    echo 'creating kind cluster...'

    # kind-darwin-amd64, kind-linux-amd64
    curl -Lo ./kind "https://github.com/kubernetes-sigs/kind/releases/download/$KIND_VERSION/kind-linux-amd64"
    chmod +x ./kind
    sudo mv ./kind /usr/local/bin/kind

    kind create cluster --name "$CLUSTER_NAME" --config ./test/e2e/kind-config.yaml --image "$KIND_IMAGE" --wait 300s

    echo 'export kubeconfig...'
    export KUBECONFIG=~/.kube/config

    echo "installing kubectl..."
    curl -Lo ./kubectl "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/darwin/amd64/kubectl"
    chmod +x ./kubectl
    sudo mv ./kubectl /usr/local/bin/kubectl

    kubectl cluster-info
    echo

    kubectl get nodes
    echo

    echo 'Cluster ready!'
    echo
}

install_controllers() {
    echo "docker pull image"
    docker pull $IMAGE
    kind load docker-image $IMAGE --name "$CLUSTER_NAME"

    echo "installing kustomize..."
    make kustomize
    KUSTOMIZE=$(pwd)/bin/kustomize

    echo "preparing kustomize yaml"
    (cd ./config/manager && $KUSTOMIZE edit set image controller=$IMAGE)

    echo "installing controller..."
    $KUSTOMIZE build ./config/default | sed -e 's/imagePullPolicy: Always/imagePullPolicy: IfNotPresent/g' | kubectl apply -f -
}

install_deps() {
  if ! command -v sudo &> /dev/null
  then
      echo "command sudo could not be found and install it"
      yum install -y sudo
  fi

  if ! command -v docker &> /dev/null
  then
      echo "command docker could not be found and install it"
      yum install -y docker
  fi

  sudo systemctl start docker
}

main() {
    if [ -z "$IMAGE" ]; then
      echo "no image provided by env var IMAGE"
      exit 1
    fi

    install_deps
    create_kind_cluster
    trap cleanup EXIT

    install_controllers
}

main