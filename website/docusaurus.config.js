const lightCodeTheme = require('prism-react-renderer/themes/github');
const darkCodeTheme = require('prism-react-renderer/themes/dracula');

/** @type {import('@docusaurus/types').DocusaurusConfig} */
module.exports = {
  title: 'Aerospike Kubernetes Operator',
  tagline: '',
  url: 'https://aerospike.github.io/',
  baseUrl: '/aerospike-kubernetes-operator/',
  onBrokenLinks: 'throw',
  onBrokenMarkdownLinks: 'throw',
  trailingSlash: true,
  favicon: 'img/favicon.ico',
  organizationName: 'aerospike', // Usually your GitHub org/user name.
  projectName: 'aerospike-kubernetes-operator', // Usually your repo name.
  themeConfig: {
    colorMode: {
      defaultMode: 'light',
      disableSwitch: true,
    },
    navbar: {
      title: 'Aerospike Kubernetes Operator',
      logo: {
        alt: 'Aerospike Kubernetes Operator',
        src: 'img/logo.png',
      },
      items: [
        // {
        //   type: 'doc',
        //   docId: 'intro',
        //   position: 'left',
        //   label: 'Docs',
        // },
        { to: '/blog', label: 'Blog', position: 'left' },
        { type: 'docsVersionDropdown', position: 'right'},
        {
          href: 'https://github.com/aerospike/skyhook',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        // {
        //   title: 'Docs',
        //   items: [
        //     {
        //       label: 'Introduction',
        //       to: '/docs/intro',
        //     },
        //   ],
        // },
        {
          title: 'Community',
          items: [
            {
              label: 'Stack Overflow',
              href: 'https://stackoverflow.com/questions/tagged/aerospike',
            },
            {
              label: 'Twitter',
              href: 'https://twitter.com/aerospikedb',
            },
          ],
        },
        {
          title: 'More',
          items: [
            {
              label: 'Blog',
              to: '/blog',
            },
            {
              label: 'GitHub',
              href: 'https://github.com/aerospike/aerospike-kubernetes-operator',
            },
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} Aerospike`,
    },
    prism: {
      theme: darkCodeTheme,
      darkTheme: darkCodeTheme,
    },
  },
  presets: [
    [
      '@docusaurus/preset-classic',
      {
        docs: {
          sidebarCollapsed: false,
          sidebarPath: require.resolve('./sidebars.js'),
          routeBasePath: '/',
          // Please change this to your repo.
          editUrl: 'https://github.com/aerospike/aerospike-kubernetes-operator/edit/main/website/',
        },
        blog: {
          showReadingTime: true,
          blogSidebarCount: 0,
          // Please change this to your repo.
          editUrl: 'https://github.com/aerospike/aerospike-kubernetes-operator/edit/main/website/blog/',
        },
        theme: {
          customCss: require.resolve('./src/css/custom.css'),
        },
      },
    ],
  ],
};
