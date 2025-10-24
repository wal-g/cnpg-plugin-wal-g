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

package walg

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
)

type Config struct {
	AWSAccessKeyID                    string `json:"AWS_ACCESS_KEY_ID,omitempty"`
	AWSEndpoint                       string `json:"AWS_ENDPOINT,omitempty"`
	AWSS3ForcePathStyle               bool   `json:"AWS_S3_FORCE_PATH_STYLE,omitempty"`
	AWSRegion                         string `json:"AWS_REGION,omitempty"`
	AWSSecretAccessKey                string `json:"AWS_SECRET_ACCESS_KEY,omitempty"`
	GoDebug                           string `json:"GODEBUG,omitempty"`
	GoMaxProcs                        int    `json:"GOMAXPROCS,omitempty"`
	PgHost                            string `json:"PGHOST,omitempty"`
	PgUser                            string `json:"PGUSER,omitempty"`
	S3LogLevel                        string `json:"S3_LOG_LEVEL,omitempty"`
	TotalBgUploadedLimit              int    `json:"TOTAL_BG_UPLOADED_LIMIT,omitempty"`
	WaleGPGKeyID                      string `json:"WALE_GPG_KEY_ID,omitempty"`
	WaleS3Prefix                      string `json:"WALE_S3_PREFIX,omitempty"`
	WalgAliveCheckInterval            string `json:"WALG_ALIVE_CHECK_INTERVAL,omitempty"`
	WalgCompressionMethod             string `json:"WALG_COMPRESSION_METHOD,omitempty"`
	WalgDeltaMaxSteps                 int    `json:"WALG_DELTA_MAX_STEPS,omitempty"`
	WalgDiskRateLimit                 int    `json:"WALG_DISK_RATE_LIMIT,omitempty"`
	WalgDownloadConcurrency           int    `json:"WALG_DOWNLOAD_CONCURRENCY,omitempty"`
	WalgDownloadFileRetries           int    `json:"WALG_DOWNLOAD_FILE_RETRIES,omitempty"`
	WalgFailoverStoragesCacheLifetime string `json:"WALG_FAILOVER_STORAGES_CACHE_LIFETIME,omitempty"`
	WalgFailoverStoragesCheck         bool   `json:"WALG_FAILOVER_STORAGES_CHECK,omitempty"`
	WalgFailoverStoragesCheckSize     string `json:"WALG_FAILOVER_STORAGES_CHECK_SIZE,omitempty"`
	WalgLibsodiumKey                  string `json:"WALG_LIBSODIUM_KEY,omitempty"`
	WalgLibsodiumKeyPath              string `json:"WALG_LIBSODIUM_KEY_PATH,omitempty"`
	WalgLibsodiumKeyTransform         string `json:"WALG_LIBSODIUM_KEY_TRANSFORM,omitempty"`
	WalgLogDestination                string `json:"WALG_LOG_DESTINATION,omitempty"`
	WalgNetworkRateLimit              int    `json:"WALG_NETWORK_RATE_LIMIT,omitempty"`
	WalgPgpKeyPath                    string `json:"WALG_PGP_KEY_PATH,omitempty"`
	WalgPrefetchDir                   string `json:"WALG_PREFETCH_DIR,omitempty"`
	WalgPreventWalOverwrite           string `json:"WALG_PREVENT_WAL_OVERWRITE,omitempty"`
	WalgS3CACertFile                  string `json:"WALG_S3_CA_CERT_FILE,omitempty"`
	WalgS3StorageClass                string `json:"WALG_S3_STORAGE_CLASS,omitempty"`
	WalgTarDisableFsync               string `json:"WALG_TAR_DISABLE_FSYNC,omitempty"`
	WalgTarSizeThreshold              int64  `json:"WALG_TAR_SIZE_THRESHOLD,omitempty"`
	WalgUploadConcurrency             int    `json:"WALG_UPLOAD_CONCURRENCY,omitempty"`
	WalgUploadDiskConcurrency         int    `json:"WALG_UPLOAD_DISK_CONCURRENCY,omitempty"`
}

func NewConfigWithDefaults() Config {
	return Config{ // TODO: evaluate defaults based on runtime constraints!
		GoMaxProcs:                        5,
		PgHost:                            "/controller/run",
		PgUser:                            "postgres",
		TotalBgUploadedLimit:              32,
		WalgAliveCheckInterval:            "30s",
		WalgCompressionMethod:             "brotli",
		WalgDeltaMaxSteps:                 6,
		WalgDiskRateLimit:                 7864320,
		WalgDownloadConcurrency:           12,
		WalgFailoverStoragesCacheLifetime: "0m",
		WalgFailoverStoragesCheck:         false,
		WalgFailoverStoragesCheckSize:     "1KB",
		WalgNetworkRateLimit:              536870912,
		WalgS3StorageClass:                "STANDARD",
		WalgTarDisableFsync:               "False",
		WalgUploadConcurrency:             16,
		WalgUploadDiskConcurrency:         8,
		WalgPreventWalOverwrite:           "True",
	}
}

func NewConfigFromBackupConfig(backupConfig *v1beta1.BackupConfigWithSecrets, pgMajorVersion int) *Config {
	config := NewConfigWithDefaults()

	if backupConfig.Spec.Storage.StorageType == v1beta1.StorageTypeS3 {
		config.AWSAccessKeyID = backupConfig.Spec.Storage.S3.AccessKeyID
		config.AWSSecretAccessKey = backupConfig.Spec.Storage.S3.AccessKeySecret
		config.AWSEndpoint = backupConfig.Spec.Storage.S3.ResolvedEndpointURL
		config.AWSRegion = backupConfig.Spec.Storage.S3.ResolvedRegion
		config.AWSS3ForcePathStyle = backupConfig.Spec.Storage.S3.ForcePathStyle
		config.WaleS3Prefix = fmt.Sprintf("%s/%d", backupConfig.Spec.Storage.S3.ResolvedPrefix, pgMajorVersion)
		config.WalgS3StorageClass = backupConfig.Spec.Storage.S3.StorageClass

		// Handle custom CA certificate if provided
		if backupConfig.Spec.Storage.S3.CustomCA != nil && backupConfig.Spec.Storage.S3.CACertData != "" {
			caFilePath, err := ensureCACertFile(
				backupConfig.Namespace,
				backupConfig.Name,
				backupConfig.Spec.Storage.S3.CACertData,
			)
			if err != nil {
				// Log the error but continue without CA certificate
				panic(fmt.Sprintf("Warning: Failed to ensure CA certificate file: %s", err.Error()))
			} else if caFilePath != "" {
				config.WalgS3CACertFile = caFilePath
			}
		}
	}

	// Configure encryption if enabled
	if backupConfig.Spec.Encryption != nil {
		if backupConfig.Spec.Encryption.Method == "libsodium" {
			config.WalgLibsodiumKey = backupConfig.Spec.Encryption.EncryptionKeyData
			config.WalgLibsodiumKeyTransform = "hex"
		}
	}
	config.WalgDeltaMaxSteps = backupConfig.Spec.DeltaMaxSteps
	config.WalgDownloadFileRetries = backupConfig.Spec.DownloadFileRetries
	config.WalgTarDisableFsync = strconv.FormatBool(backupConfig.Spec.TarDisableFsync)

	if backupConfig.Spec.DownloadConcurrency != nil {
		config.WalgDownloadConcurrency = *backupConfig.Spec.DownloadConcurrency
	}
	if backupConfig.Spec.UploadDiskRateLimit != nil {
		config.WalgDiskRateLimit = *backupConfig.Spec.UploadDiskRateLimit
	}
	if backupConfig.Spec.UploadNetworkRateLimit != nil {
		config.WalgNetworkRateLimit = *backupConfig.Spec.UploadNetworkRateLimit
	}
	if backupConfig.Spec.UploadConcurrency != nil {
		config.WalgUploadConcurrency = *backupConfig.Spec.UploadConcurrency
	}
	if backupConfig.Spec.UploadDiskConcurrency != nil {
		config.WalgUploadDiskConcurrency = *backupConfig.Spec.UploadDiskConcurrency
	}
	if backupConfig.Spec.TarSizeThreshold != nil {
		config.WalgTarSizeThreshold = *backupConfig.Spec.TarSizeThreshold
	}
	return &config
}

// ToFile dumps config to a file in JSON format acceptable by wal-g via --config param
func (c *Config) ToFile(targetFilepath string) error {
	bytes, err := json.MarshalIndent(c, "", "\t")
	if err != nil {
		return err
	}

	newFile, err := os.OpenFile(targetFilepath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return err
	}

	_, err = newFile.Write(bytes)
	return err
}

// ToEnvMap returns Map with environment variables acceptable by wal-g
func (c *Config) ToEnvMap() map[string]string {
	val := reflect.ValueOf(*c)
	typ := val.Type()

	result := make(map[string]string, val.NumField())

	for i := 0; i < val.NumField(); i++ {
		fieldName := typ.Field(i).Name

		fieldJSONTag, hasJSONTag := typ.Field(i).Tag.Lookup("json")
		if !hasJSONTag {
			continue
		}
		tagParams := strings.Split(fieldJSONTag, ",")
		envName := tagParams[0]
		// Omit fields with json:"-" tag
		if envName == "-" {
			continue
		}
		// Fields with json:"" or json:",omitempty" should use field name as a key
		if envName == "" {
			envName = fieldName
		}
		fieldValueKind := val.Field(i).Kind()
		var fieldValue string

		// Fields with omitempty should be skipped if value is empty (ex "", false, 0)
		// In our cases (no maps/slices inside struct empty is equal to IsZero)
		if slices.Contains(tagParams, "omitempty") && val.Field(i).IsZero() {
			continue
		}

		// Fields with omitzero should be skipped if value IsZero
		if slices.Contains(tagParams, "omitzero") && val.Field(i).IsZero() {
			continue
		}

		switch fieldValueKind {
		case reflect.Int, reflect.Int64:
			fieldValue = strconv.FormatInt(val.Field(i).Int(), 10)
		case reflect.String:
			fieldValue = val.Field(i).String()
		case reflect.Bool:
			fieldValue = strconv.FormatBool(val.Field(i).Bool())
		default:
			// Should panic on unknown field types, because we control all Config struct fields
			panic(fmt.Sprintf(
				"cannot marshal field %s with type %s, supported only Bool,Int,String",
				fieldName,
				fieldValueKind.String(),
			))
		}

		result[envName] = fieldValue
	}
	return result
}
