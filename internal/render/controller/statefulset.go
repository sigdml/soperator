package controller

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

// RenderStatefulSet renders new [appsv1.StatefulSet] containing Slurm controller pods
func RenderStatefulSet(
	namespace,
	clusterName string,
	nodeFilters []slurmv1.K8sNodeFilter,
	secrets *slurmv1.Secrets,
	volumeSources []slurmv1.VolumeSource,
	controller *values.SlurmController,
) (appsv1.StatefulSet, error) {
	labels := common.RenderLabels(consts.ComponentTypeController, clusterName)
	matchLabels := common.RenderMatchLabels(consts.ComponentTypeController, clusterName)

	stsVersion, podVersion, err := common.GenerateVersionsAnnotationPlaceholders()
	if err != nil {
		return appsv1.StatefulSet{}, fmt.Errorf("generating versions annotation placeholders: %w", err)
	}

	nodeFilter := utils.MustGetBy(
		nodeFilters,
		controller.K8sNodeFilterName,
		func(f slurmv1.K8sNodeFilter) string { return f.Name },
	)

	volumes := []corev1.Volume{
		common.RenderVolumeSlurmConfigs(clusterName),
		common.RenderVolumeMungeKey(secrets.MungeKey.Name, secrets.MungeKey.Key),
		common.RenderVolumeMungeSocket(),
	}
	var pvcTemplateSpecs []values.PVCTemplateSpec

	{
		if controller.VolumeSpool.VolumeSourceName != nil {
			volumes = append(
				volumes,
				common.RenderVolumeSpoolFromSource(
					consts.ComponentTypeController,
					volumeSources,
					*controller.VolumeSpool.VolumeSourceName,
				),
			)
		} else {
			pvcTemplateSpecs = append(
				pvcTemplateSpecs,
				values.PVCTemplateSpec{
					Name: common.RenderVolumeNameSpool(consts.ComponentTypeController),
					Spec: controller.VolumeSpool.VolumeClaimTemplateSpec,
				},
			)
		}
	}
	{
		if controller.VolumeJail.VolumeSourceName != nil {
			volumes = append(
				volumes,
				common.RenderVolumeJailFromSource(
					volumeSources,
					*controller.VolumeJail.VolumeSourceName,
				),
			)
		} else {
			pvcTemplateSpecs = append(
				pvcTemplateSpecs,
				values.PVCTemplateSpec{
					Name: consts.VolumeNameJail,
					Spec: controller.VolumeJail.VolumeClaimTemplateSpec,
				},
			)
		}
	}

	return appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      controller.StatefulSet.Name,
			Namespace: namespace,
			Labels:    labels,
			Annotations: map[string]string{
				consts.AnnotationVersions: string(stsVersion),
			},
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: controller.Service.Name,
			Replicas:    &controller.StatefulSet.Replicas,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
					MaxUnavailable: &controller.StatefulSet.MaxUnavailable,
				},
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
					Annotations: map[string]string{
						consts.AnnotationVersions: string(podVersion),
						fmt.Sprintf(
							"%s/%s", consts.AnnotationApparmorKey, consts.ContainerNameSlurmctld,
						): consts.AnnotationApparmorValueUnconfined,
						fmt.Sprintf(
							"%s/%s", consts.AnnotationApparmorKey, consts.ContainerNameMunge,
						): consts.AnnotationApparmorValueUnconfined,
					},
				},
				Spec: corev1.PodSpec{
					Affinity:     nodeFilter.Affinity,
					NodeSelector: nodeFilter.NodeSelector,
					Tolerations:  nodeFilter.Tolerations,
					Containers: []corev1.Container{
						renderContainerSlurmctld(&controller.ContainerSlurmctld),
						common.RenderContainerMunge(&controller.ContainerMunge),
					},
					Volumes: volumes,
				},
			},
			VolumeClaimTemplates: common.RenderVolumeClaimTemplates(
				consts.ComponentTypeController,
				namespace,
				clusterName,
				pvcTemplateSpecs,
			),
		},
	}, nil
}