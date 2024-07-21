package storageclasses

import (
	"github.com/loft-sh/vcluster/pkg/syncer/synccontext"
	storagev1 "k8s.io/api/storage/v1"
)

func (s *storageClassSyncer) translate(ctx *synccontext.SyncContext, vStorageClass *storagev1.StorageClass) *storagev1.StorageClass {
	return s.TranslateMetadata(ctx, vStorageClass).(*storagev1.StorageClass)
}

func (s *storageClassSyncer) translateUpdate(ctx *synccontext.SyncContext, pObj, vObj *storagev1.StorageClass) {
	_, updatedAnnotations, updatedLabels := s.TranslateMetadataUpdate(ctx, vObj, pObj)
	pObj.Labels = updatedLabels
	pObj.Annotations = updatedAnnotations

	pObj.Provisioner = vObj.Provisioner

	pObj.Parameters = vObj.Parameters

	pObj.ReclaimPolicy = vObj.ReclaimPolicy

	pObj.MountOptions = vObj.MountOptions

	pObj.AllowVolumeExpansion = vObj.AllowVolumeExpansion

	pObj.VolumeBindingMode = vObj.VolumeBindingMode

	pObj.AllowedTopologies = vObj.AllowedTopologies
}
