# Download control plane config that will give you access to your own control plane
kubectl get secret oidc-openmcp.my-postgres-cp.kubeconfig \
  -n project-my-project--ws-dev \
  -o jsonpath='{.data.kubeconfig}' | base64 -d > ~/.kube/my-postgres-cp.kubeconfig
