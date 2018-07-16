package controller

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"golang.org/x/net/context"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"

	coreapi "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmapi "github.com/openshift/ci-vm-operator/pkg/apis/virtualmachines/v1alpha1"
)

const (
	gceTimeout   = time.Minute * 10
	gceWaitSleep = time.Second * 5
)

type GCEClient interface {
	InstancesDelete(project string, zone string, targetInstance string) (*compute.Operation, error)
	InstancesGet(project string, zone string, instance string) (*compute.Instance, error)
	InstancesInsert(project string, zone string, instance *compute.Instance) (*compute.Operation, error)
	ZoneOperationsGet(project string, zone string, operation string) (*compute.Operation, error)
}

func NewGCEClient() (GCEClient, error) {
	// The default GCP client expects the environment variable
	// GOOGLE_APPLICATION_CREDENTIALS to point to a file with service credentials.
	client, err := google.DefaultClient(context.TODO(), compute.ComputeScope)
	if err != nil {
		return nil, err
	}
	service, err := compute.New(client)
	if err != nil {
		return nil, err
	}
	return &gceClient{c: service}, nil
}

type gceClient struct {
	c *compute.Service
}

func (c *gceClient) InstancesDelete(project string, zone string, targetInstance string) (*compute.Operation, error) {
	return c.c.Instances.Delete(project, zone, targetInstance).Do()
}

func (c *gceClient) InstancesGet(project string, zone string, instance string) (*compute.Instance, error) {
	return c.c.Instances.Get(project, zone, instance).Do()
}

func (c *gceClient) InstancesInsert(project string, zone string, instance *compute.Instance) (*compute.Operation, error) {
	return c.c.Instances.Insert(project, zone, instance).Do()
}

func (c *gceClient) ZoneOperationsGet(project string, zone string, operation string) (*compute.Operation, error) {
	return c.c.ZoneOperations.Get(project, zone, operation).Do()
}

func (c *Controller) deleteVM(vm *vmapi.VirtualMachine) error {
	logger := c.logger.WithFields(logrus.Fields{
		"virtual-machine": vm.Name,
		"namespace":       vm.Namespace,
	})

	instance, err := c.gceClient.InstancesGet(c.config.Project, string(c.config.Zone), vm.ObjectMeta.Name)
	if err != nil {
		if gerr, ok := err.(*googleapi.Error); ok && gerr.Code == http.StatusNotFound {
			logger.Infof("Skipped deleting a VM that is already deleted.")
			return nil
		}
		return fmt.Errorf("failed to check for existance of virtual machine: %v", err)
	}

	if instance == nil {
		logger.Infof("Skipped deleting a VM that is already deleted.")
		return nil
	}

	logger.Info("deleting GCE VM")
	op, err := c.gceClient.InstancesDelete(c.config.Project, string(c.config.Zone), vm.ObjectMeta.Name)
	if err == nil {
		err = c.waitForOperation(op, logger)
	}
	if err != nil {
		logger.WithError(err).Info("failed to delete GCE VM")
		return c.handleError(vm, fmt.Errorf("error deleting GCE instance: %v", err))
	}

	return err
}

// ensureVM ensures a VM in GCE exists to fulfill the request. A machine-local SSH
// key is generated to access the root account for the machine. See:
// https://cloud.google.com/compute/docs/instances/adding-removing-ssh-keys
func (c *Controller) ensureVM(vm *vmapi.VirtualMachine) error {
	logger := c.logger.WithFields(logrus.Fields{
		"virtual-machine": vm.Name,
		"namespace":       vm.Namespace,
	})

	instance, err := c.gceClient.InstancesGet(c.config.Project, string(c.config.Zone), vm.ObjectMeta.Name)
	if instance != nil {
		logger.Infof("Skipped creating a VM that is already created.")
		return nil
	}
	if err != nil {
		if gerr, ok := err.(*googleapi.Error); ok && gerr.Code != http.StatusNotFound {
			return fmt.Errorf("failed to check for existance of virtual machine: %v", err)
		}
	}

	logger.Info("creating SSH keypair")
	pem, pub, err := newSSHKeypair()
	if err != nil {
		return fmt.Errorf("coult not create SSH keypair for VM: %v", err)
	}
	formattedPub := fmt.Sprintf("root:%s root", strings.TrimSuffix(pub, "\n"))

	disks := []*compute.AttachedDisk{
		{
			AutoDelete: true,
			Boot:       true,
			InitializeParams: &compute.AttachedDiskInitializeParams{
				SourceImage: vm.Spec.BootDisk.ImageFamily,
				DiskSizeGb:  vm.Spec.BootDisk.SizeGB,
				DiskType:    fmt.Sprintf("projects/%s/zones/%s/diskTypes/%s", c.config.Project, c.config.Zone, vm.Spec.BootDisk.Type),
			},
		},
	}
	for _, disk := range vm.Spec.Disks {
		disks = append(disks, &compute.AttachedDisk{
			AutoDelete: true,
			InitializeParams: &compute.AttachedDiskInitializeParams{
				DiskSizeGb: disk.SizeGB,
				DiskType:   string(disk.Type),
			},
		})
	}
	logger.Info("creating GCE VM")
	op, err := c.gceClient.InstancesInsert(c.config.Project, string(c.config.Zone), &compute.Instance{
		Name:        vm.ObjectMeta.Name,
		MachineType: fmt.Sprintf("zones/%s/machineTypes/%s", c.config.Zone, vm.Spec.MachineType),
		Metadata: &compute.Metadata{
			Items: []*compute.MetadataItems{{
				Key:   "ssh-keys",
				Value: &formattedPub,
			}},
		},
		CanIpForward: true,
		NetworkInterfaces: []*compute.NetworkInterface{
			{
				Network: "global/networks/default",
				AccessConfigs: []*compute.AccessConfig{
					{
						Type: "ONE_TO_ONE_NAT",
						Name: "External NAT",
					},
				},
			},
		},
		Disks: disks,
	})

	if err == nil {
		err = c.waitForOperation(op, logger)
	}

	if err != nil {
		logger.WithError(err).Error("failed to create GCE VM")
		return c.handleError(vm, fmt.Errorf("error creating GCE instance: %v", err))
	}

	instance, err = c.gceClient.InstancesGet(c.config.Project, string(c.config.Zone), vm.ObjectMeta.Name)
	if err != nil {
		logger.WithError(err).Error("failed to locate newly created GCE VM")
		return fmt.Errorf("failed to check for existance of virtual machine: %v", err)
	}

	logger.Info("uploading SSH keypair to cluster")
	if _, err := c.kubeClient.CoreV1().Secrets(vm.Namespace).Create(&coreapi.Secret{
		ObjectMeta: meta.ObjectMeta{
			Name:      vm.Name,
			Namespace: vm.Namespace,
			// we do not want this controller to be able to delete
			// secrets across the cluster, so we use owner refs to
			// allow for garbage collection instead
			OwnerReferences: []meta.OwnerReference{{
				APIVersion: "v1alpha1",
				Kind:       "virtualmachine.ci.openshift.io",
				Name:       vm.Name,
				UID:        vm.UID,
			}},
		},
		StringData: map[string]string{
			"id_rsa":     pem,
			"id_rsa.pub": pub,
			"ssh_config": fmt.Sprintf(`Host %s
  HostName %s
  Port 22
  StrictHostKeyChecking no
`, instance.Name, instance.NetworkInterfaces[0].AccessConfigs[0].NatIP),
		},
	}); err != nil {
		logger.WithError(err).Error("error creating SSH secret")
		return fmt.Errorf("could not create SSH secret: %v", err)
	}

	return nil
}

func (c *Controller) waitForOperation(op *compute.Operation, logger *logrus.Entry) error {
	logger.Infof("Waiting for %v %q...", op.OperationType, op.Name)
	defer logger.Infof("Finished waiting for %v %q...", op.OperationType, op.Name)

	start := time.Now()
	ctx, cf := context.WithTimeout(context.Background(), gceTimeout)
	defer cf()

	var err error
	for {
		if err = c.checkOp(op, err); err != nil || op.Status == "DONE" {
			return err
		}
		logger.Infof("Waiting for %v %q: %v (%d%%): %v", op.OperationType, op.Name, op.Status, op.Progress, op.StatusMessage)
		select {
		case <-ctx.Done():
			return fmt.Errorf("gce operation %v %q timed out after %v", op.OperationType, op.Name, time.Since(start))
		case <-time.After(gceWaitSleep):
		}
		op, err = c.gceClient.ZoneOperationsGet(c.config.Project, path.Base(op.Zone), op.Name)
	}
}

func (c *Controller) checkOp(op *compute.Operation, err error) error {
	if err != nil || op.Error == nil || len(op.Error.Errors) == 0 {
		return err
	}

	var errs bytes.Buffer
	for _, v := range op.Error.Errors {
		errs.WriteString(v.Message)
		errs.WriteByte('\n')
	}
	return errors.New(errs.String())
}

func (c *Controller) handleError(vm *vmapi.VirtualMachine, err error) error {
	updated := vm.DeepCopy()
	updated.Status.State.ProcessingPhase = vmapi.ProcessingPhaseError
	updated.Status.State.Message = err.Error()
	_, updateErr := c.client.VirtualMachines(vm.Namespace).UpdateStatus(updated)
	return updateErr
}
