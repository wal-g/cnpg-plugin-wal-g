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
	CACertData      string // Custom CA certificate data for S3 endpoint
	// Resolved values from references
	ResolvedPrefix      string // Resolved value from Prefix or PrefixFrom
	ResolvedRegion      string // Resolved value from Region or RegionFrom
	ResolvedEndpointURL string // Resolved value from EndpointURL or EndpointURLFrom
}

// storageConfigWithSecrets defines object storage configuration extended with secrets data
type StorageConfigWithSecrets struct {
	StorageConfig
	S3 *S3StorageConfigWithSecrets `json:"s3,omitempty"` // S3-specific parameters
}

// BackupEncryptionConfigWithSecrets defines encryption configuration with embedded secrets data
type BackupEncryptionConfigWithSecrets struct {
	BackupEncryptionConfig
	EncryptionKeyData string
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
	newCfg := &BackupEncryptionConfigWithSecrets{BackupEncryptionConfig: *b.Spec.Encryption.DeepCopy()}

	secretName := b.Spec.Encryption.ExistingEncryptionSecretName
	if secretName == "" {
		secretName = fmt.Sprintf("%s-encryption", b.Name)
	}

	switch b.Spec.Encryption.Method {
	case "libsodium": // For libsodium we need to extract only encryption key
		{
			secretKeyName := "libsodiumKey"
			encryptionKey, err := extractValueFromSecret(ctx, c, &v1.SecretKeySelector{
				LocalObjectReference: v1.LocalObjectReference{Name: secretName},
				Key:                  secretKeyName,
			}, b.Namespace)
			if err != nil {
				return nil, err
			}
			newCfg.EncryptionKeyData = string(encryptionKey)
		}
	case "", "none": // Do not fill anything if encryption method is none or not specified
		return newCfg, nil
	default:
		return nil, fmt.Errorf("unknown encryption method \"%s\"", b.Spec.Encryption.Method)
	}

	return newCfg, nil
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

	// Create a deep copy of the S3StorageConfig to preserve all fields, including CustomCA
	s3Config := b.Spec.Storage.S3.DeepCopy()

	// Extract CA certificate data if CustomCA is specified
	var caCertData string
	if s3Config.CustomCA != nil {
		var certData []byte
		var err error

		switch s3Config.CustomCA.Kind {
		case "Secret":
			certData, err = extractValueFromSecret(ctx, c, &v1.SecretKeySelector{
				LocalObjectReference: v1.LocalObjectReference{Name: s3Config.CustomCA.Name},
				Key:                  s3Config.CustomCA.Key,
			}, b.Namespace)
		case "ConfigMap":
			certData, err = extractValueFromConfigMap(ctx, c, &v1.ConfigMapKeySelector{
				LocalObjectReference: v1.LocalObjectReference{Name: s3Config.CustomCA.Name},
				Key:                  s3Config.CustomCA.Key,
			}, b.Namespace)
		default:
			return nil, fmt.Errorf("unsupported CustomCA kind: %s", s3Config.CustomCA.Kind)
		}

		if err != nil {
			return nil, fmt.Errorf("failed to extract CA certificate data: %w", err)
		}
		caCertData = string(certData)
	}

	// Resolve prefix value
	resolvedPrefix, err := b.resolveStringValue(ctx, c, s3Config.Prefix, s3Config.PrefixFrom, "prefix")
	if err != nil {
		return nil, err
	}

	// Resolve region value
	resolvedRegion, err := b.resolveStringValue(ctx, c, s3Config.Region, s3Config.RegionFrom, "region")
	if err != nil {
		return nil, err
	}

	// Resolve endpoint URL value
	resolvedEndpointURL, err := b.resolveStringValue(ctx, c, s3Config.EndpointURL, s3Config.EndpointURLFrom, "endpointUrl")
	if err != nil {
		return nil, err
	}

	return &S3StorageConfigWithSecrets{
		S3StorageConfig:     *s3Config,
		AccessKeyID:         string(accessKeyID),
		AccessKeySecret:     string(accessKeySecret),
		CACertData:          caCertData,
		ResolvedPrefix:      resolvedPrefix,
		ResolvedRegion:      resolvedRegion,
		ResolvedEndpointURL: resolvedEndpointURL,
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

// extractValueFromConfigMap extracts a value from a ConfigMap
func extractValueFromConfigMap(
	ctx context.Context,
	c client.Client,
	configMapReference *v1.ConfigMapKeySelector,
	namespace string,
) ([]byte, error) {
	if configMapReference == nil {
		return nil, fmt.Errorf("configMapReference is nil")
	}

	configMap := &v1.ConfigMap{}
	err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: configMapReference.Name}, configMap)
	if err != nil {
		return nil, fmt.Errorf("while getting ConfigMap %s: %w", configMapReference.Name, err)
	}

	value, ok := configMap.Data[configMapReference.Key]
	if !ok {
		return nil, fmt.Errorf("missing key %s, inside ConfigMap %s", configMapReference.Key, configMapReference.Name)
	}

	return []byte(value), nil
}

// resolveStringValue resolves a string value from either direct value or ValueFromSource reference
// It ensures mutual exclusivity between direct value and reference
func (b *BackupConfig) resolveStringValue(
	ctx context.Context,
	c client.Client,
	directValue string,
	valueFrom *ValueFromSource,
	fieldName string,
) (string, error) {
	// Check mutual exclusivity
	if directValue != "" && valueFrom != nil {
		return "", fmt.Errorf("cannot specify both %s and %sFrom", fieldName, fieldName)
	}

	// If direct value is provided, use it
	if directValue != "" {
		return directValue, nil
	}

	// If no reference is provided, return empty string
	if valueFrom == nil {
		return "", nil
	}

	// Extract value from reference
	return extractValueFromSource(ctx, c, valueFrom, b.Namespace)
}

// extractValueFromSource extracts a value from either Secret or ConfigMap reference
func extractValueFromSource(
	ctx context.Context,
	c client.Client,
	valueFrom *ValueFromSource,
	namespace string,
) (string, error) {
	if valueFrom == nil {
		return "", fmt.Errorf("valueFrom is nil")
	}

	// Check mutual exclusivity between SecretKeyRef and ConfigMapKeyRef
	if valueFrom.SecretKeyRef != nil && valueFrom.ConfigMapKeyRef != nil {
		return "", fmt.Errorf("cannot specify both secretKeyRef and configMapKeyRef")
	}

	if valueFrom.SecretKeyRef == nil && valueFrom.ConfigMapKeyRef == nil {
		return "", fmt.Errorf("must specify either secretKeyRef or configMapKeyRef")
	}

	// Extract from Secret
	if valueFrom.SecretKeyRef != nil {
		data, err := extractValueFromSecret(ctx, c, valueFrom.SecretKeyRef, namespace)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}

	// Extract from ConfigMap
	if valueFrom.ConfigMapKeyRef != nil {
		data, err := extractValueFromConfigMap(ctx, c, valueFrom.ConfigMapKeyRef, namespace)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}

	return "", fmt.Errorf("no valid reference found")
}
