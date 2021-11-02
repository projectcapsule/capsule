<template>
  <div class="bg-gray-800 text-gray-100 relative">
    <app-navbar @onToggleMenu="toggleMenu" type="doc" class="mb-16" />
    <div
      class="
        container
        relative
        lg:flex lg:items-stretch lg:space-x-12
        min-h-screen
        whitespace-normal
      "
    >
      <div
        class="
          lg:w-3/12
          2xl:w-2/12
          bg-gray-900
          lg:bg-gray-800
          fixed
          z-20
          lg:sticky
          bottom-0
          lg:bottom-auto
          left-0
          lg:left-auto
          right-0
          lg:right-auto
          top-0
          lg:top-6
          h-screen
          overflow-auto
          pt-20
          lg:pt-0
          pb-16
          pr-2
          transform
          transition-transform
          duration-200
        "
        :class="{
          '-translate-x-full lg:translate-x-0': !isMenuOpened,
          'translate-x-0': isMenuOpened,
        }"
      >
        <app-sidebar />
      </div>
      <div class="lg:w-7/12 2xl:w-8/12 overflow-hidden">
        <article class="pb-96">
          <slot />
        </article>
      </div>
      <div
        class="
          hidden
          lg:inline-block lg:w-2/12 lg:sticky
          top-6
          h-screen
          overflow-auto
          pb-16
        "
      >
        <slot name="onThisPage"></slot>
      </div>
    </div>
    <app-footer />
  </div>
</template>

<script>
import AppNavbar from "~/components/AppNavbar.vue";
import AppFooter from "~/components/AppFooter.vue";
import AppSidebar from "~/components/AppSidebar.vue";

export default {
  components: {
    AppNavbar,
    AppFooter,
    AppSidebar,
  },
  data() {
    return {
      isMenuOpened: false,
    };
  },
  methods: {
    toggleMenu(value) {
      this.isMenuOpened = value;
      console.log(this.isMenuOpened);
    },
  },
};
</script>
