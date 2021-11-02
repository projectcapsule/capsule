<template>
  <layout-default>
    <div class="mx-auto space-y-4 py-16 text-center">
      <h1 class="text-2xl sm:text-4xl font-semibold">404 - Page not found</h1>
      <p>We couldn't find the page you are looking for</p>
      <app-button link="/">Home</app-button>
    </div>
  </layout-default>
</template>
<script>
import AppButton from "~/components/AppButton.vue";

export default {
   metaInfo() {
    return {
      title: "404 - Page not found",
    };
  },

  components: {
    AppButton,
  },
};
</script>