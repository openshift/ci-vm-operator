package controller

import (
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeclientset "k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	vmapi "github.com/openshift/ci-vm-operator/pkg/apis/virtualmachines/v1alpha1"
	vmclient "github.com/openshift/ci-vm-operator/pkg/client/clientset/versioned/typed/virtualmachines/v1alpha1"
	vminformers "github.com/openshift/ci-vm-operator/pkg/client/informers/externalversions/virtualmachines/v1alpha1"
	vmlisters "github.com/openshift/ci-vm-operator/pkg/client/listers/virtualmachines/v1alpha1"
	"github.com/openshift/ci-vm-operator/pkg/metrics"
)

const (
	// maxRetries is the number of times a service will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the
	// sequence of delays between successive queuings of a service.
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s, 20.4s, 41s, 82s
	maxRetries = 15

	controllerName = "virtual-machines"
)

// NewController returns a new *Controller to use with virtual machines.
func New(config Configuration, informer vminformers.VirtualMachineInformer, client vmclient.VirtualMachinesGetter, kubeClient kubeclientset.Interface, gceClient GCEClient) *Controller {
	logger := logrus.WithField("controller", controllerName)
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(logger.Infof)
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})

	if kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		metrics.RegisterMetricAndTrackRateLimiterUsage(
			fmt.Sprintf("ci_%s_controller", controllerName),
			kubeClient.CoreV1().RESTClient().GetRateLimiter(),
		)
	}

	c := &Controller{
		config:     config,
		client:     client,
		kubeClient: kubeClient,
		gceClient:  gceClient,
		queue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), controllerName),
		logger:     logger,
		lister:     informer.Lister(),
		synced:     informer.Informer().HasSynced,
	}

	informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.add,
		UpdateFunc: c.update,
		DeleteFunc: c.delete,
	})

	return c
}

// Controller manages virtual machines.
type Controller struct {
	config Configuration

	client     vmclient.VirtualMachinesGetter
	kubeClient kubeclientset.Interface
	gceClient  GCEClient

	lister vmlisters.VirtualMachineLister
	queue  workqueue.RateLimitingInterface
	synced cache.InformerSynced

	logger *logrus.Entry
}

func (c *Controller) add(obj interface{}) {
	vm := obj.(*vmapi.VirtualMachine)
	c.logger.Debugf("enqueueing added vm %s/%s", vm.GetNamespace(), vm.GetName())
	c.enqueue(vm)
}

func (c *Controller) update(old, obj interface{}) {
	vm := obj.(*vmapi.VirtualMachine)
	c.logger.Debugf("enqueueing updated vm %s/%s", vm.GetNamespace(), vm.GetName())
	c.enqueue(vm)
}

func (c *Controller) delete(obj interface{}) {
	vm, ok := obj.(*vmapi.VirtualMachine)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		vm, ok = tombstone.Obj.(*vmapi.VirtualMachine)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not an Object %#v", obj))
			return
		}
	}
	c.logger.Debugf("enqueueing deleted vm %s/%s", vm.GetNamespace(), vm.GetName())
	c.enqueue(vm)
}

// Run runs c; will not return until stopCh is closed. workers determines how
// many clusters will be handled in parallel.
func (c *Controller) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	c.logger.Infof("starting %s controller", controllerName)
	defer c.logger.Infof("shutting down %s controller", controllerName)

	c.logger.Infof("Waiting for caches to reconcile for %s controller", controllerName)
	if !cache.WaitForCacheSync(stopCh, c.synced) {
		utilruntime.HandleError(fmt.Errorf("unable to reconcile caches for %s controller", controllerName))
	}
	c.logger.Infof("Caches are synced for %s controller", controllerName)

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *Controller) enqueue(vm metav1.Object) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(vm)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", vm, err))
		return
	}

	c.queue.Add(key)
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (c *Controller) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) processNextWorkItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.reconcile(key.(string))
	c.handleErr(err, key)

	return true
}

func (c *Controller) handleErr(err error, key interface{}) {
	if err == nil {
		c.queue.Forget(key)
		return
	}

	logger := c.logger.WithField("virtual-machine", key)

	logger.Errorf("error syncing virtual-machine: %v", err)
	if c.queue.NumRequeues(key) < maxRetries {
		logger.Errorf("retrying virtual-machine")
		c.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	logger.Infof("dropping virtual-machine out of the queue: %v", err)
	c.queue.Forget(key)
}

// reconcile handles the business logic of ensuring that virtual machine
// state is driven to the target defined in the spec
func (c *Controller) reconcile(key string) error {
	logger := c.logger.WithField("virtual-machine", key)
	logger.Infof("reconciling virtual machine")
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	vm, err := c.client.VirtualMachines(namespace).Get(name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		logger.Info("not doing work for virtual machine because it has been deleted")
		return nil
	}
	if err != nil {
		logger.Errorf("unable to retrieve virtual machine from store: %v", err)
		return err
	}

	if !vm.ObjectMeta.DeletionTimestamp.IsZero() {
		// no-op if finalizer has been removed.
		if !sets.NewString(vm.ObjectMeta.Finalizers...).Has(vmapi.VirtualMachineFinalizer) {
			logger.Info("reconciling virtual machine causes a no-op as there is no finalizer")
			return nil
		}
		logger.Info("reconciling virtual machine causes deletion")
		if err := c.deleteVM(vm); err != nil {
			logger.Errorf("error deleting virtual machine: %v", err)
			return err
		}

		logger.Info("virtual machine deletion successful, removing finalizer")
		finalizers := sets.NewString(vm.ObjectMeta.Finalizers...)
		finalizers.Delete(vmapi.VirtualMachineFinalizer)
		vm.ObjectMeta.Finalizers = finalizers.List()
		if _, err := c.client.VirtualMachines(namespace).Update(vm); err != nil {
			logger.Errorf("error removing finalizer: %v", err)
			return err
		}
		return nil
	}

	logger.Info("reconciling virtual machine causes creation")
	return c.ensureVM(vm)
}
