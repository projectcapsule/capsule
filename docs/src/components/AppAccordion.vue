<template>
  <div>
    <button @click="toggleIsOpened()" class="flex items-center space-x-3 text-left">
      <slot name="title" />
      <icon-arrow
        class="w-2 transition-all duration-200 transform"
        :class="{
          'rotate-180': isOpened,
          'rotate-0': !isOpened,
        }"
      />
    </button>
    <div
      class="transition-all duration-400 overflow-hidden"
      :class="{
        'max-h-99': isOpened,
        'max-h-0': !isOpened,
      }"
    >
      <slot name="content" />
    </div>
  </div>
</template>

<script>
import IconArrow from "~/assets/icon/arrow.svg?inline";

export default {
  components: {
    IconArrow,
  },
  data() {
    return {
      isOpened: false,
    };
  },
  methods: {
    toggleIsOpened() {
      this.isOpened = !this.isOpened;
    },
  },
};
</script>

<style scoped>
.max-h-99 {
  max-height: 99rem;
}
</style>