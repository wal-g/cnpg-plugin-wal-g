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
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// s3StorageConfigWithSecrets defines S3-specific configuration with embedded secrets data (AccessKeyID && AccessKeySecret)
type S3StorageConfigWithSecrets struct {
	S3StorageConfig
	AccessKeyID     string
	AccessKeySecret string
}

// storageConfigWithSecrets defines object storage configuration extended with secrets data
type StorageConfigWithSecrets struct {
	StorageConfig
	S3 *S3StorageConfigWithSecrets `json:"s3,omitempty"` // S3-specific parameters
}

// BackupEncryptionLibsodiumConfigWithSecrets defines libsodium encryption configuration with embedded secret data
type BackupEncryptionLibsodiumConfigWithSecrets struct {
	BackupEncryptionLibsodiumConfig
	EncryptionKeyData string
}

// BackupEncryptionConfigWithSecrets defines encryption configuration with embedded secrets data
type BackupEncryptionConfigWithSecrets struct {
	BackupEncryptionConfig
	LibsodiumConfig *BackupEncryptionLibsodiumConfigWithSecrets `json:"libsodium,omitempty"`
}

// backupConfigSpec defines the BackupConfigSpec extended with secrets data
type BackupConfigSpecWithSecrets struct {
	BackupConfigSpec
	Storage    StorageConfigWithSecrets           `json:"storage"`
	Encryption *BackupEncryptionConfigWithSecrets `json:"encryption,omitempty"`
}

// BackupConfigWithSecrets defines the BackupConfig with embedded secrets data (ex. S3 credentials)
type BackupConfigWithSecrets struct {
	BackupConfig
	Spec BackupConfigSpecWithSecrets `json:"spec,omitempty"`
}

func (b *BackupConfig) PrefetchSecretsData(ctx context.Context, c client.Client) (*BackupConfigWithSecrets, error) {
	spec, err := b.makeBackupConfigSpecWithPrefilledSecrets(ctx, c)
	if err != nil {
		return nil, err
	}

	return &BackupConfigWithSecrets{
		BackupConfig: *b.DeepCopy(),
		Spec:         spec,
	}, nil
}

func (b *BackupConfig) makeBackupConfigSpecWithPrefilledSecrets(
	ctx context.Context,
	c client.Client,
) (BackupConfigSpecWithSecrets, error) {
	storage, err := b.makeStorageConfigWithPrefilledSecrets(ctx, c)
	if err != nil {
		return BackupConfigSpecWithSecrets{}, err
	}

	encryption, err := b.makeEncryptionConfigWithPrefilledSecrets(ctx, c)
	if err != nil {
		return BackupConfigSpecWithSecrets{}, err
	}

	return BackupConfigSpecWithSecrets{
		BackupConfigSpec: *b.Spec.DeepCopy(),
		Storage:          storage,
		Encryption:       encryption,
	}, nil
}

func (b *BackupConfig) makeEncryptionConfigWithPrefilledSecrets(
	ctx context.Context,
	c client.Client,
) (*BackupEncryptionConfigWithSecrets, error) {
	if b.Spec.Encryption.Method == "" {
		return nil, nil
	}

	libsodiumConfig, err := b.makeLibsodiumConfigWithPrefilledSecrets(ctx, c)
	if err != nil {
		return nil, err
	}

	return &BackupEncryptionConfigWithSecrets{
		BackupEncryptionConfig: *b.Spec.Encryption.DeepCopy(),
		LibsodiumConfig:        libsodiumConfig,
	}, nil
}

func (b *BackupConfig) makeLibsodiumConfigWithPrefilledSecrets(
	ctx context.Context,
	c client.Client,
) (*BackupEncryptionLibsodiumConfigWithSecrets, error) {
	if b.Spec.Encryption.Method != "libsodium" || b.Spec.Encryption.LibsodiumConfig.EncryptionKey == nil {
		return nil, nil
	}

	encryptionKey, err := extractValueFromSecret(ctx, c, b.Spec.Encryption.LibsodiumConfig.EncryptionKey, b.Namespace)
	if err != nil {
		return nil, err
	}

	return &BackupEncryptionLibsodiumConfigWithSecrets{
		BackupEncryptionLibsodiumConfig: *b.Spec.Encryption.LibsodiumConfig.DeepCopy(),
		EncryptionKeyData:               string(encryptionKey),
	}, nil
}

func (b *BackupConfig) makeStorageConfigWithPrefilledSecrets(
	ctx context.Context,
	c client.Client,
) (StorageConfigWithSecrets, error) {
	s3, err := b.makeS3StorageConfigWithPrefilledSecrets(ctx, c)
	if err != nil {
		return StorageConfigWithSecrets{}, err
	}

	return StorageConfigWithSecrets{
		StorageConfig: *b.Spec.Storage.DeepCopy(),
		S3:            s3,
	}, nil
}

func (b *BackupConfig) makeS3StorageConfigWithPrefilledSecrets(
	ctx context.Context,
	c client.Client,
) (*S3StorageConfigWithSecrets, error) {
	if b.Spec.Storage.S3 == nil {
		return nil, nil
	}

	accessKeyID, err := extractValueFromSecret(ctx, c, b.Spec.Storage.S3.AccessKeyIDRef, b.Namespace)
	if err != nil {
		return nil, err
	}

	accessKeySecret, err := extractValueFromSecret(ctx, c, b.Spec.Storage.S3.AccessKeySecretRef, b.Namespace)
	if err != nil {
		return nil, err
	}

	return &S3StorageConfigWithSecrets{
		S3StorageConfig: *b.Spec.Storage.S3.DeepCopy(),
		AccessKeyID:     string(accessKeyID),
		AccessKeySecret: string(accessKeySecret),
	}, nil
}

func extractValueFromSecret(
	ctx context.Context,
	c client.Client,
	secretReference *v1.SecretKeySelector,
	namespace string,
) ([]byte, error) {
	if secretReference == nil {
		return nil, fmt.Errorf("secretReference is nil")
	}

	secret := &v1.Secret{}
	err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretReference.Name}, secret)
	if err != nil {
		return nil, fmt.Errorf("while getting secret %s: %w", secretReference.Name, err)
	}

	value, ok := secret.Data[secretReference.Key]
	if !ok {
		return nil, fmt.Errorf("missing key %s, inside secret %s", secretReference.Key, secretReference.Name)
	}

	return value, nil
}
