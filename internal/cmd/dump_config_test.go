/*
Copyright 2025 YANDEX LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/viper"
	"github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/util/walg"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Helper function to create a fake client with the given objects
func setupFakeBackupClient(objs ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(v1beta1.AddToScheme(scheme)).To(Succeed())
	Expect(cnpgv1.AddToScheme(scheme)).To(Succeed())
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

// Helper function to create a test BackupConfig
func createTestBackupConfig(name, namespace string) *v1beta1.BackupConfig {
	return &v1beta1.BackupConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "BackupConfig",
			APIVersion: "cnpg-extensions.yandex.cloud/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1beta1.BackupConfigSpec{
			Storage: v1beta1.StorageConfig{
				StorageType: v1beta1.StorageTypeS3,
				S3: &v1beta1.S3StorageConfig{
					Prefix:      "s3://test-bucket/test-prefix",
					Region:      "us-east-1",
					EndpointURL: "https://s3.amazonaws.com",
					AccessKeyIDRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-secret",
						},
						Key: "access-key-id",
					},
					AccessKeySecretRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-secret",
						},
						Key: "access-key-secret",
					},
				},
			},
		},
	}
}

// Helper function to create a test Secret
func createTestSecret(name, namespace string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"access-key-id":     []byte("test-access-key-id"),
			"access-key-secret": []byte("test-access-key-secret"),
		},
	}
}

// Helper function to verify config file content
func verifyConfigFileContent(filePath string) {
	// Verify the file was created
	fileInfo, err := os.Stat(filePath)
	Expect(err).NotTo(HaveOccurred())
	Expect(fileInfo.Size()).To(BeNumerically(">", 0))

	// Read the file and verify it contains expected content
	content, err := os.ReadFile(filePath)
	Expect(err).NotTo(HaveOccurred())

	// Parse the JSON content
	var config walg.Config
	err = json.Unmarshal(content, &config)
	Expect(err).NotTo(HaveOccurred())

	// Verify the config has the expected values
	Expect(config.AWSAccessKeyID).To(Equal("test-access-key-id"))
	Expect(config.AWSSecretAccessKey).To(Equal("test-access-key-secret"))
	Expect(config.AWSRegion).To(Equal("us-east-1"))
	Expect(config.AWSEndpoint).To(Equal("https://s3.amazonaws.com"))
	Expect(config.WaleS3Prefix).To(Equal("s3://test-bucket/test-prefix"))
}

var _ = Describe("DumpConfig", func() {
	var (
		ctx           context.Context
		fakeClient    client.Client
		testNamespace string
		tempDir       string
	)

	BeforeEach(func() {
		ctx = context.Background()
		testNamespace = "test-namespace"

		// Create a temporary directory for test output files
		var err error
		tempDir, err = os.MkdirTemp("", "dump-config-test")
		Expect(err).NotTo(HaveOccurred())

		// Reset viper values before each test
		viper.Reset()
	})

	AfterEach(func() {
		// Clean up temporary directory
		os.RemoveAll(tempDir)
	})

	Describe("runDumpConfig", func() {
		BeforeEach(func() {
			// Create test objects
			backupConfig := createTestBackupConfig("test-backup-config", testNamespace)
			secret := createTestSecret("test-secret", testNamespace)

			// Create test objects in the default namespace
			backupConfigDefaultNS := createTestBackupConfig("test-backup-config-default", "default")
			secretDefaultNS := createTestSecret("test-secret", "default")

			// Initialize fake client with the test objects
			fakeClient = setupFakeBackupClient(backupConfig, secret, backupConfigDefaultNS, secretDefaultNS)
		})

		It("should correctly parse namespace and name from the backup-config parameter", func() {
			viper.Set("backup-config", testNamespace+"/test-backup-config")
			viper.Set("output", filepath.Join(tempDir, "config.json"))

			err := runDumpConfig(ctx, fakeClient)
			Expect(err).NotTo(HaveOccurred())

			// Verify the file was created
			_, err = os.Stat(filepath.Join(tempDir, "config.json"))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should use the default namespace if not specified", func() {
			// Add the objects to the fake client
			viper.Set("backup-config", "test-backup-config-default")
			viper.Set("output", filepath.Join(tempDir, "config.json"))

			err := runDumpConfig(ctx, fakeClient)
			Expect(err).NotTo(HaveOccurred())

			// Verify the file was created
			_, err = os.Stat(filepath.Join(tempDir, "config.json"))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return an error if it fails to get BackupConfig", func() {
			viper.Set("backup-config", testNamespace+"/non-existent-config")
			viper.Set("output", filepath.Join(tempDir, "config.json"))

			err := runDumpConfig(ctx, fakeClient)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get BackupConfig"))
		})

		It("should successfully write the config to a file", func() {
			viper.Set("backup-config", testNamespace+"/test-backup-config")
			outputPath := filepath.Join(tempDir, "config.json")
			viper.Set("output", outputPath)

			err := runDumpConfig(ctx, fakeClient)
			Expect(err).NotTo(HaveOccurred())

			// Use the helper function to verify file content
			verifyConfigFileContent(outputPath)
		})
	})
})
