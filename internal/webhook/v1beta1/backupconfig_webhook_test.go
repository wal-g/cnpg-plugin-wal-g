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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	cnpgextensionsv1beta1 "github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	// TODO (user): Add any additional imports if needed
)

var _ = Describe("BackupConfig Webhook", func() {
	var (
		obj       *cnpgextensionsv1beta1.BackupConfig
		oldObj    *cnpgextensionsv1beta1.BackupConfig
		validator BackupConfigCustomValidator
		defaulter BackupConfigCustomDefaulter
	)

	BeforeEach(func() {
		obj = &cnpgextensionsv1beta1.BackupConfig{}
		oldObj = &cnpgextensionsv1beta1.BackupConfig{}
		validator = BackupConfigCustomValidator{}
		Expect(validator).NotTo(BeNil(), "Expected validator to be initialized")
		defaulter = BackupConfigCustomDefaulter{}
		Expect(defaulter).NotTo(BeNil(), "Expected defaulter to be initialized")
		Expect(oldObj).NotTo(BeNil(), "Expected oldObj to be initialized")
		Expect(obj).NotTo(BeNil(), "Expected obj to be initialized")
		// TODO (user): Add any setup logic common to all tests
	})

	AfterEach(func() {
		// TODO (user): Add any teardown logic common to all tests
	})

	Context("When creating BackupConfig under Defaulting Webhook", func() {
		// TODO (user): Add logic for defaulting webhooks
		// Example:
		// It("Should apply defaults when a required field is empty", func() {
		//     By("simulating a scenario where defaults should be applied")
		//     obj.SomeFieldWithDefault = ""
		//     By("calling the Default method to apply defaults")
		//     defaulter.Default(ctx, obj)
		//     By("checking that the default values are set")
		//     Expect(obj.SomeFieldWithDefault).To(Equal("default_value"))
		// })
	})

	Context("When creating or updating BackupConfig under Validating Webhook", func() {
		// TODO (user): Add logic for validating webhooks
		// Example:
		// It("Should deny creation if a required field is missing", func() {
		//     By("simulating an invalid creation scenario")
		//     obj.SomeRequiredField = ""
		//     Expect(validator.ValidateCreate(ctx, obj)).Error().To(HaveOccurred())
		// })
		//
		// It("Should admit creation if all required fields are present", func() {
		//     By("simulating an invalid creation scenario")
		//     obj.SomeRequiredField = "valid_value"
		//     Expect(validator.ValidateCreate(ctx, obj)).To(BeNil())
		// })
		//
		// It("Should validate updates correctly", func() {
		//     By("simulating a valid update scenario")
		//     oldObj.SomeRequiredField = "updated_value"
		//     obj.SomeRequiredField = "updated_value"
		//     Expect(validator.ValidateUpdate(ctx, oldObj, obj)).To(BeNil())
		// })
	})

	Context("GCS storage validation", func() {
		var ctx context.Context

		BeforeEach(func() {
			ctx = context.Background()
			obj.Spec.Storage.StorageType = cnpgextensionsv1beta1.StorageTypeGCS
		})

		It("Should deny creation when GCS storage type is set but GCS config is missing", func() {
			obj.Spec.Storage.GCS = nil
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get GCS-specific configuration"))
		})

		It("Should admit creation with a direct GCS prefix", func() {
			obj.Spec.Storage.GCS = &cnpgextensionsv1beta1.GCSStorageConfig{
				Prefix: "gs://test-bucket/prefix",
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should admit creation with a GCS prefix from a Secret reference", func() {
			obj.Spec.Storage.GCS = &cnpgextensionsv1beta1.GCSStorageConfig{
				PrefixFrom: &cnpgextensionsv1beta1.ValueFromSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "gcs-config"},
						Key:                  "prefix",
					},
				},
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should deny creation when both prefix and prefixFrom are specified", func() {
			obj.Spec.Storage.GCS = &cnpgextensionsv1beta1.GCSStorageConfig{
				Prefix: "gs://test-bucket/prefix",
				PrefixFrom: &cnpgextensionsv1beta1.ValueFromSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "gcs-config"},
						Key:                  "prefix",
					},
				},
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot specify both prefix and prefixFrom"))
		})

		It("Should deny creation when prefixFrom specifies neither secretKeyRef nor configMapKeyRef", func() {
			obj.Spec.Storage.GCS = &cnpgextensionsv1beta1.GCSStorageConfig{
				PrefixFrom: &cnpgextensionsv1beta1.ValueFromSource{},
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must specify either secretKeyRef or configMapKeyRef"))
		})
	})

})
