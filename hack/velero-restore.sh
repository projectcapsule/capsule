#!/usr/bin/env bash

# This script was made to ensure the right cluster
# restoration using Velero backup technology having Capsule installed.
# It requires cluster admin permission.

# let script exit if a command fails
#set -o errexit

# let script exit if an unused variable is used
#set -o nounset

KUBECFGFILE="$HOME/.kube/config"
KUBEOPTIONS="--kubeconfig=$KUBECFGFILE"
TMPDIR=/tmp
TENANTS=""

# Print usage to stdout.
# Arguments:
#   None
# Outputs:
#   print usage with examples.
usage () {
   printf "Usage: $0 [flags] commands\n" 
   printf "Flags:\n" 
   printf "\t-c, --kubeconfig /path/to/config\tPath to the kubeconfig file to use for CLI requests.\n" 
   printf "\t-t, --tenant \"gas oil\"\t\tSpecify one or more tenants to be restored.\n" 
   printf "Commands:\n" 
   printf "\trestore\t\t\tPerform the restore on the cluster, patching the right object fields.\n" 
   printf "\n"
   printf "E.g. [restore]:\t$0 -c /path/to/kubeconfig restore\n"
   printf "E.g. [restore]:\t$0 -t \"oil\" restore\n"
}

# Update KUBEOPTIONS global var.
# Arguments:
#   None
# Outputs:
#   None
update_kube_options () {
    KUBEOPTIONS="--kubeconfig=$KUBECFGFILE"
}

# Check if the give command is present on the system.
# Arguments:
#   $1 - command name
# Outputs:
#   Notice that the command is not present.
# Examples:
#   check_prerequisite "kubectl"
check_prerequisite () {
    cmd=$1
    if ! command -v "$cmd" &> /dev/null; then
        printf "Please, install kubectl first.\n"
        exit 1
    fi
}

# Retrive tenant list.
# Arguments:
#   None
# Outputs:
#   list of the tenants.
get_tenant_list () {
    if [ ! -z "$TENANTS" ]; then
        echo "$TENANTS"
        return
    else
        tenants=$(kubectl "$KUBEOPTIONS" get tnt \
            --no-headers -o custom-columns=":.metadata.name")
        echo $tenants
    fi
    return
}

# Retrive namespace list.
# Arguments:
#   $1 - tenant to retrieve namespaces.
# Outputs:
#   list of the namespaces.
# Examples:
#   get_namespace_list "oil"
get_namespace_list () {
    tnt="$1"
    namespaces=$(kubectl "$KUBEOPTIONS" get ns -l capsule.clastix.io/tenant="$tnt" \
        --no-headers -o custom-columns=":metadata.name")
    echo $namespaces
}

# Perform cluster backup listing the namespaces
# for each tenant and retrieving their ownerReference.
# Arguments:
#   None
# Outputs:
#   Tenants backup status.
cluster_backup () {
    # Retrieve tenant names and uids.
    owner_reference=$(kubectl "$KUBEOPTIONS" get tenants.capsule.clastix.io --no-headers \
        -o custom-columns=":apiVersion,:.metadata.name,:.metadata.uid")

    # Store information inside /tmp/tenant_$(tenant_name).
    while IFS= read -r line; do
        apiVersion=$(echo "$line" | awk '{ print $1 }')
        tnt=$(echo "$line" | awk '{ print $2 }')
        uid=$(echo "$line" | awk '{ print $3 }')

        cat <<EOF > "$TMPDIR/tenant_$tnt"
{
  "op": "add",
  "path": "/metadata/ownerReferences",
  "value": [
    {
      "apiVersion": "${apiVersion}",
      "blockOwnerDeletion": true,
      "controller": true,
      "kind": "Tenant",
      "name": "${tnt}",
      "uid": "${uid}"
    }
  ]
}
EOF
    done <<< "$owner_reference"
}

# Perform cluster restore listing the namespaces
# for each tenant and adding their ownerReference.
# Arguments:
#   None
# Outputs:
#   Tenants restore status.
cluster_restore () {
    tenants=($(get_tenant_list))

    for tnt in ${tenants[@]}; do

        # Ensure backup file exists for the current tenant.
        if [ ! -f "$TMPDIR/tenant_$tnt" ]; then
            printf "Error: No backup were found for %s tenant.\n" "$tnt"
            continue
        fi
        printf "tenant(restore): %s\n" "$tnt"
        namespaces=($(get_namespace_list "$tnt"))

        for ns in ${namespaces[@]}; do
            # Read patch content from file.
            patch=$(<"$TMPDIR/tenant_$tnt")
            kubectl "$KUBEOPTIONS" patch namespaces "$ns" --type=json -p "[$patch]" &>/dev/null

            if [ $? -ne 0 ]; then
                printf "\tnamespace: %s\t[KO]\n" "$ns"
                continue
            fi
            printf "\tnamespace: %s\t[OK]\n" "$ns"
        done
    done
}

# Check presequisites:
check_prerequisite kubectl

# Print usage in case of empty arguments.
if [ $# -eq 0 ]; then
    usage
    exit
fi

# Flags parsing.
while :; do
    case "$1" in
        -h|--help)
            usage
            exit
            ;;
        -c|--kubeconfig)
            KUBECFGFILE="${2}"
            printf "Using config file: %s\n" "$KUBECFGFILE"
            update_kube_options
            shift
            ;;
        -t|--tenant)
            TENANTS="${2}"
            shift
            ;;
        *)
            break
    esac
    shift
done

# Commands parsing.
case "${@: -1}" in
    restore)
        cluster_backup
        cluster_restore
        ;;
    *)
        break
esac

