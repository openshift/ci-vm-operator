package controller

type GCPZone string

const (
	GCPZoneUSEast1b    GCPZone = "us-east1-b"
	GCPZoneUSEast1c            = "us-east1-c"
	GCPZoneUSEast1d            = "us-east1-d"
	GCPZoneUSEast4c            = "us-east4-c"
	GCPZoneUSEast4b            = "us-east4-b"
	GCPZoneUSEast4a            = "us-east4-a"
	GCPZoneUSCentral1c         = "us-central1-c"
	GCPZoneUSCentral1a         = "us-central1-a"
	GCPZoneUSCentral1f         = "us-central1-f"
	GCPZoneUSCentral1b         = "us-central1-b"
	GCPZoneUSWest1b            = "us-west1-b"
	GCPZoneUSWest1c            = "us-west1-c"
	GCPZoneUSWest1a            = "us-west1-a"
)

// Configuration holds global configuration for launching
// virtual machines in GCE.
type Configuration struct {
	Project string  `json:"project"`
	Zone    GCPZone `json:"zone"`
}
