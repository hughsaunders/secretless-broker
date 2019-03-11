#!/usr/bin/env bash

. ./utils.sh

echo ">>--- Deploying Sidecar Injection service"
./sidecar-injection/deploy-sci.sh

echo ">>--- Labeling Namespace for Sidecar Injection"
kubectl label \
  namespace quick-start-application-ns \
  cyberark-sidecar-injector=enabled

# store Secretless config
echo ">>--- Create and store Secretless configuration"

kubectl --namespace quick-start-application-ns \
    delete configmap/quick-start-application-secretless-config \
    --ignore-not-found=true
kubectl --namespace quick-start-application-ns \
 create configmap \
 quick-start-application-secretless-config \
 --from-file=etc/secretless.yml

# start application
echo ">>--- Start application"

kubectl --namespace quick-start-application-ns \
 apply \
 -f etc/quick-start-application.yml

if [[ "${CREATE_AND_CONFIGURE_LBS:-false}" == "true" ]]
then
    # Expose Service, useful if deploying to a remote K8s
    kubectl expose svc quick-start-application \
        --type=LoadBalancer --port 8080 \
        --target-port 8080 \
        --name quick-start-application-lb \
        -n quick-start-application-ns

    echo ">>--- Waiting for external IP allocation"
    while kubectl get svc/quick-start-application-lb\
        -n quick-start-application-ns |grep pending; do sleep 5; done
    echo ">>--- External IP allocated"

    # Get IP allocated to LB
    APP_IP="$(kubectl get svc/quick-start-application-lb \
        -n quick-start-application-ns \
        -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"

    # Insert IP into admin config.
    sed -i .bk \
        's/^APPLICATION_URL=.*$/APPLICATION_URL="'${APP_IP}':8080"/' \
        admin_config.sh
fi

wait_for_app quick-start-application quick-start-application-ns
