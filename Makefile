build:
	go build ./cmd/...
.PHONY: build

test:
	go test ./...
.PHONY: test

deploy: set-namespace deploy-vm-crd deploy-admission-controller deploy-ci-vm-operator
.PHONY: deploy

set-namespace:
	if [[ "$(shell oc project -q )" != "ci" ]]; then oc new-project ci; fi
.PHONY: set-namespace

deploy-vm-crd:
	oc apply -f deploy/crd.yaml
.PHONY: deploy-vm-crd

deploy-admission-controller: deploy-admission-controller-build deploy-admission-controller-infra register-admission-controller
.PHONY: deploy-admission-controller

deploy-admission-controller-build:
	oc apply -f deploy/admission-controller-build.yaml
.PHONY: deploy-admission-controller-build

deploy-admission-controller-infra:
	oc apply -f deploy/admission-controller.yaml
.PHONY: deploy-admission-controller-infra

register-admission-controller:
	# TODO: use caBundle from configmap after https://github.com/openshift/service-serving-cert-signer/pull/9
	while ! oc get secret virtual-machine-admission-control-webhook-certificates >/dev/null 2>&1 ; do sleep 0.2; done
	oc apply -f deploy/admission-controller-registration.yaml -o json --dry-run | jq ".webhooks[0].clientConfig.caBundle = \"$(shell oc get secret virtual-machine-admission-control-webhook-certificates -o "jsonpath={.data.tls\.crt}" )\"" | oc apply -f -
.PHONY: register-admission-controller

deploy-ci-vm-operator: deploy-ci-vm-operator-build deploy-ci-vm-operator-credentials deploy-ci-vm-operator-infra
.PHONY: deploy-ci-vm-operator

deploy-ci-vm-operator-build:
	oc apply -f deploy/controller-build.yaml
.PHONY: deploy-ci-vm-operator-build

deploy-ci-vm-operator-infra:
	oc apply -f deploy/controller.yaml
	oc apply -f deploy/controller-rbac.yaml
.PHONY: deploy-ci-vm-operator-infra

deploy-ci-vm-operator-credentials:
	# CI_VM_OPERATOR_GCE_CREDENTIALS_JSON is the serviceaccount credentials in JSON form for GCE
	oc create secret generic virtual-machine-operator-credentials --from-file gce.json="${CI_VM_OPERATOR_GCE_CREDENTIALS_JSON}" -o yaml --dry-run | oc apply -f -
.PHONY: deploy-ci-vm-operator-credentials