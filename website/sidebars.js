/**
 * Creating a sidebar enables you to:
 - create an ordered group of docs
 - render a sidebar for each doc of that group
 - provide next/previous navigation

 The sidebars can be generated from the filesystem, or explicitly defined here.

 Create as many sidebars as you want.

 */
module.exports = {
  // By default, Docusaurus generates a sidebar from the docs folder structure
  docsSidebar: [
    'Intro',
    {
      type: 'category',
      label: 'Getting Started',
      items: ['System-Requirements', 'Install-the-Operator-on-Kubernetes', 'Create-Aerospike-cluster', 'Connect-to-the-Aerospike-cluster', 'Troubleshooting', 'Limitations']
    },
    {
      type: 'category',
      label: 'Configuration', 
      items: ['Cluster-configuration-settings', 'Aerospike-configuration-mapping', 'Rack-Awareness', 
      'Multiple-Aerospike-clusters', 'Monitoring', 'XDR', 'Aerospike-access-control']
    },
    {
      type: 'category',
      label: 'Management', 
      items: ['Aerospike-configuration-change','Version-upgrade','Scaling','Kubernetes-Secrets',
      'Delete-Aerospike-cluster','Manage-TLS-Certificates']
    },
    {
      type: 'category',
      label: 'Storage', 
      items: ['Storage-provisioning','Scaling-namespace-storage']
    },
    {
      type: 'category',
      label: 'Storage examples', 
      items: ['Data-in-memory','Data-on-SSD','HDD-storage-with-data-in-index','HDD-storage-with-data-in-memory']
    },

  ]

}