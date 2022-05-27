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
            }
          ]
        },
        {
          title: 'Documentation',
          items: [
            {
              label: 'Getting Started',
              path: '/docs/general/getting-started'
            },
            {
              label: 'Tutorial',
              path: '/docs/general/tutorial'
            },
            {
              label: 'References',
              path: '/docs/general/references'
            },
            {
              label: 'Multi-Tenant Benchmark',
              path: '/docs/general/mtb'
            },
            {
              label: 'Capsule Proxy',
              path: '/docs/general/proxy'
            },
            {
              label: 'Dashboard',
              path: '/docs/general/lens'
            },
          ]
        },
        {
          title: 'Guides',
          items: [
            {
              label: 'OIDC Authentication',
              path: '/docs/guides/oidc-auth'
            },
            {
              label: 'Monitoring Capsule',
              path: '/docs/guides/monitoring'
            },
            {
              label: 'Backup & Restore with Velero',
              path: '/docs/guides/velero'
            },
            {
              label: 'Upgrading Tenant version',
              path: '/docs/guides/upgrading'
            },
            {
              label: 'Install on Charmed Kubernetes',
              path: '/docs/guides/charmed'
            },
            {
              title: 'Managed Kubernetes',
              subItems: [
                {
                  label: 'Overview',
                  path: '/docs/guides/managed-kubernetes/overview'
                },
                {
                  label: 'EKS',
                  path: '/docs/guides/managed-kubernetes/aws-eks'
                },
                {
                  label: 'CoAKS',
                  path: '/docs/guides/managed-kubernetes/coaks'
                },
              ]
            }
          ]
        },
        {
          title: 'Contributing',
          items: [
            {
              label: 'Guidelines',
              path: '/docs/contributing/guidelines'
            },
            {
              label: 'Development',
              path: '/docs/contributing/development'
            },
            {
              label: 'Governance',
              path: '/docs/contributing/governance'
            }            
          ]
        }
      ]
    })
  })


  api.createPages(({ createPage }) => {
    // Use the Pages API here: https://gridsome.org/docs/pages-api/
  })
}
