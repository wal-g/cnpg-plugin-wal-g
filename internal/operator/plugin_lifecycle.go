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
	"errors"
	"fmt"
	"strings"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cnpg-i-machinery/pkg/pluginhelper/decoder"
	"github.com/cloudnative-pg/cnpg-i-machinery/pkg/pluginhelper/object"
	"github.com/cloudnative-pg/cnpg-i/pkg/lifecycle"
	"github.com/go-logr/logr"
	"github.com/spf13/viper"
	"github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// LifecycleImplementation is the implementation of the lifecycle handler
type LifecycleImplementation struct {
	lifecycle.UnimplementedOperatorLifecycleServer
	Client client.Client
}

// GetCapabilities exposes the lifecycle capabilities
func (impl LifecycleImplementation) GetCapabilities(
	_ context.Context,
	_ *lifecycle.OperatorLifecycleCapabilitiesRequest,
) (*lifecycle.OperatorLifecycleCapabilitiesResponse, error) {
	return &lifecycle.OperatorLifecycleCapabilitiesResponse{
		LifecycleCapabilities: []*lifecycle.OperatorLifecycleCapabilities{
			{
				Group: "",
				Kind:  "Pod",
				OperationTypes: []*lifecycle.OperatorOperationType{
					{
						Type: lifecycle.OperatorOperationType_TYPE_CREATE,
					},
					{
						Type: lifecycle.OperatorOperationType_TYPE_PATCH,
					},
					{
						Type: lifecycle.OperatorOperationType_TYPE_EVALUATE,
					},
				},
			},
			{
				Group: batchv1.GroupName,
				Kind:  "Job",
				OperationTypes: []*lifecycle.OperatorOperationType{
					{
						Type: lifecycle.OperatorOperationType_TYPE_CREATE,
					},
				},
			},
		},
	}, nil
}

// LifecycleHook is called on Kubernetes Pods / Jobs creation by CNPG
func (impl LifecycleImplementation) LifecycleHook(
	ctx context.Context,
	request *lifecycle.OperatorLifecycleRequest,
) (*lifecycle.OperatorLifecycleResponse, error) {

	operation := request.GetOperationType().GetType().Enum()
	if operation == nil {
		return nil, errors.New("no operation set")
	}

	kind, err := object.GetKind(request.GetObjectDefinition())
	if err != nil {
		return nil, err
	}

	var cluster cnpgv1.Cluster
	if err := decoder.DecodeObjectLenient(
		request.GetClusterDefinition(),
		&cluster,
	); err != nil {
		return nil, err
	}

	logger := logr.FromContextOrDiscard(ctx).WithName("lifecycleHook").
		WithValues("clusterName", cluster.Name, "clusterNamespace", cluster.Namespace, "entity", kind)
	logger.V(1).Info("Lifecycle hook reconciliation start")
	childCtx := logr.NewContext(ctx, logger)

	switch kind {
	case "Pod":
		return impl.reconcilePod(childCtx, &cluster, request)
	case "Job":
		return impl.reconcileJob(childCtx, &cluster, request)
	default:
		return nil, fmt.Errorf("unsupported kind: %s", kind)
	}
}

// reconcilePod changes Kubernetes Pod resource if needed and returns patch with changes
func (impl LifecycleImplementation) reconcilePod(
	ctx context.Context,
	cluster *cnpgv1.Cluster,
	request *lifecycle.OperatorLifecycleRequest,
) (*lifecycle.OperatorLifecycleResponse, error) {
	pod, err := decoder.DecodePodJSON(request.GetObjectDefinition())
	if err != nil {
		return nil, err
	}

	logger := logr.FromContextOrDiscard(ctx).WithValues("podName", pod.Name, "podNamespace", pod.Namespace)
	logger.V(1).Info("Reconciling pod")

	mutatedPod := pod.DeepCopy()

	backupConfig, err := v1beta1.GetBackupConfigForCluster(ctx, impl.Client, cluster)
	if err != nil {
		return nil, fmt.Errorf("while getting backup configuration for cluster: %w", err)
	}
	if backupConfig == nil {
		logger.V(1).Info("No need to mutate instance with no backup & archiving configuration")
		return &lifecycle.OperatorLifecycleResponse{}, nil
	}

	if err := reconcilePodSpecWithPluginSidecar(
		cluster,
		backupConfig,
		&mutatedPod.Spec,
		"postgres",
		make([]corev1.EnvVar, 0),
	); err != nil {
		return nil, fmt.Errorf("while reconciling pod spec for pod: %w", err)
	}

	patch, err := object.CreatePatch(mutatedPod, pod)
	if err != nil {
		return nil, err
	}

	logger.V(1).Info("Generated patch for pod", "content", string(patch))
	return &lifecycle.OperatorLifecycleResponse{
		JsonPatch: patch,
	}, nil
}

func (impl LifecycleImplementation) reconcileJob(
	ctx context.Context,
	cluster *cnpgv1.Cluster,
	request *lifecycle.OperatorLifecycleRequest,
) (*lifecycle.OperatorLifecycleResponse, error) {

	var job batchv1.Job
	if err := decoder.DecodeObjectStrict(
		request.GetObjectDefinition(),
		&job,
		batchv1.SchemeGroupVersion.WithKind("Job"),
	); err != nil {
		return nil, err
	}

	logger := logr.FromContextOrDiscard(ctx).WithValues("jobName", job.Name, "jobNamespace", job.Namespace, "jobRole", getCNPGJobRole(&job))

	if getCNPGJobRole(&job) != "full-recovery" {
		logger.V(1).Info("job is not a recovery job, skipping")
		return &lifecycle.OperatorLifecycleResponse{}, nil
	}

	recoveryBackupConfig, err := v1beta1.GetBackupConfigForClusterRecovery(ctx, impl.Client, cluster)
	if err != nil {
		return nil, fmt.Errorf("while getting recovery backups configuration for cluster: %w", err)
	}
	if recoveryBackupConfig == nil {
		logger.V(1).Info("No need to mutate instance with no recovery backups configuration")
		return &lifecycle.OperatorLifecycleResponse{}, nil
	}

	mutatedJob := job.DeepCopy()

	if err := reconcilePodSpecWithPluginSidecar(
		cluster,
		recoveryBackupConfig,
		&mutatedJob.Spec.Template.Spec,
		"full-recovery",
		make([]corev1.EnvVar, 0),
	); err != nil {
		return nil, fmt.Errorf("while reconciling pod spec for job: %w", err)
	}

	patch, err := object.CreatePatch(mutatedJob, &job)
	if err != nil {
		return nil, err
	}

	return &lifecycle.OperatorLifecycleResponse{
		JsonPatch: patch,
	}, nil
}

// reconcilePodSpecWithPluginSidecar updates pod spec to have properly injected plugin sidecar container
// with appropriate volumes, environment variables and so on
func reconcilePodSpecWithPluginSidecar(
	cluster *cnpgv1.Cluster,
	backupConfig *v1beta1.BackupConfig,
	spec *corev1.PodSpec,
	jobRole string,
	additionalEnvs []corev1.EnvVar,
) error {
	envs := []corev1.EnvVar{
		{
			Name:  "NAMESPACE",
			Value: cluster.Namespace,
		},
		{
			Name:  "CLUSTER_NAME",
			Value: cluster.Name,
		},
		{
			Name:  "PGDATA",
			Value: "/var/lib/postgresql/data/pgdata",
		},
	}

	envs = append(envs, additionalEnvs...)

	// TODO: @endevir implement me
	// baseProbe := &corev1.Probe{
	// 	FailureThreshold: 10,
	// 	TimeoutSeconds:   10,
	// 	ProbeHandler: corev1.ProbeHandler{
	// 		Exec: &corev1.ExecAction{
	// 			Command: []string{"/manager", "healthcheck", "unix"},
	// 		},
	// 	},
	// }

	sidecarConfig := corev1.Container{}
	sidecarConfig.Name = "plugin-yandex-extensions"
	sidecarConfig.Image = viper.GetString("cnpg-i-pg-sidecar-image")
	sidecarConfig.ImagePullPolicy = cluster.Spec.ImagePullPolicy
	if jobRole == "full-recovery" {
		sidecarConfig.Args = []string{"instance", "--mode", "recovery"}
	} else {
		sidecarConfig.Args = []string{"instance", "--mode", "normal"}
	}

	sidecarConfig.Resources = backupConfig.Spec.Resources

	// TODO: @endevir implement me
	// sidecarConfig.StartupProbe = baseProbe.DeepCopy()
	sidecarConfig.SecurityContext = &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false),
		RunAsNonRoot:             ptr.To(true),
		Privileged:               ptr.To(false),
		ReadOnlyRootFilesystem:   ptr.To(true),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}

	// merge the main container envs if they aren't already set
	for i := range spec.Containers {
		container := &spec.Containers[i]
		if container.Name == jobRole {
			for _, env := range container.Env {
				found := false
				for _, existingEnv := range sidecarConfig.Env {
					if existingEnv.Name == env.Name {
						found = true
						break
					}
				}
				if !found {
					sidecarConfig.Env = append(sidecarConfig.Env, env)
				}
			}
			break
		}
	}

	// merge the default envs if they aren't already set
	for _, env := range envs {
		found := false
		for _, existingEnv := range sidecarConfig.Env {
			if existingEnv.Name == env.Name {
				found = true
				break
			}
		}
		if !found {
			sidecarConfig.Env = append(sidecarConfig.Env, env)
		}
	}

	if err := injectPluginSidecarPodSpec(spec, &sidecarConfig, jobRole); err != nil {
		return err
	}

	return nil
}

// injectPluginSidecarPodSpec injects a plugin sidecar into a CNPG Pod spec.
//
// If the "injectMainContainerVolumes" flag is true, this will append all the volume
// mounts that are used in the instance manager Pod to the passed sidecar
// container, granting it superuser access to the PostgreSQL instance.
func injectPluginSidecarPodSpec(
	spec *corev1.PodSpec,
	sidecar *corev1.Container,
	mainContainerName string,
) error {
	injectPluginVolumePodSpec(spec, mainContainerName)
	sidecar = sidecar.DeepCopy()

	var volumeMounts []corev1.VolumeMount
	sidecarContainerFound := false
	mainContainerFound := false
	for i := range spec.Containers {
		if spec.Containers[i].Name == mainContainerName {
			volumeMounts = spec.Containers[i].VolumeMounts
			mainContainerFound = true
		}
	}

	if !mainContainerFound {
		return errors.New("main container not found")
	}

	for i := range spec.InitContainers {
		if spec.InitContainers[i].Name == sidecar.Name {
			sidecarContainerFound = true
		}
	}

	if sidecarContainerFound {
		// The sidecar container was already added
		return nil
	}

	// Do not modify the passed sidecar definition
	sidecar.VolumeMounts = append(sidecar.VolumeMounts, volumeMounts...)
	sidecar.RestartPolicy = ptr.To(corev1.ContainerRestartPolicyAlways)
	spec.InitContainers = append(spec.InitContainers, *sidecar)
	return nil
}

// injectPluginVolumePodSpec injects the plugin volume into a CNPG Pod spec.
func injectPluginVolumePodSpec(spec *corev1.PodSpec, mainContainerName string) {
	const (
		pluginVolumeName = "plugins"
		pluginMountPath  = "/plugins"
	)

	foundPluginVolume := false
	for i := range spec.Volumes {
		if spec.Volumes[i].Name == pluginVolumeName {
			foundPluginVolume = true
		}
	}

	if foundPluginVolume {
		return
	}

	spec.Volumes = append(spec.Volumes, corev1.Volume{
		Name: pluginVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})

	for i := range spec.Containers {
		if spec.Containers[i].Name == mainContainerName {
			spec.Containers[i].VolumeMounts = append(
				spec.Containers[i].VolumeMounts,
				corev1.VolumeMount{
					Name:      pluginVolumeName,
					MountPath: pluginMountPath,
				},
			)
		}
	}
}

// getCNPGJobRole gets the role associated to a CNPG job
func getCNPGJobRole(job *batchv1.Job) string {
	const jobRoleLabelSuffix = "/jobRole"
	for k, v := range job.Spec.Template.Labels {
		if strings.HasSuffix(k, jobRoleLabelSuffix) {
			return v
		}
	}

	return ""
}
