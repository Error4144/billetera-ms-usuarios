// devcert creates a disposable CA and certificates exclusively for local development.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

func main() {
	directory := flag.String("out", ".local-certs", "directorio nuevo de certificados locales")
	flag.Parse()
	if err := generate(*directory); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Certificados locales generados; claves excluidas de Git")
}
func generate(directory string) error {
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		return errors.New("el directorio de certificados ya existe o no es accesible")
	}
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Billetera local development CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(30 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(directory, "ca.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0644); err != nil {
		return err
	}
	for index, identity := range []struct{ name, uri string }{{"server", ""}, {"bff", "spiffe://billetera/bff-graphql"}, {"health", "spiffe://billetera/ms-usuarios-health"}} {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return err
		}
		cert := &x509.Certificate{SerialNumber: big.NewInt(int64(index + 2)), Subject: pkix.Name{CommonName: identity.name}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(7 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
		if identity.name == "server" {
			cert.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
			cert.DNSNames = []string{"localhost", "ms-usuarios"}
			cert.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
		} else {
			parsed, err := url.Parse(identity.uri)
			if err != nil {
				return err
			}
			cert.URIs = []*url.URL{parsed}
			cert.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
		}
		der, err := x509.CreateCertificate(rand.Reader, cert, ca, &key.PublicKey, caKey)
		if err != nil {
			return err
		}
		private, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(directory, identity.name+".crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0644); err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(directory, identity.name+".key"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
			return err
		}
	}
	return nil
}
