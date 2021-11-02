<template>
  <component
    :is="link ? 'g-link' : 'button'"
    :to="link"
    class="
      inline-block
      focus:outline-none
      font-medium
      rounded-md
      relative
      bg-primary
      py-3 px-8
    "
  >
    <slot />
  </component>
</template>

<script>
export default {
  props: {
    link: {
      type: String,
      required: false,
      default: () => undefined,
    },
  },
};
</script>

<style>
</style>