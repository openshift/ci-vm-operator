package main

import (
	"flag"

	"github.com/sirupsen/logrus"

	controller "github.com/openshift/ci-vm-operator/pkg/admission-controller"
)

func main() {
	logrus.SetFormatter(&logrus.JSONFormatter{})

	config := controller.Configuration{}
	config.AddFlags()
	flag.Parse()
	if err := config.Run(); err != nil {
		logrus.WithError(err).Fatal("failed to run admission controller")
	}
}
