// This is the main.js file. Import global CSS and scripts here.
// The Client API can be used here. Learn more: gridsome.org/docs/client-api
import 'prism-themes/themes/prism-lucario.min.css'

import DefaultLayout from '~/layouts/Default.vue'
import MarkdownLayout from '~/layouts/Markdown.vue'

export default function (Vue, { router, head, isClient }) {
  // Set default layout as a global component
  Vue.component('LayoutDefault', DefaultLayout)
  Vue.component('LayoutMarkdown', MarkdownLayout)
}
