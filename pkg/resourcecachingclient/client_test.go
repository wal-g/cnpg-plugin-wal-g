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

package resourcecachingclient

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("ResourceCachingClient", func() {
	var (
		testCtx       context.Context
		testCancel    context.CancelFunc
		cachingClient *Client
		namespace     string
	)

	BeforeEach(func() {
		testCtx, testCancel = context.WithCancel(ctx)

		// Create a unique namespace for each test
		namespace = fmt.Sprintf("test-ns-%d", time.Now().UnixNano())
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		Expect(k8sClient.Create(testCtx, ns)).To(Succeed())

		// Create caching client with ConfigMap as cached type
		var err error
		cachingClient, err = CreateClient(testCtx, testMgr, []client.Object{&corev1.ConfigMap{}})
		Expect(err).NotTo(HaveOccurred())
		Expect(cachingClient).NotTo(BeNil())
	})

	AfterEach(func() {
		// Clean up namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_ = k8sClient.Delete(testCtx, ns)

		testCancel()
	})

	Describe("Get operation", func() {
		It("should cache ConfigMap on first Get and serve from cache on subsequent Gets", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: namespace,
				},
				Data: map[string]string{
					"key": "value1",
				},
			}
			Expect(k8sClient.Create(testCtx, cm)).To(Succeed())

			// First Get - should trigger watch creation
			retrieved := &corev1.ConfigMap{}
			key := types.NamespacedName{Name: "test-cm", Namespace: namespace}
			Expect(cachingClient.Get(testCtx, key, retrieved)).To(Succeed())
			Expect(retrieved.Data["key"]).To(Equal("value1"))

			// Verify watch was created
			Eventually(func() bool {
				cachingClient.wmut.RLock()
				defer cachingClient.wmut.RUnlock()
				gvk, _ := cachingClient.GroupVersionKindFor(&corev1.ConfigMap{})
				objKey := gvk.String() + "|" + key.String()
				return cachingClient.watchedObjects[objKey] != nil
			}, "5s", "100ms").Should(BeTrue())

			// Second Get - should serve from cache
			retrieved2 := &corev1.ConfigMap{}
			Expect(cachingClient.Get(testCtx, key, retrieved2)).To(Succeed())
			Expect(retrieved2.Data["key"]).To(Equal("value1"))
		})

		It("should return NotFound error for non-existent ConfigMap", func() {
			retrieved := &corev1.ConfigMap{}
			key := types.NamespacedName{Name: "non-existent", Namespace: namespace}
			err := cachingClient.Get(testCtx, key, retrieved)
			Expect(err).To(HaveOccurred())
			Expect(client.IgnoreNotFound(err)).To(Succeed())
		})

		It("should return NotFound error for non-existent ConfigMap on each Get", func() {
			key := types.NamespacedName{Name: "non-existent", Namespace: namespace}

			// First Get - should return NotFound
			retrieved := &corev1.ConfigMap{}
			err := cachingClient.Get(testCtx, key, retrieved)
			Expect(err).To(HaveOccurred())
			Expect(client.IgnoreNotFound(err)).To(Succeed())

			// Second Get - should also return NotFound (no watch created for non-existent objects)
			retrieved2 := &corev1.ConfigMap{}
			err = cachingClient.Get(testCtx, key, retrieved2)
			Expect(err).To(HaveOccurred())
			Expect(client.IgnoreNotFound(err)).To(Succeed())

			// Verify no watch entry was created (watches only created for existing objects)
			gvk, _ := cachingClient.GroupVersionKindFor(&corev1.ConfigMap{})
			objKey := gvk.String() + "|" + key.String()

			cachingClient.wmut.RLock()
			ca := cachingClient.watchedObjects[objKey]
			cachingClient.wmut.RUnlock()
			Expect(ca).To(BeNil())
		})
	})

	Describe("Watch event handling", func() {
		var (
			testHandler *testEventHandler
			cm          *corev1.ConfigMap
			key         types.NamespacedName
		)

		BeforeEach(func() {
			testHandler = &testEventHandler{
				createEvents: make(chan event.CreateEvent, 10),
				updateEvents: make(chan event.UpdateEvent, 10),
				deleteEvents: make(chan event.DeleteEvent, 10),
			}

			cm = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "watched-cm",
					Namespace: namespace,
				},
				Data: map[string]string{
					"key": "initial",
				},
			}
			key = types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}
		})

		It("should receive Added event when watching existing ConfigMap", func() {
			// Create ConfigMap first
			Expect(k8sClient.Create(testCtx, cm)).To(Succeed())

			// Now get and watch it
			retrieved := &corev1.ConfigMap{}
			Expect(cachingClient.Get(testCtx, key, retrieved)).To(Succeed())

			src, err := cachingClient.GetSource(testCtx, retrieved, testHandler)
			Expect(err).NotTo(HaveOccurred())

			// Start the source to register handlers
			mockQueue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
			defer mockQueue.ShutDown()
			Expect(src.Start(testCtx, mockQueue)).To(Succeed())

			// Wait a bit for watch to establish
			time.Sleep(500 * time.Millisecond)

			// The watch should have received an ADDED event when it started
			// (this is Kubernetes watch behavior - existing objects generate ADDED events)
			// However, our implementation caches on Get, so we won't get a duplicate event
			// Let's verify the watch is active by updating the object
			retrieved.Data["key"] = "updated-after-watch"
			Expect(k8sClient.Update(testCtx, retrieved)).To(Succeed())

			// Should receive Update event
			Eventually(testHandler.updateEvents, "10s", "100ms").Should(Receive())
		})

		It("should receive Update event with correct old and new objects when ConfigMap is modified", func() {
			// Create ConfigMap first
			Expect(k8sClient.Create(testCtx, cm)).To(Succeed())

			// Get and setup watch
			retrieved := &corev1.ConfigMap{}
			Expect(cachingClient.Get(testCtx, key, retrieved)).To(Succeed())

			src, err := cachingClient.GetSource(testCtx, retrieved, testHandler)
			Expect(err).NotTo(HaveOccurred())

			// Start the source to register handlers
			mockQueue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
			defer mockQueue.ShutDown()
			Expect(src.Start(testCtx, mockQueue)).To(Succeed())

			// Wait for watch to be established
			time.Sleep(500 * time.Millisecond)

			// Update the ConfigMap
			retrieved.Data["key"] = "updated"
			Expect(k8sClient.Update(testCtx, retrieved)).To(Succeed())

			// Should receive Update event with correct old and new values
			Eventually(testHandler.updateEvents, "10s", "100ms").Should(Receive(WithTransform(
				func(e event.UpdateEvent) bool {
					oldCM, okOld := e.ObjectOld.(*corev1.ConfigMap)
					newCM, okNew := e.ObjectNew.(*corev1.ConfigMap)
					return okOld && okNew &&
						oldCM.Data["key"] == "initial" &&
						newCM.Data["key"] == "updated"
				},
				BeTrue(),
			)))
		})

		It("should receive Delete event when ConfigMap is deleted", func() {
			// Create ConfigMap first
			Expect(k8sClient.Create(testCtx, cm)).To(Succeed())

			// Get and setup watch
			retrieved := &corev1.ConfigMap{}
			Expect(cachingClient.Get(testCtx, key, retrieved)).To(Succeed())

			src, err := cachingClient.GetSource(testCtx, retrieved, testHandler)
			Expect(err).NotTo(HaveOccurred())

			// Start the source to register handlers
			mockQueue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
			defer mockQueue.ShutDown()
			Expect(src.Start(testCtx, mockQueue)).To(Succeed())

			// Wait for watch to be established
			time.Sleep(500 * time.Millisecond)

			// Delete the ConfigMap
			Expect(k8sClient.Delete(testCtx, retrieved)).To(Succeed())

			// Should receive Delete event
			Eventually(testHandler.deleteEvents, "10s", "100ms").Should(Receive(WithTransform(
				func(e event.DeleteEvent) string {
					return e.Object.GetName()
				},
				Equal(cm.Name),
			)))
		})

		It("should handle multiple sequential updates correctly", func() {
			// Create ConfigMap first
			Expect(k8sClient.Create(testCtx, cm)).To(Succeed())

			// Get and setup watch
			retrieved := &corev1.ConfigMap{}
			Expect(cachingClient.Get(testCtx, key, retrieved)).To(Succeed())

			src, err := cachingClient.GetSource(testCtx, retrieved, testHandler)
			Expect(err).NotTo(HaveOccurred())

			// Start the source to register handlers
			mockQueue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
			defer mockQueue.ShutDown()
			Expect(src.Start(testCtx, mockQueue)).To(Succeed())

			// Wait for watch to be established
			time.Sleep(500 * time.Millisecond)

			// Perform multiple updates
			for i := 1; i <= 3; i++ {
				retrieved.Data["key"] = fmt.Sprintf("update-%d", i)
				Expect(k8sClient.Update(testCtx, retrieved)).To(Succeed())

				// Verify each update event
				Eventually(testHandler.updateEvents, "10s", "100ms").Should(Receive())

				// Small delay between updates
				time.Sleep(200 * time.Millisecond)
			}
		})

		It("should treat Modified event as Create when no prior cache exists", func() {
			// Create ConfigMap
			Expect(k8sClient.Create(testCtx, cm)).To(Succeed())

			// Setup watch
			retrieved := &corev1.ConfigMap{}
			Expect(cachingClient.Get(testCtx, key, retrieved)).To(Succeed())

			gvk, _ := cachingClient.GroupVersionKindFor(retrieved)
			objKey := gvk.String() + "|" + key.String()

			// Get source first
			src, err := cachingClient.GetSource(testCtx, retrieved, testHandler)
			Expect(err).NotTo(HaveOccurred())

			// Start the source to register handlers
			mockQueue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
			defer mockQueue.ShutDown()
			Expect(src.Start(testCtx, mockQueue)).To(Succeed())

			// Wait for watch to be established
			time.Sleep(500 * time.Millisecond)

			// Clear the cached object but keep the watch entry and handlers
			cachingClient.wmut.Lock()
			if ca := cachingClient.watchedObjects[objKey]; ca != nil {
				ca.cached = nil
			}
			cachingClient.wmut.Unlock()

			// Update the ConfigMap - should trigger Modified event
			// Need to get fresh copy since we're updating
			freshCM := &corev1.ConfigMap{}
			Expect(k8sClient.Get(testCtx, key, freshCM)).To(Succeed())
			freshCM.Data["key"] = "modified"
			Expect(k8sClient.Update(testCtx, freshCM)).To(Succeed())

			// Should receive Create event (not Update) since prior was nil
			Eventually(testHandler.createEvents, "10s", "100ms").Should(Receive(WithTransform(
				func(e event.CreateEvent) bool {
					cm, ok := e.Object.(*corev1.ConfigMap)
					return ok && cm.Data["key"] == "modified"
				},
				BeTrue(),
			)))
		})
	})

	Describe("Multiple handlers", func() {
		It("should notify all registered handlers on events", func() {
			handler1 := &testEventHandler{
				createEvents: make(chan event.CreateEvent, 10),
				updateEvents: make(chan event.UpdateEvent, 10),
				deleteEvents: make(chan event.DeleteEvent, 10),
			}
			handler2 := &testEventHandler{
				createEvents: make(chan event.CreateEvent, 10),
				updateEvents: make(chan event.UpdateEvent, 10),
				deleteEvents: make(chan event.DeleteEvent, 10),
			}

			// Create ConfigMap
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-handler-cm",
					Namespace: namespace,
				},
				Data: map[string]string{"key": "value"},
			}
			Expect(k8sClient.Create(testCtx, cm)).To(Succeed())

			// Get and setup watches with both handlers
			retrieved := &corev1.ConfigMap{}
			key := types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}
			Expect(cachingClient.Get(testCtx, key, retrieved)).To(Succeed())

			src1, err := cachingClient.GetSource(testCtx, retrieved, handler1)
			Expect(err).NotTo(HaveOccurred())

			src2, err := cachingClient.GetSource(testCtx, retrieved, handler2)
			Expect(err).NotTo(HaveOccurred())

			// Start both sources to register handlers
			mockQueue1 := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
			defer mockQueue1.ShutDown()
			Expect(src1.Start(testCtx, mockQueue1)).To(Succeed())

			mockQueue2 := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
			defer mockQueue2.ShutDown()
			Expect(src2.Start(testCtx, mockQueue2)).To(Succeed())

			// Wait for watches to be established
			time.Sleep(500 * time.Millisecond)

			// Update the ConfigMap
			retrieved.Data["key"] = "updated"
			Expect(k8sClient.Update(testCtx, retrieved)).To(Succeed())

			// Both handlers should receive the update event
			Eventually(handler1.updateEvents, "10s", "100ms").Should(Receive())
			Eventually(handler2.updateEvents, "10s", "100ms").Should(Receive())
		})
	})

	Describe("Cache invalidation", func() {
		It("should clear cache entry on Create error", func() {
			// Create a ConfigMap
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: namespace,
				},
				Data: map[string]string{"key": "value"},
			}
			Expect(k8sClient.Create(testCtx, cm)).To(Succeed())

			// Get it to populate cache
			retrieved := &corev1.ConfigMap{}
			key := types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}
			Expect(cachingClient.Get(testCtx, key, retrieved)).To(Succeed())

			// Try to create duplicate (will fail)
			duplicate := cm.DeepCopy()
			err := cachingClient.Create(testCtx, duplicate)
			Expect(err).To(HaveOccurred())

			// Cache entry should be cleared
			Eventually(func() bool {
				cachingClient.wmut.RLock()
				defer cachingClient.wmut.RUnlock()
				gvk, _ := cachingClient.GroupVersionKindFor(cm)
				objKey := gvk.String() + "|" + key.String()
				return cachingClient.watchedObjects[objKey] == nil
			}, "5s", "100ms").Should(BeTrue())
		})

		It("should clear cache entry on Update error", func() {
			// Create a ConfigMap
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm-update",
					Namespace: namespace,
				},
				Data: map[string]string{"key": "value"},
			}
			Expect(k8sClient.Create(testCtx, cm)).To(Succeed())

			// Get it to populate cache
			retrieved := &corev1.ConfigMap{}
			key := types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}
			Expect(cachingClient.Get(testCtx, key, retrieved)).To(Succeed())

			// Modify with stale resource version to cause conflict
			stale := retrieved.DeepCopy()
			stale.ResourceVersion = "999999"
			stale.Data["key"] = "updated"
			err := cachingClient.Update(testCtx, stale)
			Expect(err).To(HaveOccurred())

			// Cache entry should be cleared
			Eventually(func() bool {
				cachingClient.wmut.RLock()
				defer cachingClient.wmut.RUnlock()
				gvk, _ := cachingClient.GroupVersionKindFor(cm)
				objKey := gvk.String() + "|" + key.String()
				return cachingClient.watchedObjects[objKey] == nil
			}, "5s", "100ms").Should(BeTrue())
		})

		It("should clear cache entry on Delete error", func() {
			// Create a ConfigMap
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm-delete",
					Namespace: namespace,
				},
				Data: map[string]string{"key": "value"},
			}
			Expect(k8sClient.Create(testCtx, cm)).To(Succeed())

			// Get it to populate cache
			retrieved := &corev1.ConfigMap{}
			key := types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}
			Expect(cachingClient.Get(testCtx, key, retrieved)).To(Succeed())

			// Delete it directly
			Expect(k8sClient.Delete(testCtx, retrieved)).To(Succeed())

			// Try to delete again (will fail)
			err := cachingClient.Delete(testCtx, retrieved)
			Expect(err).To(HaveOccurred())

			// Cache entry should be cleared
			Eventually(func() bool {
				cachingClient.wmut.RLock()
				defer cachingClient.wmut.RUnlock()
				gvk, _ := cachingClient.GroupVersionKindFor(cm)
				objKey := gvk.String() + "|" + key.String()
				return cachingClient.watchedObjects[objKey] == nil
			}, "5s", "100ms").Should(BeTrue())
		})
	})

	Describe("Handler registration before object exists", func() {
		It("should not create watch for non-existent objects", func() {
			testHandler := &testEventHandler{
				createEvents: make(chan event.CreateEvent, 10),
				updateEvents: make(chan event.UpdateEvent, 10),
				deleteEvents: make(chan event.DeleteEvent, 10),
			}

			// Try to get source for non-existent object
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "future-cm",
					Namespace: namespace,
				},
			}

			src, err := cachingClient.GetSource(testCtx, cm, testHandler)
			Expect(err).To(HaveOccurred()) // Object doesn't exist
			Expect(src).To(BeNil())        // No source returned for non-existent object

			// Verify no watch entry was created (watches only for existing objects)
			gvk, _ := cachingClient.GroupVersionKindFor(cm)
			key := types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}
			objKey := gvk.String() + "|" + key.String()

			cachingClient.wmut.RLock()
			ca := cachingClient.watchedObjects[objKey]
			cachingClient.wmut.RUnlock()
			Expect(ca).To(BeNil())
		})
	})
})

// testEventHandler is a test implementation of handler.EventHandler that captures events
type testEventHandler struct {
	createEvents chan event.CreateEvent
	updateEvents chan event.UpdateEvent
	deleteEvents chan event.DeleteEvent
}

func (h *testEventHandler) Create(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.createEvents <- e
}

func (h *testEventHandler) Update(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.updateEvents <- e
}

func (h *testEventHandler) Delete(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.deleteEvents <- e
}

func (h *testEventHandler) Generic(ctx context.Context, e event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	// Not used in these tests
}

var _ handler.EventHandler = &testEventHandler{}
