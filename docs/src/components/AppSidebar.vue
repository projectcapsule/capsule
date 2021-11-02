<template>
  <aside>
    <nav>
      <ul class="space-y-3 pl-4 lg:pl-0">
        <li
          v-for="section in $static.allSidebar.edges[0].node.sections"
          :key="section.id"
        >
          <h5
            class="text-2xl lg:text-xl font-bold mb-1"
            v-if="section.title !== ''"
          >
            {{ section.title }}
          </h5>
          <ul
            class="space-y-1.5"
            :class="{
              'pl-2': section.title !== '',
            }"
          >
            <template v-for="(item, index) in section.items">
              <li :key="item.id" v-if="item.title === ''">
                <g-link
                  :to="item.path"
                  class="
                    block
                    transition-transform
                    duration-100
                    transform
                    hover:translate-x-1
                    text-lg
                    lg:text-base
                    hover:text-blue-400
                  "
                  :class="{
                    'js-index-link': item.path === '/docs/' && index === 0,
                  }"
                >
                  {{ item.label }}</g-link
                >
              </li>
              <template v-else>
                <li :key="item.id">
                  <app-accordion>
                    <template v-slot:title>
                      <h6 class="font-semibold text-xl lg:text-lg mb-1">
                        {{ item.title }}
                      </h6>
                    </template>
                    <template v-slot:content>
                      <ul class="space-y-1.5 pl-2">
                        <li v-for="subItem in item.subItems" :key="subItem.id">
                          <g-link
                            :to="subItem.path"
                            class="
                              block
                              transition-transform
                              duration-100
                              transform
                              hover:translate-x-1
                              text-lg
                              lg:text-base
                              hover:text-blue-400
                            "
                          >
                            {{ subItem.label }}</g-link
                          >
                        </li>
                      </ul>
                    </template>
                  </app-accordion>
                </li>
              </template>
            </template>
          </ul>
        </li>
      </ul>
    </nav>
  </aside>
</template>

<static-query>
{
  allSidebar {
    edges{
      node{
        id
          sections {
            title
            items{
              title
              label
              path
              subItems {
                label
                path
              }
            }
          }
      }
    }
  }
}
</static-query>

<script>
import AppAccordion from "~/components/AppAccordion.vue";

export default {
  components: {
    AppAccordion,
  },
  watch: {
    $route: function () {
      if (process.isClient && this.$route.fullPath !== "/docs/") {
        setTimeout(function () {
          document.querySelector(".js-index-link").classList.remove("active");
        }, 80);
      }
    },
  },

  mounted() {
    if (this.$route.fullPath !== "/docs/") {
      document.querySelector(".js-index-link").classList.remove("active");
    }
  },
};
</script>

<style lang="scss" scoped>
.active {
  @apply text-blue-400 font-semibold;
}
</style>