package cmd

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

// certificateLegacy acts as a handler for generating signed certificates
type certificateLegacy struct {
	serverPrivKey *rsa.PrivateKey
	serverCert    *pem.Block
	caPrivKey     *rsa.PrivateKey
	caCert        *pem.Block
}

// newCertificateLegacy creates a certificate handler for kubernetes versions <=18
func newCertificateLegacy() (*certificateLegacy, error) {
	crt := &certificateLegacy{}

	caCert := &x509.Certificate{
		SerialNumber: big.NewInt(2021),
		Subject: pkix.Name{
			Organization: []string{"edgeless.systems"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(1, 0, 0),
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caPrivKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, err
	}
	crt.caPrivKey = caPrivKey

	caPub := &caPrivKey.PublicKey
	caBytes, err := x509.CreateCertificate(rand.Reader, caCert, caCert, caPub, caPrivKey)
	if err != nil {
		return nil, err
	}

	crt.caCert = &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caBytes,
	}

	serverPrivKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, fmt.Errorf("failed creating rsa private key: %v", err)
	}
	crt.serverPrivKey = serverPrivKey

	return crt, nil
}

// get returns the signed certificate of the webhook server
func (crt *certificateLegacy) get() ([]byte, error) {
	certBytes := pem.EncodeToMemory(crt.serverCert)
	return certBytes, nil
}

// setCaBundle sets the CABundle field to the self signed rootCA generated by the handler
func (crt *certificateLegacy) setCaBundle() ([]string, error) {
	caCertBytes := pem.EncodeToMemory(crt.caCert)
	injectorValues := []string{
		fmt.Sprintf("marbleInjector.start=%v", true),
		fmt.Sprintf("marbleInjector.CABundle=%s", base64.StdEncoding.EncodeToString(caCertBytes)),
	}
	return injectorValues, nil
}

// signRequest signs the webhook certificate using the rootCA
func (crt *certificateLegacy) signRequest() error {
	serverCert := &x509.Certificate{
		SerialNumber: big.NewInt(2022),
		Subject: pkix.Name{
			CommonName:   "system:node:marble-injector.marblerun.svc",
			Organization: []string{"system:nodes"},
		},
		DNSNames:           []string{"marble-injector.marblerun.svc"},
		SignatureAlgorithm: x509.SHA256WithRSA,
		KeyUsage:           x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:        []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		NotBefore:          time.Now(),
		NotAfter:           time.Now().AddDate(1, 0, 0),
	}

	certData := crt.caCert.Bytes
	caCertx509, err := x509.ParseCertificate(certData)
	if err != nil {
		return err
	}

	serverPub := &crt.serverPrivKey.PublicKey
	serverCertBytes, err := x509.CreateCertificate(rand.Reader, serverCert, caCertx509, serverPub, crt.caPrivKey)
	if err != nil {
		return err
	}

	crt.serverCert = &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: serverCertBytes,
	}

	return nil
}

// getKey returns the private key of the webhook server
func (crt certificateLegacy) getKey() *rsa.PrivateKey {
	return crt.serverPrivKey
}
