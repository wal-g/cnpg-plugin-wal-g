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

package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Finalizer added to CNPG BackupConfig resources managed by plugin
// It is used to ensure that backup removed from storage before Backup resource deleted
const BackupConfigFinalizerName = "cnpg-plugin-wal-g.yandex.cloud/backup-config-cleanup"

// Finalizer added to Secret resources referenced by BackupConfig to protect from accidental deletion
// It is used to ensure that Secrets are not deleted while they are still referenced by a BackupConfig
const BackupConfigSecretFinalizerName = "cnpg-plugin-wal-g.yandex.cloud/backup-config-secret-protection"

// Finalizer added to ConfigMap resources referenced by BackupConfig to protect from accidental deletion
// It is used to ensure that ConfigMap are not deleted while they are still referenced by a BackupConfig
const BackupConfigCMFinalizerName = "cnpg-plugin-wal-g.yandex.cloud/backup-config-configmap-protection"

// S3StorageConfig defines S3-specific configuration for object storage
type S3StorageConfig struct {
	// e.g. s3://bucket/path/to/folder
	Prefix string `json:"prefix,omitempty"`

	// S3 Region
	Region string `json:"region,omitempty"`

	// S3 endpoint url
	EndpointURL string `json:"endpointUrl,omitempty"`

	// To enable path-style addressing (i.e., http://s3.amazonaws.com/BUCKET/KEY)
	// when connecting to an S3-compatible service that lack of support for
	// sub-domain style bucket URLs (i.e., http://BUCKET.s3.amazonaws.com/KEY)
	ForcePathStyle bool `json:"forcePathStyle,omitempty"`

	// S3 storage class used for backup files.
	// Default is "STANDARD". Other supported values include
	// "STANDARD_IA" for Infrequent Access and
	// "REDUCED_REDUNDANCY" for Reduced Redundancy.
	StorageClass string `json:"storageClass,omitempty"`

	// Custom CA certificate reference for S3 endpoint
	// Can be specified in either ConfigMap or Secret
	CustomCA *CustomCAReference `json:"customCA,omitempty"`

	AccessKeyIDRef     *corev1.SecretKeySelector `json:"accessKeyId,omitempty"`
	AccessKeySecretRef *corev1.SecretKeySelector `json:"accessKeySecret,omitempty"`
}

// CustomCAReference defines a reference to a custom CA certificate stored in a ConfigMap or Secret
type CustomCAReference struct {
	// Kind of the resource containing the CA certificate
	// Can be either "ConfigMap" or "Secret"
	// +kubebuilder:validation:Enum=ConfigMap;Secret
	Kind string `json:"kind"`

	// Name of the ConfigMap or Secret containing the CA certificate
	Name string `json:"name"`

	// Key in the ConfigMap or Secret containing the CA certificate
	Key string `json:"key"`
}

type StorageType string

const (
	StorageTypeS3 = "s3"
)

// StorageConfig defines object storage configuration for BackupConfig
type StorageConfig struct {
	StorageType StorageType      `json:"type"`         // Type of storage to use, currently supported "s3" only
	S3          *S3StorageConfig `json:"s3,omitempty"` // S3-specific parameters
}

// BackupConfigSpec defines the desired state of BackupConfig.
type BackupConfigSpec struct {
	// How many goroutines to use during backup && wal downloading. Default value is evaluated at runtime
	DownloadConcurrency *int `json:"downloadConcurrency,omitempty"`

	// Disk read rate limit during backup creation in bytes per second. Default value is evaluated at runtime
	UploadDiskRateLimit *int `json:"uploadDiskRateLimitBytesPerSecond,omitempty"`

	// Network upload rate limit during backup uploading in bytes per second. Default value is evaluated at runtime
	UploadNetworkRateLimit *int `json:"uploadNetworkRateLimitBytesPerSecond,omitempty"`

	// How many concurrency streams to use during backup uploading. Default value is evaluated at runtime
	UploadConcurrency *int `json:"uploadConcurrency,omitempty"`

	// How many concurrency streams are reading disk during backup uploading. Default value is evaluated at runtime
	UploadDiskConcurrency *int `json:"uploadDiskConcurrency,omitempty"`

	// How many times failed file will be retried during backup / wal download. Default: 15.
	DownloadFileRetries int `json:"downloadFileRetries,omitempty"`

	// Determines how many delta backups can be between full backups. Defaults to 0.
	DeltaMaxSteps int `json:"deltaMaxSteps,omitempty"`

	// Disable calling fsync after writing files when extracting tar files. Default: false.
	TarDisableFsync bool `json:"tarDisableFsync,omitempty"`

	// Threshold in bytes is size of one backup bundle. Default: 1073741823.
	TarSizeThreshold *int64 `json:"tarSizeThreshold,omitempty"`

	// Backups retention configuration
	Retention BackupRetentionConfig `json:"retention,omitempty"`

	// Backups storage configuration
	Storage StorageConfig `json:"storage"`

	// Resources for wal-g sidecar configurations
	//
	// IMPORTANT: resource changes will NOT trigger auto-update on clusters
	// Manual rollout with pods recreation needed instead
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Backups encryption configuration
	Encryption BackupEncryptionConfig `json:"encryption,omitempty"`
}

type BackupEncryptionConfig struct {
	// Method used for backup encryption.
	// Currently "libsodium" method supported only.
	// Use "none" or leave empty to disable encryption.
	Method string `json:"method,omitempty"`

	// Name of the existing secret with entryption keys.
	//
	// If empty / not provided - secret will be generated automatically
	// with name "<backupconfig-name>-encryption" and random key
	//
	// For "libsodium" method secret should contain single key "libsodiumKey"
	// containing hex-encoded 32-bytes length key i.e. created with `openssl rand -hex 32`
	ExistingEncryptionSecretName string `json:"encryptionSecret,omitempty"`
}

type BackupRetentionConfig struct {
	// Whether to ignore manually created backups in retention policy
	//
	// IMPORTANT: Automatically created backups should have OwnerReference with
	// ScheduledBackup or Cluster resource to be treated as auto backups!
	// (.spec.backupOwnerReference: "self" or "cluster" in ScheduledBackup resource)
	// +kubebuilder:default:=false
	// +optional
	IgnoreForManualBackups bool `json:"ignoreForManualBackups,omitempty"`

	// Minimal number of full backups to keep, this will keep backups
	// even if backup should be deleted due to DeleteBackupsAfter policy
	// Default is 5 backups
	// +kubebuilder:default:=5
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=99
	// +optional
	MinBackupsToKeep int `json:"minBackupsToKeep,omitempty"`

	// DeleteBackupsAfter is the retention policy to be used for backups
	// and WALs (i.e. '60d'). It is expressed in the form
	// of `XXu` where `XX` is a positive integer and `u` is in `[dwmh]` -
	// days, weeks, months, hours (i.e. '7d', '4w', '1m', '6h').
	// Different units should not be used at the same time
	// If not specified - backups will not be deleted automatically
	// +kubebuilder:validation:Pattern=^[1-9][0-9]*[dwmh]$
	// +optional
	DeleteBackupsAfter string `json:"deleteBackupsAfter,omitempty"`
}

// BackupConfigStatus defines the observed state of BackupConfig.
type BackupConfigStatus struct {
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// BackupConfig is the Schema for the backupconfigs API.
type BackupConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackupConfigSpec   `json:"spec,omitempty"`
	Status BackupConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// BackupConfigList contains a list of BackupConfig.
type BackupConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BackupConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BackupConfig{}, &BackupConfigList{})
}
