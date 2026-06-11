package resourcecachingclient

import (
	"context"
	"errors"

	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// NarrowSource connects a per-object watch (started by Client.GetSource) to a controller workqueue.
type NarrowSource struct {
	source.Source
	handler handler.EventHandler
	onStart func(ctx context.Context, q workqueue.TypedRateLimitingInterface[reconcile.Request])
}

// Start registers event handlers on the workqueue; the watch goroutine is started separately.
// Start must return immediately (controller-runtime v0.20+).
func (s *NarrowSource) Start(ctx context.Context, q workqueue.TypedRateLimitingInterface[reconcile.Request]) error {
	if s.handler == nil {
		return errors.New("must specify NarrowSource.handler")
	}
	s.onStart(ctx, q)
	return nil
}

var _ source.Source = &NarrowSource{}
