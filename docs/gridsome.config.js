// This is where project configuration and plugin options are located.
// Learn more: https://gridsome.org/docs/config

// Changes here require a server restart.
// To restart press CTRL + C in terminal and run `gridsome develop`

module.exports = {
  siteName: 'Capsule Documentation',
  titleTemplate: 'Capsule Documentation | %s',
  siteDescription: 'Documentation of Capsule, multi-tenant Operator for Kubernetes',
  icon: {
    favicon: './src/assets/favicon.png',
  },
  plugins: [
    {
      use: "gridsome-plugin-tailwindcss",

      options: {
        tailwindConfig: './tailwind.config.js',
        // presetEnvConfig: {},
        // shouldImport: false,
        // shouldTimeTravel: false
      }
    },
    {
      use: '@gridsome/source-filesystem',
      options: {
        baseDir: './content',
        path: '**/*.md',
        pathPrefix: '/docs',
        typeName: 'MarkdownPage',
        remark: {
          externalLinksTarget: '_blank',
          externalLinksRel: ['noopener', 'noreferrer'],
          plugins: [
            '@gridsome/remark-prismjs'
          ]
        }
      }
    },
  ],
  chainWebpack: config => {
    const svgRule = config.module.rule('svg')
    svgRule.uses.clear()
    svgRule
      .use('vue-svg-loader')
      .loader('vue-svg-loader')
  }
}
