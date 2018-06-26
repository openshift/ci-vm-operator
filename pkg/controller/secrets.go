package controller

import (
	"bytes"
	"crypto/rsa"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"

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
