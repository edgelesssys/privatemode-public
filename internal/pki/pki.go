// package pki handles the creation and management of public key infrastructure (PKI) for Continuum, more specifically for etcd.
package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"path/filepath"
	"slices"
	"time"

	ccrypto "github.com/edgelesssys/continuum/internal/crypto"
	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/spf13/afero"
)

const (
	isCA  = true
	notCA = false
)

// PKI holds CA information and is used to create new certificates for etcd clients and servers.
type PKI struct {
	caCert *x509.Certificate
	caKey  *ecdsa.PrivateKey

	log *slog.Logger
}

// New sets up a new PKI with the given CA certificate and key.
// If no CA certificate and key are provided, a new CA key pair will be created.
func New(caCert *x509.Certificate, caKey *ecdsa.PrivateKey, fs afero.Afero, log *slog.Logger) (*PKI, error) {
	p := &PKI{
		caCert: caCert,
		caKey:  caKey,
		log:    log,
	}

	// If no CA certificate and key are provided, we set up a new CA key pair
	if p.caCert == nil && p.caKey == nil {
		var certExist, keyExist bool
		if _, err := fs.Stat(filepath.Join(constants.EtcdPKIPath(), "ca.crt")); err == nil {
			certExist = true
		}
		if _, err := fs.Stat(filepath.Join(constants.EtcdPKIPath(), "ca.key")); err == nil {
			keyExist = true
		}

		// Both the CA certificate and key exist on disk
		// -> load them from disk and return
		if certExist && keyExist {
			log.Info("Reusing existing CA key pair")

			caCertPEM, err := fs.ReadFile(filepath.Join(constants.EtcdPKIPath(), "ca.crt"))
			if err != nil {
				return nil, fmt.Errorf("reading CA certificate: %w", err)
			}
			caKeyPEM, err := fs.ReadFile(filepath.Join(constants.EtcdPKIPath(), "ca.key"))
			if err != nil {
				return nil, fmt.Errorf("reading CA key: %w", err)
			}

			// Decode cert and key from PEM
			caCert, err := ccrypto.ParseCertificateFromPEM(caCertPEM)
			if err != nil {
				return nil, fmt.Errorf("parsing CA certificate: %w", err)
			}
			caKey, err := ccrypto.ParseECPrivateKeyFromPEM(caKeyPEM)
			if err != nil {
				return nil, fmt.Errorf("parsing CA key: %w", err)
			}

			// Return early
			// We don't need to save cert and key to disk again
			p.caCert = caCert
			p.caKey = caKey
			return p, nil
		}

		// Log a warning if only one of the CA certificate and key exists on disk
		if certExist || keyExist {
			log.Warn("Incomplete CA key pair found. Creating new key pair.", "certificateExists", certExist, "keyExists", keyExist)
		} else {
			log.Info("Creating new CA key pair")
		}

		caCommonName := "continuum-etcd-root-ca"
		validity := time.Duration(math.MaxInt64) // CA does not expire
		caCert, caKey, err := p.generateCert(caCommonName, nil, nil, validity, isCA)
		if err != nil {
			return nil, fmt.Errorf("creating root CA: %w", err)
		}

		p.caCert = caCert
		p.caKey = caKey
	} else if p.caCert == nil || p.caKey == nil {
		return nil, errors.New("either both CA certificate and key must be provided, or neither of them")
	}

	// Save the CA certificate and key to disk
	if err := fs.MkdirAll(constants.EtcdPKIPath(), 0o700); err != nil {
		return nil, fmt.Errorf("creating PKI directory: %w", err)
	}
	caCertPEM, caKeyPEM, err := encodeKeyPairToPEM(p.caCert, p.caKey)
	if err != nil {
		return nil, fmt.Errorf("encoding root CA: %w", err)
	}
	if err := fs.WriteFile(filepath.Join(constants.EtcdPKIPath(), "ca.crt"), caCertPEM, 0o600); err != nil {
		return nil, fmt.Errorf("writing root CA certificate: %w", err)
	}
	if err := fs.WriteFile(filepath.Join(constants.EtcdPKIPath(), "ca.key"), caKeyPEM, 0o600); err != nil {
		return nil, fmt.Errorf("writing root CA key: %w", err)
	}

	return p, nil
}

// CACertificate returns the CA certificate of the PKI.
func (p *PKI) CACertificate() []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: p.caCert.Raw,
	})
}

// CreateCertificate creates a new certificate and key signed by the PKI's CA.
// The certificate is signed for the given common name, subject alternative names (SANs), and IP addresses,
// and will be valid for the given duration.
func (p *PKI) CreateCertificate(
	commonName string, sans []string, ips []net.IP, validity time.Duration,
) (certPEM []byte, keyPEM []byte, err error) {
	sans = slices.DeleteFunc(sans, func(s string) bool { return s == "" })  // Remove empty strings
	ips = slices.DeleteFunc(ips, func(ip net.IP) bool { return ip == nil }) // Remove nil entries

	cert, key, err := p.generateCert(commonName, sans, ips, validity, notCA)
	if err != nil {
		return nil, nil, fmt.Errorf("creating certificate: %w", err)
	}
	certPEM, keyPEM, err = encodeKeyPairToPEM(cert, key)
	if err != nil {
		return nil, nil, fmt.Errorf("encoding certificate to PEM: %w", err)
	}

	return certPEM, keyPEM, nil
}

func (p *PKI) generateCert(
	commonName string, subjAltNames []string, ips []net.IP, validity time.Duration, isCA bool,
) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	parentCertificate := p.caCert
	parentPrivateKey := p.caKey

	// Generate private key
	privk, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generating private key: %w", err)
	}

	// Certificate parameters
	notBefore := time.Now().Add(-5 * time.Minute) // Account for potential clock skew
	notAfter := notBefore.Add(validity)

	serialNumber, err := ccrypto.GenerateCertificateSerialNumber()
	if err != nil {
		return nil, nil, fmt.Errorf("generating serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: commonName,
		},
		DNSNames:    subjAltNames,
		IPAddresses: ips,
		NotBefore:   notBefore,
		NotAfter:    notAfter,

		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  isCA,
	}

	if parentCertificate == nil {
		parentCertificate = &template
		parentPrivateKey = privk
	}
	certRaw, err := x509.CreateCertificate(rand.Reader, &template, parentCertificate, &privk.PublicKey, parentPrivateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("creating certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(certRaw)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing certificate: %w", err)
	}

	return cert, privk, nil
}

// encodeKeyPairToPEM encodes a certificate and private key to PEM format.
func encodeKeyPairToPEM(cert *x509.Certificate, key *ecdsa.PrivateKey) (certPEM []byte, keyPEM []byte, err error) {
	certPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	})
	privkBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling private key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: privkBytes,
	})
	return certPEM, keyPEM, nil
}
