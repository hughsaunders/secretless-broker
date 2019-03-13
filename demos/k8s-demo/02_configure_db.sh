#!/usr/bin/env bash

. ./admin_config.sh
. ./utils.sh

# setup db
echo ">>--- Set up database"

docker run \
 --rm \
 -i \
 -e PGPASSWORD=${DB_ADMIN_PASSWORD} \
 postgres:9.6 \
  psql \
  -U ${DB_ADMIN_USER} \
  "postgres://$DB_URL" \
  << EOSQL
/* Create Application User */
CREATE USER ${DB_USER} PASSWORD '${DB_INITIAL_PASSWORD}';

/* Create Table */
CREATE TABLE pets (
    id serial primary key,
    name varchar(256)
);

/* Grant Permissions */
GRANT SELECT, INSERT ON public.pets TO ${DB_USER};
GRANT USAGE, SELECT ON SEQUENCE public.pets_id_seq TO ${DB_USER};
EOSQL

# create namespace
echo ">>--- Clean up quick-start-application-ns namespace"

kubectl delete namespace quick-start-application-ns --ignore-not-found=true
while [[ $(kubectl get namespace quick-start-application-ns 2>/dev/null) ]] ; do
  echo "Waiting for quick-start-application-ns namespace clean up"
  sleep 5
done

	# Remove non-namespaced resources
echo ">>--- Clean up cluster scoped resources"
kubectl delete ClusterRole/secretless-crd-role --ignore-not-found=true
kubectl delete ClusterRoleBinding/quick-start-use-secretless-crd

echo ">>--- Create application namespace"
kubectl create namespace quick-start-application-ns

echo Ready!

# store db credentials
kubectl --namespace quick-start-application-ns \
 create secret generic \
 quick-start-backend-credentials \
 --from-literal=address="${DB_URL}" \
 --from-literal=username="${DB_USER}" \
 --from-literal=password="${DB_INITIAL_PASSWORD}"

# create application service account
kubectl --namespace quick-start-application-ns \
  create serviceaccount \
  quick-start-application

# grant quick-start-application service account
# in quick-start-application-ns namespace
# access to quick-start-backend-credentials
kubectl --namespace quick-start-application-ns \
 create \
 -f etc/quick-start-application-entitlements.yml

# Create the secretless-crd-role and grant it to
# the application service account. This allows
# the secretless broker to manage sbconfig objects.
kubectl --namespace quick-start-application-ns \
  create \
  -f etc/crd-entitlements.yml
