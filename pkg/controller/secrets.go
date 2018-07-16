package controller

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"time"

	"github.com/sirupsen/logrus"

	"golang.org/x/crypto/ssh"
)

// createSSHSecretForVM will create a SSH keypair for the VM
func newSSHKeypair() (string, string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return "", "", fmt.Errorf("failed to create SSH private key: %v", err)
	}

	privateKeyData := bytes.Buffer{}
	privateKeyPEM := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}
	if err := pem.Encode(&privateKeyData, privateKeyPEM); err != nil {
		return "", "", fmt.Errorf("failed to encode SSH private key: %v", err)
	}

	pub, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to create SSH public key: %v", err)
	}
	publicKeyData := bytes.Buffer{}
	if _, err := publicKeyData.Write(ssh.MarshalAuthorizedKey(pub)); err != nil {
		return "", "", fmt.Errorf("failed to encode SSH public key: %v", err)
	}

	return privateKeyData.String(), publicKeyData.String(), nil
}

func pollForSSHConnection(sshConfig SSHConnectionConfig, instanceHostname, pem string, logger *logrus.Entry) error {
	signer, err := ssh.ParsePrivateKey([]byte(pem))
	if err != nil {
		logger.WithError(err).Error("failed to parse private key")
		return fmt.Errorf("failed to parse private key: %v", err)
	}
	config := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: fixed host key
		Timeout:         time.Duration(sshConfig.TimeoutSeconds) * time.Second,
	}
	succeeded := false
	logger = logger.WithField("hostname", instanceHostname)
	for i := 0; i < sshConfig.Retries; i ++ {
		iLogger := logger.WithField("attempt", i + 1)
		iLogger.Debug("dialing host")
		conn, dialErr := net.DialTimeout("tcp", instanceHostname, config.Timeout)
		if dialErr == nil {
			iLogger.Debug("dial success")
			c, _, _, connErr := ssh.NewClientConn(conn, instanceHostname, config)
			if connErr == nil {
				iLogger.Debug("SSH connection success")
				if closeErr := c.Close(); closeErr != nil {
					iLogger.WithError(closeErr).Warning("failed to close SSH connection")
				}
				succeeded = true
			} else {
				iLogger.WithError(connErr).Warning("SSH connection failure")
			}
			break
		} else {
			iLogger.WithError(dialErr).Debug("dial failure")
		}
		time.Sleep(time.Duration(sshConfig.DelaySeconds) * time.Second)
	}
	if !succeeded {
		return fmt.Errorf("could not connect to VM over SSH in %d attempts", sshConfig.Retries)
	}
	return nil
}
