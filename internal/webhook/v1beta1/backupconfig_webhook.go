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

	"github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// nolint:unused
// log is for logging in this package.
var backupconfiglog = logf.Log.WithName("backupconfig-resource")

// SetupBackupConfigWebhookWithManager registers the webhook for BackupConfig in the manager.
func SetupBackupConfigWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&v1beta1.BackupConfig{}).
		WithValidator(&BackupConfigCustomValidator{}).
		WithDefaulter(&BackupConfigCustomDefaulter{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-cnpg-extensions-yandex-cloud-v1beta1-backupconfig,mutating=true,failurePolicy=fail,sideEffects=None,groups=cnpg-extensions.yandex.cloud,resources=backupconfigs,verbs=create;update,versions=v1beta1,name=mbackupconfig-v1beta1.kb.io,admissionReviewVersions=v1

// BackupConfigCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind BackupConfig when those are created or updated.
type BackupConfigCustomDefaulter struct{}

var _ webhook.CustomDefaulter = &BackupConfigCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind BackupConfig.
func (d *BackupConfigCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	backupconfig, ok := obj.(*v1beta1.BackupConfig)

	if !ok {
		return fmt.Errorf("expected an BackupConfig object but got %T", obj)
	}
	backupconfiglog.Info("Defaulting for BackupConfig", "name", backupconfig.GetName())
	backupconfig.Default()
	return nil
}

// +kubebuilder:webhook:path=/validate-cnpg-extensions-yandex-cloud-v1beta1-backupconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=cnpg-extensions.yandex.cloud,resources=backupconfigs,verbs=create;update,versions=v1beta1,name=vbackupconfig-v1beta1.kb.io,admissionReviewVersions=v1

// BackupConfigCustomValidator struct is responsible for validating the BackupConfig resource
// when it is created, updated, or deleted.
type BackupConfigCustomValidator struct {
}

var _ webhook.CustomValidator = &BackupConfigCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type BackupConfig.
func (v *BackupConfigCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	backupconfig, ok := obj.(*v1beta1.BackupConfig)
	if !ok {
		return nil, fmt.Errorf("expected a BackupConfig object but got %T", obj)
	}
	backupconfiglog.Info("Validation for BackupConfig upon creation", "name", backupconfig.GetName())

	return v.validateBackupConfig(backupconfig)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type BackupConfig.
func (v *BackupConfigCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	backupconfig, ok := newObj.(*v1beta1.BackupConfig)
	if !ok {
		return nil, fmt.Errorf("expected a BackupConfig object for the newObj but got %T", newObj)
	}
	backupconfiglog.Info("Validation for BackupConfig upon update", "name", backupconfig.GetName())

	return v.validateBackupConfig(backupconfig)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type BackupConfig.
func (v *BackupConfigCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	backupconfig, ok := obj.(*v1beta1.BackupConfig)
	if !ok {
		return nil, fmt.Errorf("expected a BackupConfig object but got %T", obj)
	}
	backupconfiglog.Info("Validation for BackupConfig upon deletion", "name", backupconfig.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}

func (v *BackupConfigCustomValidator) validateBackupConfig(backupconfig *v1beta1.BackupConfig) (admission.Warnings, error) {
	type validationFunc func(*v1beta1.BackupConfig) (admission.Warnings, error)
	validations := []validationFunc{
		v.validateStorage,
		v.validateS3MutualExclusivity,
	}

	var allWarnings admission.Warnings
	var allErrs field.ErrorList
	for _, validate := range validations {
		warn, err := validate(backupconfig)
		if err != nil {
			allErrs = append(allErrs, &field.Error{
				Type:   field.ErrorTypeInvalid,
				Detail: err.Error(),
			})
		}
		if len(warn) != 0 {
			allWarnings = append(allWarnings, warn...)
		}
	}

	if len(allErrs) == 0 {
		return allWarnings, nil
	}
	return allWarnings, apierrors.NewInvalid(
		schema.GroupKind{Group: "cnpg-extensions.yandex.cloud", Kind: "BackupConfig"},
		backupconfig.GetName(), allErrs)
}

func (v *BackupConfigCustomValidator) validateStorage(backupconfig *v1beta1.BackupConfig) (admission.Warnings, error) {
	if backupconfig.Spec.Storage.StorageType == v1beta1.StorageTypeS3 &&
		backupconfig.Spec.Storage.S3 == nil {
		return admission.Warnings{}, fmt.Errorf("failed to get S3-specific configuration for object storage")
	}
	return nil, nil
}

// validateS3MutualExclusivity validates mutual exclusivity constraints for S3 configuration
func (v *BackupConfigCustomValidator) validateS3MutualExclusivity(backupconfig *v1beta1.BackupConfig) (admission.Warnings, error) {
	if backupconfig.Spec.Storage.S3 == nil {
		return nil, nil
	}

	s3Config := backupconfig.Spec.Storage.S3

	// Validate prefix mutual exclusivity
	if s3Config.Prefix != "" && s3Config.PrefixFrom != nil {
		return admission.Warnings{}, fmt.Errorf("cannot specify both prefix and prefixFrom")
	}

	// Validate region mutual exclusivity
	if s3Config.Region != "" && s3Config.RegionFrom != nil {
		return admission.Warnings{}, fmt.Errorf("cannot specify both region and regionFrom")
	}

	// Validate endpointUrl mutual exclusivity
	if s3Config.EndpointURL != "" && s3Config.EndpointURLFrom != nil {
		return admission.Warnings{}, fmt.Errorf("cannot specify both endpointUrl and endpointUrlFrom")
	}

	// Validate ValueFromSource references
	if err := v.validateValueFromSource(s3Config.PrefixFrom, "prefixFrom"); err != nil {
		return admission.Warnings{}, err
	}

	if err := v.validateValueFromSource(s3Config.RegionFrom, "regionFrom"); err != nil {
		return admission.Warnings{}, err
	}

	if err := v.validateValueFromSource(s3Config.EndpointURLFrom, "endpointUrlFrom"); err != nil {
		return admission.Warnings{}, err
	}

	return nil, nil
}

// validateValueFromSource validates a ValueFromSource reference
func (v *BackupConfigCustomValidator) validateValueFromSource(valueFrom *v1beta1.ValueFromSource, fieldName string) error {
	if valueFrom == nil {
		return nil
	}

	// Check mutual exclusivity between SecretKeyRef and ConfigMapKeyRef
	if valueFrom.SecretKeyRef != nil && valueFrom.ConfigMapKeyRef != nil {
		return fmt.Errorf("%s cannot specify both secretKeyRef and configMapKeyRef", fieldName)
	}

	// Ensure at least one reference is provided
	if valueFrom.SecretKeyRef == nil && valueFrom.ConfigMapKeyRef == nil {
		return fmt.Errorf("%s must specify either secretKeyRef or configMapKeyRef", fieldName)
	}

	// Validate SecretKeyRef
	if valueFrom.SecretKeyRef != nil {
		if valueFrom.SecretKeyRef.Name == "" {
			return fmt.Errorf("%s.secretKeyRef.name cannot be empty", fieldName)
		}
		if valueFrom.SecretKeyRef.Key == "" {
			return fmt.Errorf("%s.secretKeyRef.key cannot be empty", fieldName)
		}
	}

	// Validate ConfigMapKeyRef
	if valueFrom.ConfigMapKeyRef != nil {
		if valueFrom.ConfigMapKeyRef.Name == "" {
			return fmt.Errorf("%s.configMapKeyRef.name cannot be empty", fieldName)
		}
		if valueFrom.ConfigMapKeyRef.Key == "" {
			return fmt.Errorf("%s.configMapKeyRef.key cannot be empty", fieldName)
		}
	}

	return nil
}
