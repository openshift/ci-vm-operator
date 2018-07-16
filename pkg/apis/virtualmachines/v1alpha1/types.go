package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	VirtualMachineFinalizer = "virtualmachines.ci.openshift.io"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VirtualMachine is a specification for a virtual machine
type VirtualMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VirtualMachineSpec   `json:"spec"`
	Status VirtualMachineStatus `json:"status"`
}

// VirtualMachineType identifies a GCP machine type
type VirtualMachineType string

const (
	VirtualMachineTypeStandard1  VirtualMachineType = "n1-standard-1"
	VirtualMachineTypeStandard2                     = "n1-standard-2"
	VirtualMachineTypeStandard4                     = "n1-standard-4"
	VirtualMachineTypeStandard8                     = "n1-standard-8"
	VirtualMachineTypeStandard16                    = "n1-standard-16"
	VirtualMachineTypeStandard32                    = "n1-standard-32"
	VirtualMachineTypeStandard64                    = "n1-standard-64"
	VirtualMachineTypeStandard96                    = "n1-standard-96"
)

// VirtualMachineSpec is the spec for a VirtualMachine resource
type VirtualMachineSpec struct {
	// MachineType is the machine size to provision. We support only
	// standard types for now. See:
	// https://cloud.google.com/compute/docs/machine-types
	MachineType VirtualMachineType `json:"machineType"`
	// BootDisk is the disk we boot from
	BootDisk VirtualMachineBootDiskSpec `json:"bootDisk"`
	// Disks are additional disks to attach to the virtual machine
	Disks []VirtualMachineDiskSpec `json:"disks,omitempty"`
}

type VirtualMachineBootDiskSpec struct {
	// ImageFamily is the full or partial path to the image family
	// to use for the boot disk. See:
	// https://cloud.google.com/compute/docs/reference/rest/v1/images/getFromFamily
	ImageFamily string `json:"imageFamily"`

	VirtualMachineDiskSpec `json:",inline"`
}

// VirtualMachineDiskType identifies a GCP disk type
type VirtualMachineDiskType string

const (
	VirtualMachineDiskTypePersistentStandard VirtualMachineDiskType = "pd-standard"
	VirtualMachineDiskTypePersistentSSD                             = "pd-ssd"
	VirtualMachineDiskTypeLocalSSD                                  = "local-ssd"
)

// VirtualMachineDiskSpec contains initialization parameters for a disk
type VirtualMachineDiskSpec struct {
	// SizeGB is the size of the disk in GB
	SizeGB int64 `json:"sizeGb"`
	// Type is the disk type to use
	Type VirtualMachineDiskType `json:"type"`
}

// VirtualMachineStatus is the status for a VirtualMachine resource
type VirtualMachineStatus struct {
	State     ProcessingState        `json:"state"`
	SelfLink  string                 `json:"selfLink"`
	SecretRef corev1.ObjectReference `json:"secretRef"`
}

type ProcessingPhase string

const (
	ProcessingPhasePending      = "pending"
	ProcessingPhaseProvisioning = "provisioning"
	ProcessingPhaseProvisioned  = "provisioned"
	ProcessingPhaseError        = "error"
)

type ProcessingState struct {
	ProcessingPhase ProcessingPhase `json:"processingPhase"`
	Message         string          `json:"message"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VirtualMachineList is a list of VirtualMachine resources
type VirtualMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []VirtualMachine `json:"items"`
}
