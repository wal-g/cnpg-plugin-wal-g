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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	cnpgextensionsv1beta1 "github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
)

// nolint:unused
// log is for logging in this package.
var backupconfiglog = logf.Log.WithName("backupconfig-resource")

// SetupBackupConfigWebhookWithManager registers the webhook for BackupConfig in the manager.
func SetupBackupConfigWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&cnpgextensionsv1beta1.BackupConfig{}).
		WithValidator(&BackupConfigCustomValidator{}).
		WithDefaulter(&BackupConfigCustomDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-cnpg-extensions-yandex-cloud-v1beta1-backupconfig,mutating=true,failurePolicy=fail,sideEffects=None,groups=cnpg-extensions.yandex.cloud,resources=backupconfigs,verbs=create;update,versions=v1beta1,name=mbackupconfig-v1beta1.kb.io,admissionReviewVersions=v1

// BackupConfigCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind BackupConfig when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type BackupConfigCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

var _ webhook.CustomDefaulter = &BackupConfigCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind BackupConfig.
func (d *BackupConfigCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	backupconfig, ok := obj.(*cnpgextensionsv1beta1.BackupConfig)

	if !ok {
		return fmt.Errorf("expected an BackupConfig object but got %T", obj)
	}
	backupconfiglog.Info("Defaulting for BackupConfig", "name", backupconfig.GetName())

	// TODO(user): fill in your defaulting logic.

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-cnpg-extensions-yandex-cloud-v1beta1-backupconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=cnpg-extensions.yandex.cloud,resources=backupconfigs,verbs=create;update,versions=v1beta1,name=vbackupconfig-v1beta1.kb.io,admissionReviewVersions=v1

// BackupConfigCustomValidator struct is responsible for validating the BackupConfig resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type BackupConfigCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

var _ webhook.CustomValidator = &BackupConfigCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type BackupConfig.
func (v *BackupConfigCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	backupconfig, ok := obj.(*cnpgextensionsv1beta1.BackupConfig)
	if !ok {
		return nil, fmt.Errorf("expected a BackupConfig object but got %T", obj)
	}
	backupconfiglog.Info("Validation for BackupConfig upon creation", "name", backupconfig.GetName())

	// TODO(user): fill in your validation logic upon object creation.

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type BackupConfig.
func (v *BackupConfigCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	backupconfig, ok := newObj.(*cnpgextensionsv1beta1.BackupConfig)
	if !ok {
		return nil, fmt.Errorf("expected a BackupConfig object for the newObj but got %T", newObj)
	}
	backupconfiglog.Info("Validation for BackupConfig upon update", "name", backupconfig.GetName())

	// TODO(user): fill in your validation logic upon object update.

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type BackupConfig.
func (v *BackupConfigCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	backupconfig, ok := obj.(*cnpgextensionsv1beta1.BackupConfig)
	if !ok {
		return nil, fmt.Errorf("expected a BackupConfig object but got %T", obj)
	}
	backupconfiglog.Info("Validation for BackupConfig upon deletion", "name", backupconfig.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
