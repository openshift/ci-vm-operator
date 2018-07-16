package admission_controller

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"net/http"
	"crypto/tls"
	"bytes"

	"github.com/sirupsen/logrus"
	"github.com/mattbaird/jsonpatch"

	admissionapi "k8s.io/api/admission/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/api/equality"

	vmapi "github.com/openshift/ci-vm-operator/pkg/apis/virtualmachines/v1alpha1"
)

type Configuration struct {
	CertFile string
	KeyFile  string
	LogLevel string
}

func (c *Configuration) AddFlags() {
	flag.StringVar(&c.CertFile, "tls-cert-file", c.CertFile, "File containing the default x509 Certificate for HTTPS. (CA cert, if any, concatenated after server cert).")
	flag.StringVar(&c.KeyFile, "tls-private-key-file", c.KeyFile, "File containing the default x509 private key matching --tls-cert-file.")
	flag.StringVar(&c.LogLevel, "log-level", logrus.DebugLevel.String(), "Logging level.")
}

func (c *Configuration) Run() error {
	logrus.Info("starting VirtualMachines admission controller")

	level, err := logrus.ParseLevel(c.LogLevel)
	if err != nil {
		logrus.WithError(err).Fatal("failed to parse log level")
	}
	logrus.SetLevel(level)

	sCert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	if err != nil {
		logrus.WithError(err).Fatal("failed to load x509 key pair")
	}

	http.HandleFunc("/validate", handle(validate))
	http.HandleFunc("/mutate", handle(mutate))
	server := &http.Server{
		Addr: ":8443",
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{sCert},
		},
	}
	return server.ListenAndServeTLS("", "")
}

func handle(worker func(admissionapi.AdmissionReview) (*admissionapi.AdmissionResponse)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		logrus.Info("handling admission review for VirtualMachine")
		var body []byte
		if r.Body != nil {
			if data, err := ioutil.ReadAll(r.Body); err == nil {
				body = data
			}
		}

		// verify the content type is accurate
		contentType := r.Header.Get("Content-Type")
		if contentType != "application/json" {
			logrus.Errorf("contentType=%s, expect application/json", contentType)
			return
		}

		var reviewResponse *admissionapi.AdmissionResponse
		ar := admissionapi.AdmissionReview{}
		deserializer := codecs.UniversalDeserializer()
		if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
			logrus.WithError(err).Error("failed to decode admission request body")
			reviewResponse = errResponse(err)
		} else {
			reviewResponse = worker(ar)
		}

		response := admissionapi.AdmissionReview{}
		if reviewResponse != nil {
			response.Response = reviewResponse
			response.Response.UID = ar.Request.UID
		}
		// reset the Object and OldObject, they are not needed in a response.
		ar.Request.Object = runtime.RawExtension{}
		ar.Request.OldObject = runtime.RawExtension{}

		resp, err := json.Marshal(response)
		if err != nil {
			logrus.WithError(err).Error("failed to marshal admission response")
		}
		if _, err := w.Write(resp); err != nil {
			logrus.WithError(err).Error("failed to send admission response")
		}
	}
}

func validate(ar admissionapi.AdmissionReview) (*admissionapi.AdmissionResponse) {
	logger := newLogger(ar)
	logger.Info("validating VirtualMachine to ensure only Status is updated")
	// we know we are configured for the VirtualMachine CRD only
	// and for the UPDATE operation only, so we need to check simply
	// that the UPDATE is to the /status subresource or that spec
	// was unchanged in the UPDATE and allow only those requests
	valid := true
	if ar.Request.SubResource != "status" {
		newVm, err := deserialize(ar.Request.Object.Raw)
		if err != nil {
			return err
		}
		oldVm, err := deserialize(ar.Request.OldObject.Raw)
		if err != nil {
			return err
		}
		if !equality.Semantic.DeepEqual(oldVm.Spec, newVm.Spec) {
			valid = false
		}
	}
	prefix := ""
	if !valid {
		prefix = "in"
	}
	logger.Infof("VirtualMachine was %svalid", prefix)
	return &admissionapi.AdmissionResponse{
		Allowed: valid,
		Result: &meta.Status{// ignored when Allowed is true
			Reason: meta.StatusReasonForbidden,
			Message: "Updates to spec are forbidden for VirtualMachines",
		},
	}
}

func mutate(ar admissionapi.AdmissionReview) (*admissionapi.AdmissionResponse) {
	logger := newLogger(ar)
	logger.Info("mutating VitualMachine to ensure finalizer is present")
	vm, response := deserialize(ar.Request.Object.Raw)
	if response != nil {
		return response
	}

	finalizers := sets.NewString(vm.ObjectMeta.Finalizers...)
	finalizers.Insert(vmapi.VirtualMachineFinalizer)
	updated := vm.DeepCopy()
	updated.ObjectMeta.Finalizers = finalizers.List()
	rawUpdated, response := serialize(*updated)
	if response != nil {
		return response
	}

	patch, err := jsonpatch.CreatePatch(ar.Request.Object.Raw, rawUpdated)
	if err != nil {
		logger.WithError(err).Error("failed to generate patch to ensure VirtualMachine finalizer")
		return errResponse(err)
	}
	rawPatch := bytes.Buffer{}
	if err := json.NewEncoder(&rawPatch).Encode(patch); err != nil {
		logger.WithError(err).Error("failed to encode patch to ensure VirtualMachine finalizer")
		return errResponse(err)
	}

	pt := admissionapi.PatchTypeJSONPatch
	return &admissionapi.AdmissionResponse{
		Allowed:   true,
		PatchType: &pt,
		Patch:     rawPatch.Bytes(),
	}
}

func deserialize(raw []byte) (vmapi.VirtualMachine, *admissionapi.AdmissionResponse) {
	vm := vmapi.VirtualMachine{}
	if _, _, err := codecs.UniversalDeserializer().Decode(raw, nil, &vm); err != nil {
		logrus.WithError(err).Error("failed to decode VirtualMachine in admission request body")
		return vm, errResponse(err)
	}
	return vm, nil
}

func serialize(vm vmapi.VirtualMachine) ([]byte, *admissionapi.AdmissionResponse) {
	raw := bytes.Buffer{}
	if err := codecs.LegacyCodec(vmapi.SchemeGroupVersion).Encode(&vm, &raw); err != nil {
		logrus.WithError(err).Error("failed to encode VirtualMachine into JSON")
		return raw.Bytes(), errResponse(err)
	}
	return raw.Bytes(), nil
}

func errResponse(err error) *admissionapi.AdmissionResponse {
	return &admissionapi.AdmissionResponse{
		Result: &meta.Status{
			Message: err.Error(),
		},
	}
}

func newLogger(review admissionapi.AdmissionReview) *logrus.Entry {
	logger := logrus.New()
	logger.Formatter = &logrus.JSONFormatter{}
	return logger.WithFields(logrus.Fields{
		"resource":    review.Request.Resource,
		"subresource": review.Request.SubResource,
		"name":        review.Request.Name,
		"namespace":   review.Request.Namespace,
		"operation":   review.Request.Operation,
	})
}
