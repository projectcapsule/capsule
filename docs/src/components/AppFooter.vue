<template>
  <footer class="py-8 bg-gray-900 bg-opacity-50">
    <div
      class="
        container
        lg:flex lg:items-center lg:justify-between
        space-y-4
        lg:space-y-0
      "
    >
      <div class="space-y-2">
        <h1 class="font-bold text-2xl inline-block">Clastix Labs</h1>
        <small class="block">
          © {{ new Date().getFullYear() }} All rights reserved
        </small>
      </div>
      <ul class="lg:flex lg:items-center lg:space-x-5 space-y-2 lg:space-y-0">
        <li>
          <a
            href="https://github.com/clastix/capsule"
            target="_blank"
            rel="noopener noreferrer"
            class="flex space-x-2 hover:text-blue-400"
            ><icon-github class="w-5 h-5" /> <span> GitHub </span></a
          >
        </li>
        <li>
          <a
            href="https://twitter.com/clastixio"
            target="_blank"
            rel="noopener noreferrer"
            class="flex space-x-2 hover:text-blue-400"
            ><icon-twitter class="w-5 h-5" /> <span>Twitter</span></a
          >
        </li>
        <li>
          <a
            href="https://www.linkedin.com/company/clastix/"
            target="_blank"
            rel="noopener noreferrer"
            class="flex space-x-2 hover:text-blue-400"
            ><icon-linkedin class="w-5 h-5" /> <span>Linkedin</span></a
          >
        </li>
      </ul>
    </div>
  </footer>
</template>

<script>
import IconGithub from "~/assets/icon/github.svg?inline";
import IconTwitter from "~/assets/icon/twitter.svg?inline";
import IconLinkedin from "~/assets/icon/linkedin.svg?inline";

export default {
  components: {
    IconGithub,
    IconLinkedin,
    IconTwitter,
  },
};
</script>

<style>
</style>