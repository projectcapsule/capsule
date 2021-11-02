// Server API makes it possible to hook into various parts of Gridsome
// on server-side and add custom data to the GraphQL data layer.
// Learn more: https://gridsome.org/docs/server-api/

// Changes here require a server restart.
// To restart press CTRL + C in terminal and run `gridsome develop`

module.exports = function (api) {
  api.loadSource(actions => {
    // Use the Data Store API here: https://gridsome.org/docs/data-store-api/
    const sidebar = actions.addCollection({
      typeName: 'Sidebar'
    })

    sidebar.addNode({
      sections: [
        {
          items: [
            {
              label: 'Overview',
              path: '/docs/'
            },
            {
              label: 'Dev guide',
              path: '/docs/dev-guide'
            },
            {
              label: 'Contributing',
              path: '/docs/contributing'
            },
          ]
        },
        {
          title: 'Capsule Operator',
          items: [
            {
              label: 'Getting Started',
              path: '/docs/operator/getting-started'
            },
            {
              label: 'Monitoring Capsule',
              path: '/docs/operator/monitoring'
            },
            {
              label: 'References',
              path: '/docs/operator/references'
            },
            {
              label: 'Contributing',
              path: '/docs/operator/contributing'
            },
            {
              title: 'Run on Managed Kubernetes',
              subItems: [
                {
                  label: 'Overview',
                  path: '/docs/operator/managed-kubernetes/overview'
                },
                {
                  label: 'AWS EKS',
                  path: '/docs/operator/managed-kubernetes/aws-eks'
                },
              ]
            },
            {
              title: 'SIG Multi-tenancy benchmark',
              subItems: [
                {
                  label: 'SIG Multi-tenancy benchmark',
                  path: '/docs/operator/mtb/sig-multitenancy-bench'
                },
                {
                  label: 'Block access to cluster resources',
                  path: '/docs/operator/mtb/block-access-to-cluster-resources'
                },
                {
                  label: 'Block access to multitenant resources',
                  path: '/docs/operator/mtb/block-access-to-multitenant-resources'
                },
                {
                  label: 'Block access to other tenant resources',
                  path: '/docs/operator/mtb/block-access-to-other-tenant-resources'
                },
                {
                  label: 'Block add capabilities',
                  path: '/docs/operator/mtb/block-add-capabilities'
                },
                {
                  label: 'Require always imagePullPolicy',
                  path: '/docs/operator/mtb/require-always-imagepullpolicy'
                },
                {
                  label: 'Require run as non-root user',
                  path: '/docs/operator/mtb/require-run-as-non-root-user'
                },
                {
                  label: 'Block privileged containers',
                  path: '/docs/operator/mtb/block-privileged-containers'
                },
                {
                  label: 'Block privilege escalation',
                  path: '/docs/operator/mtb/block-privilege-escalation'
                },
                {
                  label: 'Configure namespace resource quotas',
                  path: '/docs/operator/mtb/configure-namespace-resource-quotas'
                },
                {
                  label: 'Block modification of resource quotas',
                  path: '/docs/operator/mtb/block-modification-of-resource-quotas'
                },
                {
                  label: 'Configure namespace object limits',
                  path: '/docs/operator/mtb/configure-namespace-object-limits'
                },
                {
                  label: 'Block use of host path volumes',
                  path: '/docs/operator/mtb/block-use-of-host-path-volumes'
                },
                {
                  label: 'Block use of host networking and ports',
                  path: '/docs/operator/mtb/block-use-of-host-networking-and-ports'
                },
                {
                  label: 'Block use of host PID',
                  path: '/docs/operator/mtb/block-use-of-host-pid'
                },
                {
                  label: 'Block use of host IPC',
                  path: '/docs/operator/mtb/block-use-of-host-ipc'
                },
                {
                  label: 'Block use of NodePort services',
                  path: '/docs/operator/mtb/block-use-of-nodeport-services'
                },
                {
                  label: 'Require PersistentVolumeClaim for storage',
                  path: '/docs/operator/mtb/require-persistentvolumeclaim-for-storage'
                },
                {
                  label: 'Require PV reclaim policy of delete',
                  path: '/docs/operator/mtb/require-reclaim-policy-of-delete'
                },
                {
                  label: 'Block use of existing PVs',
                  path: '/docs/operator/mtb/block-use-of-existing-persistent-volumes'
                },
                {
                  label: 'Block network access across tenant namespaces',
                  path: '/docs/operator/mtb/block-network-access-across-tenant-namespaces'
                },
                {
                  label: 'Allow self-service management of Network Policies',
                  path: '/docs/operator/mtb/allow-self-service-management-of-network-policies'
                },
                {
                  label: 'Allow self-service management of Roles',
                  path: '/docs/operator/mtb/allow-self-service-management-of-roles'
                },
                {
                  label: 'Allow self-service management of Role Bindings',
                  path: '/docs/operator/mtb/allow-self-service-management-of-rolebindings'
                },
              ]
            },
            {
              title: 'Use cases',
              subItems: [
                {
                  label: 'Onboard Tenants',
                  path: '/docs/operator/use-cases/overview'
                },
                {
                  label: 'Assign Tenant Ownership',
                  path: '/docs/operator/use-cases/tenant-ownership'
                },
                {
                  label: 'Create Namespaces',
                  path: '/docs/operator/use-cases/create-namespaces'
                },
                {
                  label: 'Assign Permissions',
                  path: '/docs/operator/use-cases/permissions'
                },
                {
                  label: 'Enforce Resources Quotas and Limits',
                  path: '/docs/operator/use-cases/resources-quota-limits'
                },
                {
                  label: 'Enforce Pod Priority Classes',
                  path: '/docs/operator/use-cases/pod-priority-classes'
                },
                {
                  label: 'Assign specific Node Pools',
                  path: '/docs/operator/use-cases/nodes-pool'
                },
                {
                  label: 'Assign Ingress Classes',
                  path: '/docs/operator/use-cases/ingress-classes'
                },
                {
                  label: 'Assign Ingress Hostnames',
                  path: '/docs/operator/use-cases/ingress-hostnames'
                },
                {
                  label: 'Control hostname collision in Ingresses',
                  path: '/docs/operator/use-cases/hostname-collision'
                },
                {
                  label: 'Assign Storage Classes',
                  path: '/docs/operator/use-cases/storage-classes'
                },
                {
                  label: 'Assign Network Policies',
                  path: '/docs/operator/use-cases/network-policies'
                },
                {
                  label: 'Enforce Containers image PullPolicy',
                  path: '/docs/operator/use-cases/images-pullpolicy'
                },
                {
                  label: 'Assign Trusted Images Registries',
                  path: '/docs/operator/use-cases/images-registries'
                },
                {
                  label: 'Assign Pod Security Policies',
                  path: '/docs/operator/use-cases/pod-security-policies'
                },
                {
                  label: 'Create Custom Resources',
                  path: '/docs/operator/use-cases/custom-resources'
                },
                {
                  label: 'Taint Namespaces',
                  path: '/docs/operator/use-cases/taint-namespaces'
                },
                {
                  label: 'Assign multiple Tenants',
                  path: '/docs/operator/use-cases/multiple-tenants'
                },
                {
                  label: 'Cordon Tenants',
                  path: '/docs/operator/use-cases/cordoning-tenant'
                },
                {
                  label: 'Disable Service Types',
                  path: '/docs/operator/use-cases/service-type'
                },
                {
                  label: 'Taint Services',
                  path: '/docs/operator/use-cases/taint-services'
                },
                {
                  label: 'Allow adding labels and annotations on namespaces',
                  path: '/docs/operator/use-cases/namespace-labels-and-annotations'
                },
                {
                  label: 'Velero Backup Restoration',
                  path: '/docs/operator/use-cases/velero-backup-restoration'
                },
                {
                  label: 'Deny Wildcard Hostnames',
                  path: '/docs/operator/use-cases/deny-wildcard-hostnames'
                },
                {
                  label: 'Denying specific user-defined labels or annotations on Nodes',
                  path: '/docs/operator/use-cases/deny-specific-user-defined-labels-or-annotations-on-nodes'
                },
              ]
            },
          ]
        },
        {
          title: 'Capsule Proxy',
          items: [
            {
              label: 'Overview',
              path: '/docs/proxy/overview'
            },
            {
              label: 'Standalone Installation',
              path: '/docs/proxy/standalone'
            },
            {
              label: 'Sidecar Installation',
              path: '/docs/proxy/sidecar'
            },
            {
              label: 'OIDC Authentication',
              path: '/docs/proxy/oidc-auth'
            },
            {
              label: 'Contributing',
              path: '/docs/proxy/contributing'
            }
          ]
        },
        {
          title: 'Capsule Lens extension',
          items: [
            {
              label: 'Overview',
              path: '/docs/lens-extension/overview'
            },
          ]
        },
      ]

    })
  })


  api.createPages(({ createPage }) => {
    // Use the Pages API here: https://gridsome.org/docs/pages-api/
  })
}
