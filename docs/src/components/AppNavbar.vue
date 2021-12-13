<template>
  <header class="py-3 lg:py-6 relative bg-gray-900 bg-opacity-50">
    <div class="container flex items-center lg:justify-between">
      <div class="w-full flex items-center space-x-4 lg:space-x-0">
        <div class="w-2/12 lg:hidden" v-if="type === 'doc'">
          <button
            class="
              lg:hidden
              relative
              z-30
              shadow-lg
              w-8
              h-8
              space-y-1
              inline-flex
              flex-col
              justify-center
              items-center
              outline-none
              focus:outline-none
              transition-all
              duration-200
              transform
            "
            @click="toggleMenu()"
          >
            <span
              class="
                inline-block
                w-6
                h-0.5
                rounded-sm
                transition-all
                duration-200
                bg-gray-100
              "
              :class="{
                'transform rotate-45 translate-y-1.5': menuOpened,
              }"
            ></span>
            <span
              class="inline-block w-6 h-0.5 rounded-sm bg-gray-100"
              :class="{
                invisible: menuOpened,
              }"
            ></span>
            <span
              class="
                inline-block
                w-6
                h-0.5
                rounded-sm
                transition-all
                duration-200
                bg-gray-100
              "
              :class="{
                'transform -rotate-45 -translate-y-1.5': menuOpened,
              }"
            ></span>
          </button>
        </div>
        <div
          class="w-8/12 lg:w-auto text-center flex items-center space-x-16"
          :class="{
            'justify-center lg:justify-start': type === 'doc',
          }"
        >
          <g-link to="/" class="flex items-center space-x-4">
            <logo-capsule class="w-10 lg:w-12" />
            <h1 class="font-bold text-2xl lg:text-3xl inline-block">Capsule</h1>
          </g-link>
          <nav class="hidden lg:inline-block">
            <ul class="flex items-center font-medium space-x-5">
              <li>
                <g-link to="/docs/general">Documentation</g-link>
              </li>
              <li>
                <g-link to="/docs/guides">Guides</g-link>
              </li>
              <li>
                <g-link to="/docs/contributing">Contributing</g-link>
              </li>
              <!-- <li class="group relative">
                main
                <ul
                  class="
                    py-2
                    px-4
                    absolute
                    top-full
                    hidden
                    group-hover:block
                    bg-gray-100
                    text-gray-800
                    rounded
                    text-left
                  "
                >
                  <li>
                    <a href="/" target="_blank" rel="noopener noreferrer">
                      v1.5.0
                    </a>
                  </li>
                  <li>
                    <a href="/" target="_blank" rel="noopener noreferrer">
                      v1.6.0
                    </a>
                  </li>
                  <li>
                    <a href="/" target="_blank" rel="noopener noreferrer">
                      main
                    </a>
                  </li>
                </ul>
              </li> -->
            </ul>
          </nav>
        </div>
        <div class="w-2/12 lg:hidden" v-if="type === 'doc'">
          <button @click="toggleSearch()" class="block ml-auto">
            <icon-search class="w-8" />
          </button>
        </div>
      </div>
      <div
        class="relative"
        :class="{
          'hidden lg:flex items-center space-x-16': type === 'doc',
        }"
      >
        <div v-if="type === 'doc'">
          <div class="relative">
            <icon-search
              class="w-8 absolute left-0 bottom-2 text-gray-100 opacity-80"
            />

            <input
              type="search"
              name="search"
              id="search"
              class="
                rounded-0
                pl-10
                pr-2
                py-2
                outline-none
                w-96
                bg-transparent
                border-b border-solid border-gray-100
                text-gray-100
              "
              placeholder="Search"
              @focus="focused = true"
              @blur="focusOut()"
              @input="query = $event.target.value"
              @change="query = $event.target.value"
            />
          </div>
          <ul
            v-show="showResult"
            class="
              absolute
              left-0
              top-12
              z-20
              text-left
              bg-gray-900
              rounded-br rounded-bl
              text-sm
              min-w-96
            "
          >
            <li v-if="results.length === 0" class="px-4 py-2">
              No results for <span class="font-bold">{{ query }}</span
              >.
            </li>
            <li v-else v-for="result in results" :key="result.id">
              <g-link
                :to="result.item.path + result.item.anchor"
                class="block px-4 py-2 hover:bg-gray-700 hover:text-blue-400"
              >
                <span v-if="result.item.value === result.item.title">
                  {{ result.item.value }}
                </span>

                <span v-else class="flex items-center">
                  {{ result.item.title }}
                  <icon-arrow class="w-2 mx-2 transform -rotate-90" />
                  <span class="font-normal opacity-75">
                    {{ result.item.value }}
                  </span>
                </span>
              </g-link>
            </li>
          </ul>
        </div>
        <ul
          class="items-center"
          :class="{
            'hidden lg:flex': type === 'doc',
            flex: type === 'default',
          }"
        >
          <li>
            <a
              href="https://github.com/clastix/capsule"
              target="_blank"
              rel="noopener noreferrer"
            >
              GitHub
            </a>
          </li>
        </ul>
      </div>
    </div>
    <div
      class="
        container
        py-4
        absolute
        bg-gray-900
        inset-x-0
        top-0
        z-30
        shadow-xl
        rounded-br rounded-bl
      "
      v-if="searchOpened && type === 'doc'"
    >
      <div class="flex items-center space-x-4">
        <input
          type="search"
          name="search"
          id="search"
          class="w-full rounded p-2 outline-none text-gray-800"
          placeholder="Search"
          @focus="focused = true"
          @blur="focusOutMobile()"
          @input="query = $event.target.value"
          @change="query = $event.target.value"
        />
        <button @click="toggleSearch()">Cancel</button>
      </div>
      <ul class="p-4 space-y-2" v-show="showResult">
        <li v-if="results.length === 0">
          No results for <span class="font-bold">{{ query }}</span
          >.
        </li>
        <li v-else v-for="result in results" :key="result.id">
          <g-link
            :to="result.item.path + result.item.anchor"
            class="hover:text-blue-400"
          >
            <span v-if="result.item.value === result.item.title">
              {{ result.item.value }}
            </span>

            <span v-else class="flex items-center">
              {{ result.item.title }}
              <span class="block">
                <icon-arrow class="w-2 mx-2 transform -rotate-90" />
              </span>
              <span class="font-normal opacity-75">
                {{ result.item.value }}
              </span>
            </span>
          </g-link>
        </li>
      </ul>
    </div>
  </header>
</template>

<static-query>
query Search {
   allMarkdownPage{
    edges {
      node {
        id
        path
        title
        headings {
        	depth
          value
          anchor
      	}
      }
    }
  }
}
</static-query>

<script>
import Fuse from "fuse.js";

import IconSearch from "~/assets/icon/search.svg?inline";
import IconGithub from "~/assets/icon/github.svg?inline";
import IconTwitter from "~/assets/icon/twitter.svg?inline";
import IconSlack from "~/assets/icon/slack.svg?inline";
import IconArrow from "~/assets/icon/arrow.svg?inline";
import LogoCapsule from "~/assets/logo.svg?inline";

export default {
  props: {
    type: {
      type: String,
      required: false,
      default: () => "default",
      validator: (value) => ["default", "doc"].includes(value),
    },
  },

  components: {
    IconSearch,
    IconGithub,
    IconTwitter,
    IconSlack,
    IconArrow,
    LogoCapsule,
  },

  data() {
    return {
      menuOpened: false,
      searchOpened: false,
      query: "",
      focusIndex: -1,
      focused: false,
    };
  },

  watch: {
    $route: function () {
      if (this.menuOpened) {
        this.menuOpened = false;
        this.$emit("onToggleMenu", this.menuOpened);
        document.querySelector("body").classList.remove("overflow-hidden");
      }
    },
  },

  computed: {
    results() {
      const fuse = new Fuse(this.headings, {
        keys: ["value"],
        threshold: 0.25,
      });
      return fuse.search(this.query).slice(0, 15);
    },

    headings() {
      let result = [];
      const allPages = this.$static.allMarkdownPage.edges.map(
        (edge) => edge.node
      );
      // Create the array of all headings of all pages.
      allPages.forEach((page) => {
        page.headings.forEach((heading) => {
          result.push({
            ...heading,
            path: page.path,
            title: page.title,
          });
        });
      });
      return result;
    },

    showResult() {
      // Show results, if the input is focused and the query is not empty.
      return this.focused && this.query.length > 0;
    },
  },

  methods: {
    toggleMenu() {
      this.menuOpened = !this.menuOpened;
      this.$emit("onToggleMenu", this.menuOpened);
      if (this.menuOpened) {
        document.querySelector("body").classList.add("overflow-hidden");
      } else {
        document.querySelector("body").classList.remove("overflow-hidden");
      }
    },

    toggleSearch() {
      this.searchOpened = !this.searchOpened;
    },

    focusOut() {
      const _this = this;
      setTimeout(function () {
        _this.focused = false;
      }, 200);
    },

    focusOutMobile() {
      const _this = this;
      setTimeout(function () {
        _this.focused = false;
        _this.toggleSearch();
      }, 200);
    },
  },
};
</script>