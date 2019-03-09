#!/usr/bin/env bash

. ./utils.sh
. ./admin_config.sh

# create namespace
echo ">>--- Clean up quick-start-backend-ns namespace"

kubectl delete namespace quick-start-backend-ns
while [[ $(kubectl get namespace quick-start-backend-ns 2>/dev/null) ]] ; do
  echo "Waiting for quick-start-backend-ns namespace clean up"
  sleep 5
done
kubectl create namespace quick-start-backend-ns

echo Ready!

# add pg certificates to kubernetes secrets
kubectl --namespace quick-start-backend-ns \
  create secret generic \
  quick-start-backend-certs \
  --from-file=etc/pg_server.crt \
  --from-file=etc/pg_server.key

# create database
echo ">>--- Create database"

kubectl --namespace quick-start-backend-ns \
 apply -f etc/pg.yml

# Wait for it
wait_for_app quick-start-backend quick-start-backend-ns

# I considered making this a utils function, but transparent
# commands are useful for a tutorial.
if [[ "${CREATE_AND_CONFIGURE_LBS:-false}" == "true" ]]
then
  # Expose Service, useful if deploying to a remote K8s
  kubectl expose svc quick-start-backend \
      --type=LoadBalancer --port 5432 \
      --target-port 5432 \
      --name quick-start-backend-lb \
      -n quick-start-backend-ns

  echo ">>--- Waiting for external IP allocation"
  # Wait for LB IP to be allocated
  while kubectl get svc/quick-start-backend-lb\
      -n quick-start-backend-ns |grep pending; do sleep 5; done
  echo ">>--- External IP allocated"

  # Get IP allocated to LB
  DB_IP="$(kubectl get svc/quick-start-backend-lb \
      -n quick-start-backend-ns \
      -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"

  # Insert IP into admin config.
  sed -i .bk \
    's+^DB_URL=.*$+DB_URL="'${DB_IP}':5432/quick_start_db"+' \
    admin_config.sh
fi

kubectl --namespace quick-start-backend-ns \
 exec \
 -i \
 $(get_first_pod_for_app quick-start-backend quick-start-backend-ns) \
 -- \
  psql \
  -U ${DB_ADMIN_USER} \
  -c "
CREATE DATABASE quick_start_db;
"
