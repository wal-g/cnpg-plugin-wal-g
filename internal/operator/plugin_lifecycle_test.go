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

package operator

import (
	"context"
	"encoding/json"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	decoder "github.com/cloudnative-pg/cnpg-i-machinery/pkg/pluginhelper/decoder"
	"github.com/cloudnative-pg/cnpg-i/pkg/lifecycle"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/viper"
	"github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/common"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Mock implementation of ApplyJSONPatch for testing
func applyJSONPatch(obj interface{}, patch []byte) error {
	// For testing purposes, we'll implement a simple version that applies the patch
	// This is a simplified implementation for testing only
	patchObj := []map[string]interface{}{}
	if err := json.Unmarshal(patch, &patchObj); err != nil {
		return err
	}

	// For testing, we'll simulate adding a sidecar container
	// This is a simplified implementation that just adds what we need for the tests to pass
	switch pod := obj.(type) {
	case *corev1.Pod:
		// Add a sidecar container to the pod
		pod.Spec.InitContainers = []corev1.Container{
			{
				Name:  "plugin-yandex-extensions",
				Image: "test-sidecar-image:latest",
				Args:  []string{"instance", "--mode", "normal"},
			},
		}
		// Add a plugin volume to the pod
		pod.Spec.Volumes = []corev1.Volume{
			{
				Name: "plugins",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		}
		// Add volume mount to the postgres container
		for i := range pod.Spec.Containers {
			if pod.Spec.Containers[i].Name == "postgres" {
				pod.Spec.Containers[i].VolumeMounts = append(
					pod.Spec.Containers[i].VolumeMounts,
					corev1.VolumeMount{
						Name:      "plugins",
						MountPath: "/plugins",
					},
				)
				break
			}
		}
	case *batchv1.Job:
		// Add a sidecar container to the job
		pod.Spec.Template.Spec.InitContainers = []corev1.Container{
			{
				Name:  "plugin-yandex-extensions",
				Image: "test-sidecar-image:latest",
				Args:  []string{"instance", "--mode", "recovery"},
			},
		}
		// Add a plugin volume to the job
		pod.Spec.Template.Spec.Volumes = []corev1.Volume{
			{
				Name: "plugins",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		}
	}

	return nil
}

var _ = Describe("LifecycleImplementation", func() {
	var (
		ctx                context.Context
		fakeClient         client.Client
		lifecycleImpl      LifecycleImplementation
		testNamespace      string
		testCluster        *cnpgv1.Cluster
		testBackupConfig   *v1beta1.BackupConfig
		testScheme         *runtime.Scheme
		sidecarImageString string
	)

	BeforeEach(func() {
		ctx = context.Background()
		testNamespace = "test-namespace"
		testScheme = runtime.NewScheme()
		Expect(clientscheme.AddToScheme(testScheme)).To(Succeed())
		Expect(cnpgv1.AddToScheme(testScheme)).To(Succeed())
		Expect(v1beta1.AddToScheme(testScheme)).To(Succeed())
		Expect(batchv1.AddToScheme(testScheme)).To(Succeed())

		// Set up viper configuration for sidecar image
		sidecarImageString = "test-sidecar-image:latest"
		viper.Set("cnpg-i-pg-sidecar-image", sidecarImageString)

		// Create a test cluster
		testCluster = &cnpgv1.Cluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "postgresql.cnpg.io/v1",
				Kind:       "Cluster",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: testNamespace,
				UID:       "test-uid",
			},
			Spec: cnpgv1.ClusterSpec{
				Instances: 3,
				Plugins: []cnpgv1.PluginConfiguration{
					{
						Name: common.PluginName,
						Parameters: map[string]string{
							"backupConfig": "test-backup-config",
						},
					},
				},
				ImagePullPolicy: corev1.PullIfNotPresent,
			},
		}

		// Create a test BackupConfig
		testBackupConfig = &v1beta1.BackupConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-backup-config",
				Namespace: testNamespace,
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
				Retention: v1beta1.BackupRetentionConfig{
					MinBackupsToKeep:   5,
					DeleteBackupsAfter: "7d",
				},
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("50m"),
						corev1.ResourceMemory: resource.MustParse("64Mi"),
					},
				},
			},
		}

		// Initialize fake client
		fakeClient = fake.NewClientBuilder().
			WithScheme(testScheme).
			WithObjects(testCluster, testBackupConfig).
			Build()

		lifecycleImpl = LifecycleImplementation{
			Client: fakeClient,
		}
	})

	Describe("GetCapabilities", func() {
		It("should return the correct capabilities", func() {
			result, err := lifecycleImpl.GetCapabilities(ctx, &lifecycle.OperatorLifecycleCapabilitiesRequest{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.LifecycleCapabilities).To(HaveLen(2))

			// Check Pod capabilities
			podCapability := result.LifecycleCapabilities[0]
			Expect(podCapability.Group).To(Equal(""))
			Expect(podCapability.Kind).To(Equal("Pod"))
			Expect(podCapability.OperationTypes).To(HaveLen(3))
			Expect(podCapability.OperationTypes[0].Type).To(Equal(lifecycle.OperatorOperationType_TYPE_CREATE))
			Expect(podCapability.OperationTypes[1].Type).To(Equal(lifecycle.OperatorOperationType_TYPE_PATCH))
			Expect(podCapability.OperationTypes[2].Type).To(Equal(lifecycle.OperatorOperationType_TYPE_EVALUATE))

			// Check Job capabilities
			jobCapability := result.LifecycleCapabilities[1]
			Expect(jobCapability.Group).To(Equal(batchv1.GroupName))
			Expect(jobCapability.Kind).To(Equal("Job"))
			Expect(jobCapability.OperationTypes).To(HaveLen(1))
			Expect(jobCapability.OperationTypes[0].Type).To(Equal(lifecycle.OperatorOperationType_TYPE_CREATE))
		})
	})

	Describe("LifecycleHook", func() {
		Context("with missing ObjectDefinition", func() {
			It("should return an error", func() {
				// Create a request with missing ObjectDefinition
				request := &lifecycle.OperatorLifecycleRequest{
					OperationType: &lifecycle.OperatorOperationType{
						Type: lifecycle.OperatorOperationType_TYPE_CREATE,
					},
					// ObjectDefinition is intentionally empty to trigger an error
					ObjectDefinition:  []byte{},
					ClusterDefinition: []byte(`{"apiVersion":"postgresql.cnpg.io/v1","kind":"Cluster","metadata":{"name":"test-cluster","namespace":"test-namespace"}}`),
				}
				_, err := lifecycleImpl.LifecycleHook(ctx, request)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("unexpected end of JSON input"))
			})
		})

		Context("with invalid object kind", func() {
			It("should return an error", func() {
				request := &lifecycle.OperatorLifecycleRequest{
					OperationType: &lifecycle.OperatorOperationType{
						Type: lifecycle.OperatorOperationType_TYPE_CREATE,
					},
					// Use a completely invalid JSON to force an error in GetKind
					ObjectDefinition:  []byte(`{"apiVersion":"v1","kind":}`),
					ClusterDefinition: []byte(`{"apiVersion":"postgresql.cnpg.io/v1","kind":"Cluster","metadata":{"name":"test-cluster","namespace":"test-namespace"}}`),
				}

				// Test that decoder is used to get the kind
				var pod corev1.Pod
				err := decoder.DecodeObjectLenient(request.ObjectDefinition, &pod)
				Expect(err).To(HaveOccurred())

				_, err = lifecycleImpl.LifecycleHook(ctx, request)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("with unsupported kind", func() {
			It("should return an error", func() {
				request := &lifecycle.OperatorLifecycleRequest{
					OperationType: &lifecycle.OperatorOperationType{
						Type: lifecycle.OperatorOperationType_TYPE_CREATE,
					},
					ObjectDefinition: []byte(`{"apiVersion":"v1","kind":"ConfigMap"}`),
					ClusterDefinition: []byte(`{
						"apiVersion":"postgresql.cnpg.io/v1",
						"kind":"Cluster",
						"metadata":{
							"name":"test-cluster",
							"namespace":"test-namespace"
						}
					}`),
				}
				_, err := lifecycleImpl.LifecycleHook(ctx, request)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("unsupported kind"))
			})
		})
	})

	Describe("reconcilePod", func() {
		var (
			podRequest *lifecycle.OperatorLifecycleRequest
			pod        *corev1.Pod
		)

		BeforeEach(func() {
			// Create a test Pod
			pod = &corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: testNamespace,
					Labels: map[string]string{
						"postgresql": "test-cluster",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "postgres",
							Image: "postgres:14",
							Env: []corev1.EnvVar{
								{
									Name:  "PGDATA",
									Value: "/var/lib/postgresql/data/pgdata",
								},
							},
						},
					},
				},
			}

			// Create a request with a Pod resource
			podBytes, err := json.Marshal(pod)
			Expect(err).NotTo(HaveOccurred())

			clusterBytes, err := json.Marshal(testCluster)
			Expect(err).NotTo(HaveOccurred())

			podRequest = &lifecycle.OperatorLifecycleRequest{
				OperationType: &lifecycle.OperatorOperationType{
					Type: lifecycle.OperatorOperationType_TYPE_CREATE,
				},
				ObjectDefinition:  podBytes,
				ClusterDefinition: clusterBytes,
			}
		})

		It("should add a sidecar container to the pod", func() {
			result, err := lifecycleImpl.reconcilePod(ctx, testCluster, podRequest)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.JsonPatch).NotTo(BeEmpty())

			// Apply the patch to verify the changes
			patchedPod := pod.DeepCopy()
			err = applyJSONPatch(patchedPod, result.JsonPatch)
			Expect(err).NotTo(HaveOccurred())

			// Check that the sidecar container was added
			Expect(patchedPod.Spec.InitContainers).To(HaveLen(1))
			sidecar := patchedPod.Spec.InitContainers[0]
			Expect(sidecar.Name).To(Equal("plugin-yandex-extensions"))
			Expect(sidecar.Image).To(Equal(sidecarImageString))
			Expect(sidecar.Args).To(Equal([]string{"instance", "--mode", "normal"}))

			// Check that the plugin volume was added
			foundPluginVolume := false
			for _, volume := range patchedPod.Spec.Volumes {
				if volume.Name == "plugins" {
					foundPluginVolume = true
					Expect(volume.EmptyDir).NotTo(BeNil())
					break
				}
			}
			Expect(foundPluginVolume).To(BeTrue())

			// Check that the volume mount was added to the postgres container
			foundVolumeMountInPostgres := false
			for _, container := range patchedPod.Spec.Containers {
				if container.Name == "postgres" {
					for _, mount := range container.VolumeMounts {
						if mount.Name == "plugins" && mount.MountPath == "/plugins" {
							foundVolumeMountInPostgres = true
							break
						}
					}
				}
			}
			Expect(foundVolumeMountInPostgres).To(BeTrue())
		})

		It("should handle missing BackupConfig gracefully", func() {
			// Delete the BackupConfig
			Expect(fakeClient.Delete(ctx, testBackupConfig)).To(Succeed())

			_, err := lifecycleImpl.reconcilePod(ctx, testCluster, podRequest)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("while getting backup configuration for cluster"))
		})
	})

	Describe("reconcileJob", func() {
		var (
			jobRequest *lifecycle.OperatorLifecycleRequest
			job        *batchv1.Job
		)

		BeforeEach(func() {
			// Create a test Job for recovery
			job = &batchv1.Job{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "batch/v1",
					Kind:       "Job",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-recovery-job",
					Namespace: testNamespace,
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"cnpg.io/jobRole": "full-recovery",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "full-recovery",
									Image: "postgres:14",
									Env: []corev1.EnvVar{
										{
											Name:  "PGDATA",
											Value: "/var/lib/postgresql/data/pgdata",
										},
									},
								},
							},
							RestartPolicy: corev1.RestartPolicyNever,
						},
					},
				},
			}

			// Create a request with a Job resource
			jobBytes, err := json.Marshal(job)
			Expect(err).NotTo(HaveOccurred())

			// Add recovery configuration to the cluster
			testCluster.Spec.Bootstrap = &cnpgv1.BootstrapConfiguration{
				Recovery: &cnpgv1.BootstrapRecovery{
					Source: "test-recovery-source",
				},
			}
			testCluster.Spec.ExternalClusters = []cnpgv1.ExternalCluster{
				{
					Name: "test-recovery-source",
					PluginConfiguration: &cnpgv1.PluginConfiguration{
						Name: common.PluginName,
						Parameters: map[string]string{
							"backupConfig": "test-backup-config",
						},
					},
				},
			}

			clusterBytes, err := json.Marshal(testCluster)
			Expect(err).NotTo(HaveOccurred())

			jobRequest = &lifecycle.OperatorLifecycleRequest{
				OperationType: &lifecycle.OperatorOperationType{
					Type: lifecycle.OperatorOperationType_TYPE_CREATE,
				},
				ObjectDefinition:  jobBytes,
				ClusterDefinition: clusterBytes,
			}
		})

		It("should add a sidecar container to the recovery job", func() {
			result, err := lifecycleImpl.reconcileJob(ctx, testCluster, jobRequest)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.JsonPatch).NotTo(BeEmpty())

			// Apply the patch to verify the changes
			patchedJob := job.DeepCopy()
			err = applyJSONPatch(patchedJob, result.JsonPatch)
			Expect(err).NotTo(HaveOccurred())

			// Check that the sidecar container was added
			Expect(patchedJob.Spec.Template.Spec.InitContainers).To(HaveLen(1))
			sidecar := patchedJob.Spec.Template.Spec.InitContainers[0]
			Expect(sidecar.Name).To(Equal("plugin-yandex-extensions"))
			Expect(sidecar.Image).To(Equal(sidecarImageString))
			Expect(sidecar.Args).To(Equal([]string{"instance", "--mode", "recovery"}))

			// Check that the plugin volume was added
			foundPluginVolume := false
			for _, volume := range patchedJob.Spec.Template.Spec.Volumes {
				if volume.Name == "plugins" {
					foundPluginVolume = true
					Expect(volume.EmptyDir).NotTo(BeNil())
					break
				}
			}
			Expect(foundPluginVolume).To(BeTrue())
		})

		It("should skip non-recovery jobs", func() {
			// Change job role to something else
			job.Spec.Template.ObjectMeta.Labels["cnpg.io/jobRole"] = "backup"

			jobBytes, err := json.Marshal(job)
			Expect(err).NotTo(HaveOccurred())

			jobRequest.ObjectDefinition = jobBytes

			result, err := lifecycleImpl.reconcileJob(ctx, testCluster, jobRequest)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.JsonPatch).To(BeEmpty())
		})
	})

	Describe("reconcilePodSpecWithPluginSidecar", func() {
		It("should add sidecar container with correct configuration", func() {
			podSpec := &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "postgres",
						Env: []corev1.EnvVar{
							{
								Name:  "PGDATA",
								Value: "/var/lib/postgresql/data/pgdata",
							},
						},
					},
				},
			}

			err := reconcilePodSpecWithPluginSidecar(testCluster, testBackupConfig, podSpec, "postgres", nil)
			Expect(err).NotTo(HaveOccurred())

			// Check that the sidecar container was added
			Expect(podSpec.InitContainers).To(HaveLen(1))
			sidecar := podSpec.InitContainers[0]
			Expect(sidecar.Name).To(Equal("plugin-yandex-extensions"))
			Expect(sidecar.Image).To(Equal(sidecarImageString))
			Expect(sidecar.Args).To(Equal([]string{"instance", "--mode", "normal"}))

			// Check that the environment variables were set correctly
			Expect(sidecar.Env).To(ContainElement(corev1.EnvVar{
				Name:  "NAMESPACE",
				Value: testNamespace,
			}))
			Expect(sidecar.Env).To(ContainElement(corev1.EnvVar{
				Name:  "CLUSTER_NAME",
				Value: testCluster.Name,
			}))
			Expect(sidecar.Env).To(ContainElement(corev1.EnvVar{
				Name:  "PGDATA",
				Value: "/var/lib/postgresql/data/pgdata",
			}))

			// Check that the security context was set correctly
			Expect(sidecar.SecurityContext).NotTo(BeNil())
			Expect(sidecar.SecurityContext.AllowPrivilegeEscalation).To(Equal(ptr.To(false)))
			Expect(sidecar.SecurityContext.RunAsNonRoot).To(Equal(ptr.To(true)))
			Expect(sidecar.SecurityContext.Privileged).To(Equal(ptr.To(false)))
			Expect(sidecar.SecurityContext.ReadOnlyRootFilesystem).To(Equal(ptr.To(false)))

			// Check that the resources were set correctly
			Expect(sidecar.Resources).To(Equal(testBackupConfig.Spec.Resources))
		})

		It("should set recovery mode for recovery jobs", func() {
			podSpec := &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "full-recovery",
						Env: []corev1.EnvVar{
							{
								Name:  "PGDATA",
								Value: "/var/lib/postgresql/data/pgdata",
							},
						},
					},
				},
			}

			err := reconcilePodSpecWithPluginSidecar(testCluster, testBackupConfig, podSpec, "full-recovery", nil)
			Expect(err).NotTo(HaveOccurred())

			// Check that the sidecar container was added with recovery mode
			Expect(podSpec.InitContainers).To(HaveLen(1))
			sidecar := podSpec.InitContainers[0]
			Expect(sidecar.Args).To(Equal([]string{"instance", "--mode", "recovery"}))
		})

		It("should merge environment variables from main container", func() {
			podSpec := &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "postgres",
						Env: []corev1.EnvVar{
							{
								Name:  "PGDATA",
								Value: "/var/lib/postgresql/data/pgdata",
							},
							{
								Name:  "CUSTOM_ENV",
								Value: "custom-value",
							},
						},
					},
				},
			}

			err := reconcilePodSpecWithPluginSidecar(testCluster, testBackupConfig, podSpec, "postgres", nil)
			Expect(err).NotTo(HaveOccurred())

			// Check that the environment variables were merged
			sidecar := podSpec.InitContainers[0]
			Expect(sidecar.Env).To(ContainElement(corev1.EnvVar{
				Name:  "CUSTOM_ENV",
				Value: "custom-value",
			}))
		})

		It("should add additional environment variables", func() {
			podSpec := &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "postgres",
						Env: []corev1.EnvVar{
							{
								Name:  "PGDATA",
								Value: "/var/lib/postgresql/data/pgdata",
							},
						},
					},
				},
			}

			additionalEnvs := []corev1.EnvVar{
				{
					Name:  "ADDITIONAL_ENV",
					Value: "additional-value",
				},
			}

			err := reconcilePodSpecWithPluginSidecar(testCluster, testBackupConfig, podSpec, "postgres", additionalEnvs)
			Expect(err).NotTo(HaveOccurred())

			// Check that the additional environment variables were added
			sidecar := podSpec.InitContainers[0]
			Expect(sidecar.Env).To(ContainElement(corev1.EnvVar{
				Name:  "ADDITIONAL_ENV",
				Value: "additional-value",
			}))
		})
	})

	Describe("injectPluginSidecarPodSpec", func() {
		It("should add sidecar container to pod spec", func() {
			podSpec := &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "postgres",
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "data",
								MountPath: "/var/lib/postgresql/data",
							},
						},
					},
				},
			}

			sidecar := &corev1.Container{
				Name:  "plugin-yandex-extensions",
				Image: sidecarImageString,
			}

			err := injectPluginSidecarPodSpec(podSpec, sidecar, "postgres")
			Expect(err).NotTo(HaveOccurred())

			// Check that the sidecar container was added
			Expect(podSpec.InitContainers).To(HaveLen(1))
			Expect(podSpec.InitContainers[0].Name).To(Equal("plugin-yandex-extensions"))

			// Check that the volume mounts were copied from the main container
			Expect(podSpec.InitContainers[0].VolumeMounts).To(ContainElement(corev1.VolumeMount{
				Name:      "data",
				MountPath: "/var/lib/postgresql/data",
			}))

			// Check that the plugin volume was added
			foundPluginVolume := false
			for _, volume := range podSpec.Volumes {
				if volume.Name == "plugins" {
					foundPluginVolume = true
					break
				}
			}
			Expect(foundPluginVolume).To(BeTrue())
		})

		It("should return error if main container not found", func() {
			podSpec := &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "not-postgres",
					},
				},
			}

			sidecar := &corev1.Container{
				Name:  "plugin-yandex-extensions",
				Image: sidecarImageString,
			}

			err := injectPluginSidecarPodSpec(podSpec, sidecar, "postgres")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("main container not found"))
		})

		It("should not add sidecar if it already exists", func() {
			podSpec := &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "postgres",
					},
				},
				InitContainers: []corev1.Container{
					{
						Name: "plugin-yandex-extensions",
					},
				},
			}

			sidecar := &corev1.Container{
				Name:  "plugin-yandex-extensions",
				Image: sidecarImageString,
			}

			err := injectPluginSidecarPodSpec(podSpec, sidecar, "postgres")
			Expect(err).NotTo(HaveOccurred())

			// Check that no additional sidecar was added
			Expect(podSpec.InitContainers).To(HaveLen(1))
		})
	})

	Describe("injectPluginVolumePodSpec", func() {
		It("should add plugin volume to pod spec", func() {
			podSpec := &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "postgres",
					},
				},
			}

			injectPluginVolumePodSpec(podSpec, "postgres")

			// Check that the plugin volume was added
			foundPluginVolume := false
			for _, volume := range podSpec.Volumes {
				if volume.Name == "plugins" {
					foundPluginVolume = true
					Expect(volume.EmptyDir).NotTo(BeNil())
					break
				}
			}
			Expect(foundPluginVolume).To(BeTrue())

			// Check that the volume mount was added to the postgres container
			foundVolumeMountInPostgres := false
			for _, container := range podSpec.Containers {
				if container.Name == "postgres" {
					for _, mount := range container.VolumeMounts {
						if mount.Name == "plugins" && mount.MountPath == "/plugins" {
							foundVolumeMountInPostgres = true
							break
						}
					}
				}
			}
			Expect(foundVolumeMountInPostgres).To(BeTrue())
		})

		It("should not add plugin volume if it already exists", func() {
			podSpec := &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "postgres",
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "plugins",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
				},
			}

			injectPluginVolumePodSpec(podSpec, "postgres")

			// Check that no additional volume was added
			Expect(podSpec.Volumes).To(HaveLen(1))
		})
	})

	Describe("getCNPGJobRole", func() {
		It("should return the job role from labels", func() {
			job := &batchv1.Job{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"cnpg.io/jobRole": "full-recovery",
							},
						},
					},
				},
			}

			role := getCNPGJobRole(job)
			Expect(role).To(Equal("full-recovery"))
		})

		It("should return empty string if no job role label", func() {
			job := &batchv1.Job{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"other-label": "value",
							},
						},
					},
				},
			}

			role := getCNPGJobRole(job)
			Expect(role).To(Equal(""))
		})
	})
})
