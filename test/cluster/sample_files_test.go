package cluster

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/yaml"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

const fileDir = "config/samples"

var _ = Describe("Sample files validation", func() {
	var (
		aeroCluster = &asdbv1.AerospikeCluster{}
		ctx         = context.TODO()
		err         error
	)

	AfterEach(func() {
		Expect(deleteCluster(k8sClient, ctx, aeroCluster)).NotTo(HaveOccurred())
	})

	DescribeTable("Sample files validation",
		func(filePath string) {
			aeroCluster, err = deployClusterUsingFile(ctx, filePath)
			Expect(err).NotTo(HaveOccurred())
		}, getEntries(),
	)

	Context("XDR sample files validation", func() {

		It("XDR sample files validation", func() {
			var destCluster *asdbv1.AerospikeCluster

			sourceClusterFile := filepath.Join(projectRoot, fileDir, "xdr_src_cluster_cr.yaml")
			destClusterFile := filepath.Join(projectRoot, fileDir, "xdr_dst_cluster_cr.yaml")

			By("Creating XDR destination cluster")
			destCluster, err = deployClusterUsingFile(ctx, destClusterFile)
			Expect(err).NotTo(HaveOccurred())

			defer func() {
				Expect(deleteCluster(k8sClient, ctx, destCluster)).NotTo(HaveOccurred())
			}()

			By("Creating XDR source cluster")
			aeroCluster, err = deployClusterUsingFile(ctx, sourceClusterFile)
			Expect(err).NotTo(HaveOccurred())

			By("Writing some data in source cluster")
			aeroCluster, err = getCluster(k8sClient, ctx, types.NamespacedName{
				Name:      aeroCluster.Name,
				Namespace: aeroCluster.Namespace,
			})
			Expect(err).NotTo(HaveOccurred())

			err = WriteDataToCluster(
				aeroCluster, k8sClient, []string{"test"},
			)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying data in destination cluster")
			destCluster, err = getCluster(k8sClient, ctx, types.NamespacedName{
				Name:      destCluster.Name,
				Namespace: destCluster.Namespace,
			})
			Expect(err).NotTo(HaveOccurred())

			records, err := CheckDataInCluster(
				destCluster, k8sClient, []string{"test"},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(records).NotTo(BeEmpty())

			for namespace, recordExists := range records {
				Expect(recordExists).To(
					BeTrue(), fmt.Sprintf(
						"Namespace: %s - should have records",
						namespace,
					),
				)
			}
		})
	})
})

func getEntries() []TableEntry {
	// To recover panic failures during testing tree construction phase
	defer GinkgoRecover()

	files, err := getSamplesFiles()
	if err != nil {
		Fail(err.Error())
	}

	tableEntries := make([]TableEntry, 0, len(files))
	for idx := range files {
		tableEntries = append(tableEntries, Entry(fmt.Sprintf("Testing sample file - %s", files[idx]), files[idx]))
	}

	return tableEntries
}

func getSamplesFiles() ([]string, error) {
	var (
		files []string
		err   error
	)

	// getGitRepoRootPath is called here explicitly to get ProjectRoot at this point
	// This may be empty if getSamplesFiles is called during var initialization phase
	if projectRoot == "" {
		projectRoot, err = getGitRepoRootPath()
		if err != nil {
			return nil, err
		}
	}

	absolutePath := filepath.Join(projectRoot, fileDir)

	// Files/Dirs ignored are:
	// 1. PMEM sample file as hardware is not available
	// 2. XDR related files as they are separately tested
	// 3. All files which are not CR samples
	// 4. BackupService, Backup and Restore related files
	ignoreFiles := sets.New[string](
		"pmem_cluster_cr.yaml",
		"xdr_dst_cluster_cr.yaml",
		"xdr_src_cluster_cr.yaml",
		"aerospikebackup.yaml",
		"aerospikebackupservice.yaml",
		"aerospikerestore.yaml",
	)

	if err := filepath.Walk(absolutePath, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// ignore directories
		if info.IsDir() {
			return nil
		}

		parts := strings.Split(path, "/")
		file := parts[len(parts)-1]

		if ignoreFiles.Has(file) || !strings.HasSuffix(path, "_cr.yaml") {
			return nil
		}

		files = append(files, filepath.Join(absolutePath, info.Name()))

		return nil
	}); err != nil {
		return nil, err
	}

	return files, nil
}

func deployClusterUsingFile(ctx context.Context, filePath string) (*asdbv1.AerospikeCluster, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	aeroCluster := &asdbv1.AerospikeCluster{}

	if err := yaml.UnmarshalStrict(data, aeroCluster); err != nil {
		return aeroCluster, err
	}

	if !strings.Contains(filePath, "xdr") {
		baseName := strings.TrimSuffix(filepath.Base(filePath), "_cr.yaml")
		aeroCluster.Name = strings.ReplaceAll(baseName, "_", "-")
	}

	// Deploy the cluster
	if err := DeployCluster(k8sClient, ctx, aeroCluster); err != nil {
		return aeroCluster, err
	}

	return aeroCluster, nil
}
