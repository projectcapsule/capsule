<template>
  <div class="bg-gray-800 text-gray-100 relative">
    <app-navbar @onToggleMenu="toggleMenu" class="mb-16" />
    <div
      class="
        relative
        lg:flex lg:items-stretch lg:space-x-12
        min-h-screen
        whitespace-normal
      "
    >
      <slot />
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
