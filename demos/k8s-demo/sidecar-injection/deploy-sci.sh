#!/bin/bash -xe
. utils.sh

helm list >/dev/null \
    || { echo "Helm must be installed and functioning before sidecar-injector can be installed."; exit 1; }


echo ">>--- Ensure sidecar-injection repo present"
[[ -e sidecar-injection/repo ]] \
    || git clone https://github.com/cyberark/sidecar-injector sidecar-injection/repo


echo ">>--- Cleanup previous sidecar-injection deployments"
helm list --all |grep cyberark-sci &>/dev/null\
    && helm delete --purge cyberark-sci

# remove namespace if it exists
kubectl delete namespace injectors --ignore-not-found=true

echo ">>--- Deploy sidecar injection helm chart"
helm --namespace injectors \
 install \
 --name cyberark-sci \
 --set "caBundle=$(kubectl -n kube-system \
   get configmap \
   extension-apiserver-authentication \
   -o=jsonpath='{.data.client-ca-file}' \
 )" \
 sidecar-injection/repo/charts/cyberark-sidecar-injector/

echo ">>--- Wait for CSR to be created"
while :
do
    # the yaml output doesn't include the condition field so -o jsonpath doesn't work here
    kubectl \
        -n injectors \
        get csr/cyberark-sci-cyberark-sidecar-injector.injectors \
        |grep Pending \
        && break
    sleep 5
done
echo ">>--- Found CSR, aproving"

# Approve CSR
kubectl \
    -n injectors \
    certificate approve cyberark-sci-cyberark-sidecar-injector.injectors


echo ">>--- Waiting for Sidecar Injector pod to initialise"
pod=$(get_first_pod_for_app cyberark-sidecar-injector injectors)
while :
do
    status=$(kubectl -n injectors get pod/${pod} \
               -o jsonpath="{.status.phase}")
    [[ "${status}" == "Running" ]] && break
    sleep 5
done
echo ">>--- Sidecar Injector pod running"
echo ">>--- Sidecar Injector deployment complete."
