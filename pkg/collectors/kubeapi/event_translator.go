package kubeapi

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/yairfalse/tapio/pkg/domain"
)

// ToDomainResourceEvent converts a kubeapi ResourceEvent to a domain ResourceEvent
// This translation happens at the boundary when events are serialized
func ToDomainResourceEvent(event ResourceEvent) domain.ResourceEvent {
	// Convert owner references
	ownerRefs := make([]domain.ResourceOwnerRef, len(event.OwnerReferences))
	for i, ref := range event.OwnerReferences {
		ownerRefs[i] = domain.ResourceOwnerRef{
			APIVersion: ref.APIVersion,
			Kind:       ref.Kind,
			Name:       ref.Name,
			UID:        string(ref.UID),
		}
	}

	// Convert related objects
	relatedObjs := make([]domain.ResourceObjectReference, len(event.RelatedObjects))
	for i, obj := range event.RelatedObjects {
		relatedObjs[i] = domain.ResourceObjectReference{
			APIVersion: obj.APIVersion,
			Kind:       obj.Kind,
			Name:       obj.Name,
			Namespace:  obj.Namespace,
			UID:        string(obj.UID),
			Relation:   obj.Relation,
		}
	}

	return domain.ResourceEvent{
		EventType:       event.EventType,
		Timestamp:       event.Timestamp,
		APIVersion:      event.APIVersion,
		Kind:            event.Kind,
		Name:            event.Name,
		Namespace:       event.Namespace,
		UID:             string(event.UID),
		OwnerReferences: ownerRefs,
		Labels:          event.Labels,
		Annotations:     event.Annotations,
		Object:          event.Object,
		OldObject:       event.OldObject,
		ResourceVersion: event.ResourceVersion,
		Reason:          event.Reason,
		Message:         event.Message,
		Source: domain.ResourceEventSource{
			Component: event.Source.Component,
			Host:      event.Source.Host,
		},
		RelatedObjects: relatedObjs,
	}
}

// FromDomainResourceEvent converts a domain ResourceEvent back to a kubeapi ResourceEvent
// This is used when the kubeapi collector needs to work with domain events
func FromDomainResourceEvent(event domain.ResourceEvent) ResourceEvent {
	// Convert owner references
	ownerRefs := make([]OwnerRef, len(event.OwnerReferences))
	for i, ref := range event.OwnerReferences {
		ownerRefs[i] = OwnerRef{
			APIVersion: ref.APIVersion,
			Kind:       ref.Kind,
			Name:       ref.Name,
			UID:        types.UID(ref.UID),
		}
	}

	// Convert related objects
	relatedObjs := make([]ObjectReference, len(event.RelatedObjects))
	for i, obj := range event.RelatedObjects {
		relatedObjs[i] = ObjectReference{
			APIVersion: obj.APIVersion,
			Kind:       obj.Kind,
			Name:       obj.Name,
			Namespace:  obj.Namespace,
			UID:        types.UID(obj.UID),
			Relation:   obj.Relation,
		}
	}

	return ResourceEvent{
		EventType:       event.EventType,
		Timestamp:       event.Timestamp,
		APIVersion:      event.APIVersion,
		Kind:            event.Kind,
		Name:            event.Name,
		Namespace:       event.Namespace,
		UID:             types.UID(event.UID),
		OwnerReferences: ownerRefs,
		Labels:          event.Labels,
		Annotations:     event.Annotations,
		Object:          toRuntimeObject(event.Object),
		OldObject:       toRuntimeObject(event.OldObject),
		ResourceVersion: event.ResourceVersion,
		Reason:          event.Reason,
		Message:         event.Message,
		Source: EventSource{
			Component: event.Source.Component,
			Host:      event.Source.Host,
		},
		RelatedObjects: relatedObjs,
	}
}

// toRuntimeObject converts interface{} to runtime.Object
// Returns nil if the conversion isn't possible
func toRuntimeObject(obj interface{}) runtime.Object {
	if obj == nil {
		return nil
	}
	if runtimeObj, ok := obj.(runtime.Object); ok {
		return runtimeObj
	}
	return nil
}
