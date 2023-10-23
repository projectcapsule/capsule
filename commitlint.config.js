const Configuration = {
    extends: ['@commitlint/config-conventional'],
    plugins: ['commitlint-plugin-function-rules'],
    rules: {
      'scope-enum': [2, 'always', ['all', 'chart', 'operator', 'manifest', 'deps', 'release', 'website', 'repo', 'e2e', 'make']],
      'type-enum': [2, 'always', ['chore', 'ci', 'docs', 'feat', 'test', 'fix', 'sec']],
    },
    /*
     * Whether commitlint uses the default ignore rules, see the description above.
     */
    defaultIgnores: true,
    /*
     * Custom URL to show upon failure
     */
    helpUrl:
      'https://github.com/projectcapsule/capsule/blob/main/CONTRIBUTING.md#commits',
  };
  
  module.exports = Configuration;