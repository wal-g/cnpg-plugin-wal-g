package resourcecachingclient

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/workqueue"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type Client struct {
	client.Client
	scheme         *runtime.Scheme
	config         *rest.Config
	mapper         meta.RESTMapper
	watchedGVKs    map[string]bool           // read only once init
	watchedObjects map[string]*watchedObject // mutex'ed
	wmut           sync.RWMutex              // for watchedObjects map
	ctx            context.Context           // context for long-running operations (ex. Watch requests)
}

// CreateClient creates a new client layer that sits on top of the provided `underlying` client.
// This client implements Get for the provided GVKs, using a specific cache.
//
// Other kinds of requests (ie. non-get and non-managed GVKs) are forwarded to the `underlying`
// client.
//
// Furthermore, cached objects are also available as `source.Source`.
func CreateClient(ctx context.Context, mgr manager.Manager, cachedObjectTypes []client.Object) (*Client, error) {
	watchedGVKs := make(map[string]bool)

	for _, obj := range cachedObjectTypes {
		gvk, err := mgr.GetClient().GroupVersionKindFor(obj)
		if err != nil {
			return nil, err
		}
		watchedGVKs[gvk.String()] = true
	}

	return &Client{
		Client:         mgr.GetClient(),
		watchedGVKs:    watchedGVKs,
		scheme:         mgr.GetScheme(),
		config:         mgr.GetConfig(),
		mapper:         mgr.GetRESTMapper(),
		watchedObjects: make(map[string]*watchedObject),
		ctx:            ctx,
	}, nil
}

type watchedObject struct {
	cached   client.Object
	handlers []handlerOnQueue
}

type handlerOnQueue struct {
	handler handler.EventHandler
	queue   workqueue.TypedRateLimitingInterface[reconcile.Request]
}

func (c *Client) Get(ctx context.Context, key client.ObjectKey, out client.Object, opts ...client.GetOption) error {
	gvk, err := c.GroupVersionKindFor(out)
	if err != nil {
		return err
	}
	strGVK := gvk.String()
	if _, managed := c.watchedGVKs[strGVK]; managed {
		// Kind is managed by this cache layer => check for watch
		_, err := c.getAndCreateWatchIfNeeded(ctx, out, gvk, key)
		if err != nil {
			return err
		}
		return nil
	}

	return c.Client.Get(ctx, key, out, opts...)
}

func (c *Client) getAndCreateWatchIfNeeded(ctx context.Context, obj client.Object, gvk schema.GroupVersionKind, key client.ObjectKey) (string, error) {
	strGVK := gvk.String()
	objKey := strGVK + "|" + key.String()

	// Checking if object presented in cache
	c.wmut.RLock()
	ca := c.watchedObjects[objKey]
	c.wmut.RUnlock()
	if ca != nil {
		if ca.cached == nil {
			return objKey, errors.NewNotFound(schema.GroupResource{Group: gvk.Group, Resource: gvk.Kind}, key.Name)
		}
		// Return from cache
		err := copyInto(ca.cached, obj)
		if err != nil {
			return objKey, err
		}
		return objKey, nil
	}

	// Live query
	rlog := log.FromContext(ctx).WithName("narrowcache").WithValues("objKey", objKey)
	rlog.Info("Cache miss, doing live query")

	// Get mapping
	mapping, err := c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return objKey, err
	}

	err = c.typedGet(ctx, mapping, key, obj)
	if err != nil {
		return objKey, err
	}

	// Create watch for later calls
	w, err := c.typedWatch(c.ctx, mapping, key)
	if err != nil {
		return objKey, err
	}

	// Store fetched object
	err = c.setToCache(objKey, obj)
	if err != nil {
		return objKey, err
	}

	// Start updating goroutine
	go c.updateCache(c.ctx, objKey, w)
	return objKey, nil
}

func (c *Client) restClientForMapping(mapping *meta.RESTMapping) (rest.Interface, error) {
	httpClient, err := rest.HTTPClientFor(c.config)
	if err != nil {
		return nil, fmt.Errorf("while creating HTTPClientFor typed watch: %w", err)
	}

	restClient, err := apiutil.RESTClientForGVK(mapping.GroupVersionKind, false, c.config, serializer.NewCodecFactory(c.scheme), httpClient)

	if err != nil {
		return nil, fmt.Errorf("while creating RESTClientForGVK %s: %w", mapping.GroupVersionKind.String(), err)
	}

	return restClient, nil
}

func (c *Client) typedGet(ctx context.Context, mapping *meta.RESTMapping, key client.ObjectKey, obj client.Object) error {
	restClient, err := c.restClientForMapping(mapping)
	if err != nil {
		return err
	}

	return restClient.Get().
		Namespace(key.Namespace).
		Resource(mapping.Resource.Resource).
		Name(key.Name).Do(ctx).Into(obj)
}

func (c *Client) typedWatch(ctx context.Context, mapping *meta.RESTMapping, key client.ObjectKey) (watch.Interface, error) {
	logger := log.FromContext(ctx).WithName("narrowcache").V(1) // Debug logs only for this method
	restClient, err := c.restClientForMapping(mapping)
	if err != nil {
		return nil, err
	}

	// ListOption with FieldSelector to authorize watch with resourceName scoped permissions
	opts := metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(metav1.ObjectNameField, key.Name).String(),
		Watch:         true,
	}

	r := restClient.Get().
		Namespace(key.Namespace).
		Resource(mapping.Resource.Resource).
		VersionedParams(&opts, runtime.NewParameterCodec(c.scheme))

	logger.Info("performing watch request", "url", r.URL().String())
	return r.Watch(ctx)
}

// "Terrible hack" cc directxman12 / sigs.k8s.io/controller-runtime/pkg/cache/internal/cache_reader.go
func copyInto(obj runtime.Object, out client.Object) error {
	// Copy the value of the item in the cache to the returned value
	// TODO(directxman12): this is a terrible hack, pls fix (we should have deepcopyinto)
	outVal := reflect.ValueOf(out)
	objVal := reflect.ValueOf(obj)
	if !objVal.Type().AssignableTo(outVal.Type()) {
		return fmt.Errorf("cache had type %s, but %s was asked for", objVal.Type(), outVal.Type())
	}
	reflect.Indirect(outVal).Set(reflect.Indirect(objVal))
	// if !c.disableDeepCopy {
	// 	out.GetObjectKind().SetGroupVersionKind(c.groupVersionKind)
	// }
	return nil
}

func (c *Client) updateCache(ctx context.Context, key string, watcher watch.Interface) {
	logger := log.FromContext(ctx).WithName("narrowcache").V(1) // Debug logs only for this method
	for watchEvent := range watcher.ResultChan() {
		logger.WithValues("key", key, "event type", watchEvent.Type, "event data", watchEvent.Object).Info("Event received")
		if watchEvent.Type == watch.Added || watchEvent.Type == watch.Modified {
			err := c.setToCache(key, watchEvent.Object)
			if err != nil {
				logger.WithValues("key", key).Error(err, "Error while updating cache")
			}
		} else if watchEvent.Type == watch.Deleted {
			c.removeFromCache(key)
		}
		c.callHandlers(ctx, key, watchEvent)
	}
	logger.WithValues("key", key).Info("Watch terminated. Clearing cache entry.")
	c.clearEntryByKey(key)
}

func (c *Client) setToCache(key string, obj runtime.Object) error {
	cObj, ok := obj.(client.Object)
	if !ok {
		return fmt.Errorf("could not convert runtime.Object to client.Object")
	}
	c.wmut.Lock()
	defer c.wmut.Unlock()
	if ca := c.watchedObjects[key]; ca != nil {
		ca.cached = cObj
	} else {
		c.watchedObjects[key] = &watchedObject{cached: cObj}
	}
	return nil
}

func (c *Client) removeFromCache(key string) {
	c.wmut.Lock()
	defer c.wmut.Unlock()
	if ca := c.watchedObjects[key]; ca != nil {
		ca.cached = nil
	}
}

func (c *Client) addHandler(key string, hoq handlerOnQueue) {
	c.wmut.Lock()
	defer c.wmut.Unlock()
	if ca := c.watchedObjects[key]; ca != nil {
		ca.handlers = append(ca.handlers, hoq)
	}
}

func (c *Client) callHandlers(ctx context.Context, key string, ev watch.Event) {
	var fn func(hoq handlerOnQueue)
	switch ev.Type {
	case watch.Added:
		fn = func(hoq handlerOnQueue) {
			createEvent := event.CreateEvent{Object: ev.Object.(client.Object)}
			hoq.handler.Create(ctx, createEvent, hoq.queue)
		}
	case watch.Modified:
		fn = func(hoq handlerOnQueue) {
			// old object unknown (not an issue for us - we just enqueue reconcile requests)
			modEvent := event.UpdateEvent{ObjectOld: ev.Object.(client.Object), ObjectNew: ev.Object.(client.Object)}
			hoq.handler.Update(ctx, modEvent, hoq.queue)
		}
	case watch.Deleted:
		fn = func(hoq handlerOnQueue) {
			delEvent := event.DeleteEvent{Object: ev.Object.(client.Object)}
			hoq.handler.Delete(ctx, delEvent, hoq.queue)
		}
	case watch.Bookmark:
	case watch.Error:
		// Not managed
	}
	if fn == nil {
		return
	}
	c.wmut.RLock()
	defer c.wmut.RUnlock()
	if ca := c.watchedObjects[key]; ca != nil {
		for _, hoq := range ca.handlers {
			h := hoq
			go fn(h)
		}
	}
}

func (c *Client) GetSource(ctx context.Context, obj client.Object, h handler.EventHandler) (source.Source, error) {
	// Prepare a Source and make sure it is associated with a watch
	rlog := log.FromContext(ctx).WithName("narrowcache")
	rlog.WithValues("name", obj.GetName(), "namespace", obj.GetNamespace()).Info("Getting Source:")
	gvk, err := c.GroupVersionKindFor(obj)
	if err != nil {
		return nil, err
	}
	strGVK := gvk.String()
	_, managed := c.watchedGVKs[strGVK]
	if !managed {
		return nil, fmt.Errorf("called 'GetSource' on unmanaged GVK: %s", strGVK)
	}

	key, err := c.getAndCreateWatchIfNeeded(ctx, obj, gvk, client.ObjectKeyFromObject(obj))
	if err != nil {
		return nil, err
	}

	return &NarrowSource{
		handler: h,
		onStart: func(_ context.Context, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			c.addHandler(key, handlerOnQueue{handler: h, queue: q})
		},
	}, nil
}

func (c *Client) clearEntry(ctx context.Context, obj client.Object) {
	key := types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}
	gvk, _ := c.GroupVersionKindFor(obj)
	strGVK := gvk.String()
	if _, managed := c.watchedGVKs[strGVK]; managed {
		log.FromContext(ctx).
			WithName("narrowcache").
			WithValues("name", obj.GetName(), "namespace", obj.GetNamespace()).
			Info("Invalidating cache entry")
		strGVK := gvk.String()
		objKey := strGVK + "|" + key.String()
		c.clearEntryByKey(objKey)
	}
}

func (c *Client) clearEntryByKey(key string) {
	// Note that this doesn't remove the watch, which lives in a goroutine
	// Watch would recreate cache object on received event, or it can be recreated on subsequent Get call
	c.wmut.Lock()
	defer c.wmut.Unlock()
	delete(c.watchedObjects, key)
}

func (c *Client) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if err := c.Client.Create(ctx, obj, opts...); err != nil {
		// might be due to an outdated cache, clear the corresponding entry
		c.clearEntry(ctx, obj)
		return err
	}
	return nil
}

func (c *Client) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if err := c.Client.Delete(ctx, obj, opts...); err != nil {
		// might be due to an outdated cache, clear the corresponding entry
		c.clearEntry(ctx, obj)
		return err
	}
	return nil
}

func (c *Client) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if err := c.Client.Update(ctx, obj, opts...); err != nil {
		// might be due to an outdated cache, clear the corresponding entry
		c.clearEntry(ctx, obj)
		return err
	}
	return nil
}
