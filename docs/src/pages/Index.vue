<template>
  <layout-default>
    <div class="w-full">
      <div class="relative text-center">
        <div
          class="absolute inset-x-0 -bottom-12 h-72 bg-deco opacity-50"
        ></div>
        <div
          class="
            absolute
            inset-x-0
            -bottom-12
            h-72
            bg-gradient-to-b
            from-gray-800
            to-transparent
          "
        ></div>
        <div class="container relative">
          <div class="lg:w-7/12 mx-auto mb-28">
            <logo-capsule class="w-32 lg:w-48 mx-auto mb-4" />
            <h1 class="text-4xl lg:text-6xl font-bold mb-8">capsule</h1>
            <p class="text-lg lg:text-2xl mb-8">
              A multi-tenancy and policy-based
              framework for Kubernetes
            </p>
            <div>
              <app-button link="/docs">Get Started</app-button>
            </div>
          </div>
        </div>
      </div>
      <div class="container">
        <div
          class="
            2xl:w-10/12
            mx-auto
            grid grid-cols-1
            md:grid-cols-2
            lg:grid-cols-3
            gap-8
            lg:gap-16
            mb-16
          "
        >
          <div class="bg-gray-900 bg-opacity-20 p-5 rounded-md shadow-lg">
            <h2 class="text-xl lg:text-3xl font-semibold mb-2.5">
              Self-Service
            </h2>
            <p class="lg:text-lg">
              Leave developers the freedom to self-provision their cluster
              resources according to the assigned boundaries.
            </p>
          </div>

          <div class="bg-gray-900 bg-opacity-20 p-5 rounded-md shadow-lg">
            <h2 class="text-xl lg:text-3xl font-semibold mb-2.5">
              Preventing Clusters Sprawl
            </h2>
            <p class="lg:text-lg">
              Share a single cluster with multiple teams, groups of users, or
              departments by saving operational and management efforts.
            </p>
          </div>
          <div class="bg-gray-900 bg-opacity-20 p-5 rounded-md shadow-lg">
            <h2 class="text-xl lg:text-3xl font-semibold mb-2.5">Governance</h2>
            <p class="lg:text-lg">
              Leverage Kubernetes Admission Controllers to enforce the industry
              security best practices and meet policy requirements.
            </p>
          </div>
          <div class="bg-gray-900 bg-opacity-20 p-5 rounded-md shadow-lg">
            <h2 class="text-xl lg:text-3xl font-semibold mb-2.5">
              Resources Control
            </h2>
            <p class="lg:text-lg">
              Take control of the resources consumed by users while preventing
              them to overtake.
            </p>
          </div>
          <div class="bg-gray-900 bg-opacity-20 p-5 rounded-md shadow-lg">
            <h2 class="text-xl lg:text-3xl font-semibold mb-2.5">
              Native Experience
            </h2>
            <p class="lg:text-lg">
              Provide multi-tenancy with a native Kubernetes experience without
              introducing additional management layers, plugins, or customized
              binaries.
            </p>
          </div>
          <div class="bg-gray-900 bg-opacity-20 p-5 rounded-md shadow-lg">
            <h2 class="text-xl lg:text-3xl font-semibold mb-2.5">
              GitOps ready
            </h2>
            <p class="lg:text-lg">
              Capsule is completely declarative and GitOps ready.
            </p>
          </div>
        </div>
      </div>
    </div>
  </layout-default>
</template>

<page-query>
query {
  metadata {
    siteDescription
  }
}
</page-query>

<script>
import AppButton from "~/components/AppButton.vue";
import LogoCapsule from "~/assets/logo.svg?inline";
export default {
  metaInfo() {
    return {
      title: "Capsule: Kubernetes Operator for multi-tenancy",
      meta: [
        {
          property: "og:title",
          content:
            "Capsule Documentation | Capsule: Kubernetes Operator for multi-tenancy",
        },
        {
          property: "og:description",
          content: this.$page.metadata.siteDescription,
        },
        {
          property: "og:image",
          content:
            "https://quizzical-roentgen-574926.netlify.app/assets/share.png",
        },
        {
          property: "twitter:card",
          content: "summary",
        },
        {
          property: "twitter:title",
          content:
            "Capsule Documentation | Capsule: Kubernetes Operator for multi-tenancy",
        },
        {
          property: "twitter:description",
          content: this.$page.metadata.siteDescription,
        },
        {
          property: "og:url",
          content: "https://capsule.clastix.io/",
        },
      ],
    };
  },

  components: {
    AppButton,
    LogoCapsule,
  },
};
</script>

<style lang="scss" scoped>
.bg-deco {
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 2000 1500'%3E%3Cdefs%3E%3Crect stroke='%231F2937' stroke-width='0.5' width='1' height='1' id='s'/%3E%3Cpattern id='a' width='3' height='3' patternUnits='userSpaceOnUse' patternTransform='scale(12) translate(-916.67 -687.5)'%3E%3Cuse fill='%232f3641' href='%23s' y='2'/%3E%3Cuse fill='%232f3641' href='%23s' x='1' y='2'/%3E%3Cuse fill='%233b414a' href='%23s' x='2' y='2'/%3E%3Cuse fill='%233b414a' href='%23s'/%3E%3Cuse fill='%23454a52' href='%23s' x='2'/%3E%3Cuse fill='%23454a52' href='%23s' x='1' y='1'/%3E%3C/pattern%3E%3Cpattern id='b' width='7' height='11' patternUnits='userSpaceOnUse' patternTransform='scale(12) translate(-916.67 -687.5)'%3E%3Cg fill='%234e5259'%3E%3Cuse href='%23s'/%3E%3Cuse href='%23s' y='5' /%3E%3Cuse href='%23s' x='1' y='10'/%3E%3Cuse href='%23s' x='2' y='1'/%3E%3Cuse href='%23s' x='2' y='4'/%3E%3Cuse href='%23s' x='3' y='8'/%3E%3Cuse href='%23s' x='4' y='3'/%3E%3Cuse href='%23s' x='4' y='7'/%3E%3Cuse href='%23s' x='5' y='2'/%3E%3Cuse href='%23s' x='5' y='6'/%3E%3Cuse href='%23s' x='6' y='9'/%3E%3C/g%3E%3C/pattern%3E%3Cpattern id='h' width='5' height='13' patternUnits='userSpaceOnUse' patternTransform='scale(12) translate(-916.67 -687.5)'%3E%3Cg fill='%234e5259'%3E%3Cuse href='%23s' y='5'/%3E%3Cuse href='%23s' y='8'/%3E%3Cuse href='%23s' x='1' y='1'/%3E%3Cuse href='%23s' x='1' y='9'/%3E%3Cuse href='%23s' x='1' y='12'/%3E%3Cuse href='%23s' x='2'/%3E%3Cuse href='%23s' x='2' y='4'/%3E%3Cuse href='%23s' x='3' y='2'/%3E%3Cuse href='%23s' x='3' y='6'/%3E%3Cuse href='%23s' x='3' y='11'/%3E%3Cuse href='%23s' x='4' y='3'/%3E%3Cuse href='%23s' x='4' y='7'/%3E%3Cuse href='%23s' x='4' y='10'/%3E%3C/g%3E%3C/pattern%3E%3Cpattern id='c' width='17' height='13' patternUnits='userSpaceOnUse' patternTransform='scale(12) translate(-916.67 -687.5)'%3E%3Cg fill='%23565a60'%3E%3Cuse href='%23s' y='11'/%3E%3Cuse href='%23s' x='2' y='9'/%3E%3Cuse href='%23s' x='5' y='12'/%3E%3Cuse href='%23s' x='9' y='4'/%3E%3Cuse href='%23s' x='12' y='1'/%3E%3Cuse href='%23s' x='16' y='6'/%3E%3C/g%3E%3C/pattern%3E%3Cpattern id='d' width='19' height='17' patternUnits='userSpaceOnUse' patternTransform='scale(12) translate(-916.67 -687.5)'%3E%3Cg fill='%231F2937'%3E%3Cuse href='%23s' y='9'/%3E%3Cuse href='%23s' x='16' y='5'/%3E%3Cuse href='%23s' x='14' y='2'/%3E%3Cuse href='%23s' x='11' y='11'/%3E%3Cuse href='%23s' x='6' y='14'/%3E%3C/g%3E%3Cg fill='%235d6066'%3E%3Cuse href='%23s' x='3' y='13'/%3E%3Cuse href='%23s' x='9' y='7'/%3E%3Cuse href='%23s' x='13' y='10'/%3E%3Cuse href='%23s' x='15' y='4'/%3E%3Cuse href='%23s' x='18' y='1'/%3E%3C/g%3E%3C/pattern%3E%3Cpattern id='e' width='47' height='53' patternUnits='userSpaceOnUse' patternTransform='scale(12) translate(-916.67 -687.5)'%3E%3Cg fill='%235783AB'%3E%3Cuse href='%23s' x='2' y='5'/%3E%3Cuse href='%23s' x='16' y='38'/%3E%3Cuse href='%23s' x='46' y='42'/%3E%3Cuse href='%23s' x='29' y='20'/%3E%3C/g%3E%3C/pattern%3E%3Cpattern id='f' width='59' height='71' patternUnits='userSpaceOnUse' patternTransform='scale(12) translate(-916.67 -687.5)'%3E%3Cg fill='%235783AB'%3E%3Cuse href='%23s' x='33' y='13'/%3E%3Cuse href='%23s' x='27' y='54'/%3E%3Cuse href='%23s' x='55' y='55'/%3E%3C/g%3E%3C/pattern%3E%3Cpattern id='g' width='139' height='97' patternUnits='userSpaceOnUse' patternTransform='scale(12) translate(-916.67 -687.5)'%3E%3Cg fill='%235783AB'%3E%3Cuse href='%23s' x='11' y='8'/%3E%3Cuse href='%23s' x='51' y='13'/%3E%3Cuse href='%23s' x='17' y='73'/%3E%3Cuse href='%23s' x='99' y='57'/%3E%3C/g%3E%3C/pattern%3E%3C/defs%3E%3Crect fill='url(%23a)' width='100%25' height='100%25'/%3E%3Crect fill='url(%23b)' width='100%25' height='100%25'/%3E%3Crect fill='url(%23h)' width='100%25' height='100%25'/%3E%3Crect fill='url(%23c)' width='100%25' height='100%25'/%3E%3Crect fill='url(%23d)' width='100%25' height='100%25'/%3E%3Crect fill='url(%23e)' width='100%25' height='100%25'/%3E%3Crect fill='url(%23f)' width='100%25' height='100%25'/%3E%3Crect fill='url(%23g)' width='100%25' height='100%25'/%3E%3C/svg%3E");
  // background-image: url("data:image/svg+xml,%3csvg xmlns='http://www.w3.org/2000/svg' version='1.1' xmlns:xlink='http://www.w3.org/1999/xlink' xmlns:svgjs='http://svgjs.com/svgjs' width='1440' height='250' preserveAspectRatio='none' viewBox='0 0 1440 250'%3e%3cg mask='url(%26quot%3b%23SvgjsMask1012%26quot%3b)' fill='none'%3e%3crect width='1440' height='250' x='0' y='0' fill='rgba(31%2c 41%2c 55%2c 1)'%3e%3c/rect%3e%3cpath d='M36 250L286 0L571 0L321 250z' fill='url(%23SvgjsLinearGradient1013)'%3e%3c/path%3e%3cpath d='M264.6 250L514.6 0L678.1 0L428.1 250z' fill='url(%23SvgjsLinearGradient1013)'%3e%3c/path%3e%3cpath d='M507.20000000000005 250L757.2 0L1073.7 0L823.7 250z' fill='url(%23SvgjsLinearGradient1013)'%3e%3c/path%3e%3cpath d='M725.8000000000001 250L975.8000000000001 0L1302.8000000000002 0L1052.8000000000002 250z' fill='url(%23SvgjsLinearGradient1013)'%3e%3c/path%3e%3cpath d='M1439 250L1189 0L995.5 0L1245.5 250z' fill='url(%23SvgjsLinearGradient1014)'%3e%3c/path%3e%3cpath d='M1157.4 250L907.4000000000001 0L817.4000000000001 0L1067.4 250z' fill='url(%23SvgjsLinearGradient1014)'%3e%3c/path%3e%3cpath d='M961.8 250L711.8 0L383.29999999999995 0L633.3 250z' fill='url(%23SvgjsLinearGradient1014)'%3e%3c/path%3e%3cpath d='M688.1999999999999 250L438.19999999999993 0L208.69999999999993 0L458.69999999999993 250z' fill='url(%23SvgjsLinearGradient1014)'%3e%3c/path%3e%3cpath d='M1247.2258701549645 250L1440 57.225870154964355L1440 250z' fill='url(%23SvgjsLinearGradient1013)'%3e%3c/path%3e%3cpath d='M0 250L192.77412984503565 250L 0 57.225870154964355z' fill='url(%23SvgjsLinearGradient1014)'%3e%3c/path%3e%3c/g%3e%3cdefs%3e%3cmask id='SvgjsMask1012'%3e%3crect width='1440' height='250' fill='white'%3e%3c/rect%3e%3c/mask%3e%3clinearGradient x1='0%25' y1='100%25' x2='100%25' y2='0%25' id='SvgjsLinearGradient1013'%3e%3cstop stop-color='rgba(87%2c 131%2c 171%2c 0.14)' offset='0'%3e%3c/stop%3e%3cstop stop-opacity='0' stop-color='rgba(87%2c 131%2c 171%2c 0.14)' offset='0.66'%3e%3c/stop%3e%3c/linearGradient%3e%3clinearGradient x1='100%25' y1='100%25' x2='0%25' y2='0%25' id='SvgjsLinearGradient1014'%3e%3cstop stop-color='rgba(87%2c 131%2c 171%2c 0.14)' offset='0'%3e%3c/stop%3e%3cstop stop-opacity='0' stop-color='rgba(87%2c 131%2c 171%2c 0.14)' offset='0.66'%3e%3c/stop%3e%3c/linearGradient%3e%3c/defs%3e%3c/svg%3e");
  background-size: cover;
}
</style>