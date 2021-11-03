<template>
  <ul class="space-y-2">
    <template v-for="(heading, index) in headings">
      <li v-if="heading.depth === 2" :key="index" class="hover:underline">
        <g-link
          :to="`${pagePath}${heading.anchor}`"
          class="text-sm"
          :class="{
            'text-white underline font-semibold':
              activeAnchor === heading.anchor,
            'text-gray-300': activeAnchor !== heading.anchor,
          }"
        >
          {{ heading.value }}
        </g-link>
      </li>
    </template>
  </ul>
</template>

<script>
export default {
  props: {
    headings: {
      type: Array,
      required: true,
    },
    pagePath: {
      type: String,
      required: true,
    },
  },

  data() {
    return {
      activeAnchor: "",
      observer: null,
    };
  },

  watch: {
    $route: function () {
      if (process.isClient && window.location.hash) {
        this.activeAnchor = window.location.hash;
      }

      if (this.observer) {
        // Clear the current observer.
        this.observer.disconnect();

        this.$nextTick(this.initObserver);
      }
    },
  },

  mounted() {
    if (process.isClient) {
      if (window.location.hash) {
        this.activeAnchor = window.location.hash;
      }
      this.$nextTick(this.initObserver);
    }
  },

  methods: {
    observerCallback(entries, observer) {
      // This early return fixes the jumping
      // of the bubble active state when we click on a link.
      // There should be only one intersecting element anyways.
      if (entries.length > 1) {
        return;
      }

      const id = entries[0].target.id;

      // We want to give the link of the intersecting
      // headline active and add the hash to the url.
      if (id) {
        this.activeAnchor = "#" + id;

        if (history.replaceState) {
          history.replaceState(null, null, "#" + id);
        }
      }
    },

    initObserver() {
      this.observer = new IntersectionObserver(this.observerCallback, {
        // This rootMargin should allow intersections at the top of the page.
        // root: document.querySelector('.content'),
        rootMargin: "0px 0px 99999px",
        threshold: 1,
      });

      const elements = document.querySelectorAll(
        ".content h2"
      );

      for (let i = 0; i < elements.length; i++) {
        this.observer.observe(elements[i]);
      }
    },
  },
};
</script>

<style>
</style>