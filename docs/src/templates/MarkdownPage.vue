<template>
  <layout-markdown>
    <div class="content" v-html="$page.markdownPage.content"></div>
    <template v-slot:onThisPage>
      <on-this-page
        :headings="$page.markdownPage.headings"
        :pagePath="$page.markdownPage.path"
      />
    </template>
  </layout-markdown>
</template>

<page-query>
query ($id: ID!) {
  markdownPage(id: $id) {
    id
    title
    content
    path
    headings{
      depth
      value
      anchor
    }
  }

  metadata {
    siteDescription
  }
}
</page-query>


<script>
import OnThisPage from "~/components/OnThisPage.vue";

export default {
  metaInfo() {
    return {
      title: this.$page.markdownPage.title,
      meta: [
        {
          property: "og:title",
          content: this.$page.markdownPage.title,
        },
        {
          property: "og:description",
          content: this.$page.metadata.siteDescription,
        },
        {
          property: "og:image",
          content: 'https://quizzical-roentgen-574926.netlify.app/assets/share.png',
        },
        {
          property: "twitter:card",
          content: "summary",
        },
        {
          property: "twitter:title",
          content: this.$page.markdownPage.title,
        },
        {
          property: "twitter:description",
          content: this.$page.metadata.siteDescription,
        },
        {
          property: "og:url",
          content: `https://capsule.clastix.io${this.$page.markdownPage.path}`,
        },
      ],
    };
  },

  components: {
    OnThisPage,
  },
};
</script>

<style lang="scss">
.content {
  a {
    @apply underline hover:text-blue-400;
  }
  p {
    @apply mb-2;
  }
  pre {
    margin-bottom: 1.5rem !important;
    margin-top: 1rem !important;
  }
  h1,
  h2,
  h3,
  h4,
  h5,
  h6 {
    @apply font-bold mb-2.5;
    &:not(:first-child) {
      @apply font-bold mt-4;
    }
  }
  h1 {
    @apply text-3xl;
  }
  h2 {
    @apply text-2xl;
  }
  h3 {
    @apply text-xl;
  }
  h4 {
    @apply text-lg;
  }
  h5 {
    @apply text-base;
  }
  h6 {
    @apply text-sm;
  }
  ul {
    @apply list-disc mb-2;
  }

  ol {
    @apply list-decimal mb-2;
  }

  blockquote {
    @apply border-l-4 pl-4 ml-4 my-4 border-solid border-primary;
  }

  ol,
  ul {
    @apply pl-5;
    li {
      @apply mb-0.5;
    }
  }
}
</style>