package acme

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/iodesystems/ubuntu-router/internal/config"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
)

// User implements registration.User for Lego
type User struct {
	Email        string                 `json:"email"`
	Registration *registration.Resource `json:"registration,omitempty"`
	key          crypto.PrivateKey
}

func (u *User) GetEmail() string                        { return u.Email }
func (u *User) GetRegistration() *registration.Resource { return u.Registration }
func (u *User) GetPrivateKey() crypto.PrivateKey        { return u.key }

// Client wraps the Lego ACME client
type Client struct {
	accountDir string
	staging    bool
}

// NewClient creates a new ACME client
func NewClient(accountDir string, staging bool) *Client {
	return &Client{
		accountDir: accountDir,
		staging:    staging,
	}
}

// LogFunc is a function that receives log lines
type LogFunc func(line string)

// ObtainCertificate requests a certificate for the given domains
func (c *Client) ObtainCertificate(email string, domains []string, providerCfg *config.DNSProviderConfig, logFn LogFunc) (*certificate.Resource, error) {
	if logFn == nil {
		logFn = func(s string) {}
	}

	// Create DNS challenge provider
	dnsProvider, err := CreateChallengeProvider(providerCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create DNS provider: %w", err)
	}

	logFn(fmt.Sprintf("Using DNS provider: %s", ProviderName(providerCfg)))

	// Load or create user
	user, err := c.loadOrCreateUser(email)
	if err != nil {
		return nil, fmt.Errorf("failed to load/create ACME user: %w", err)
	}

	// Configure lego client
	legoConfig := lego.NewConfig(user)
	legoConfig.Certificate.KeyType = certcrypto.RSA2048

	if c.staging {
		legoConfig.CADirURL = lego.LEDirectoryStaging
		logFn("Using Let's Encrypt STAGING environment")
	} else {
		logFn("Using Let's Encrypt PRODUCTION environment")
	}

	client, err := lego.NewClient(legoConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create ACME client: %w", err)
	}

	// Set DNS provider
	if err := client.Challenge.SetDNS01Provider(dnsProvider); err != nil {
		return nil, fmt.Errorf("failed to set DNS provider: %w", err)
	}

	// Register if needed
	if user.Registration == nil {
		logFn("Registering ACME account...")
		reg, err := client.Registration.Register(registration.RegisterOptions{
			TermsOfServiceAgreed: true,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to register: %w", err)
		}
		user.Registration = reg
		if err := c.saveUser(user); err != nil {
			logFn(fmt.Sprintf("Warning: failed to save user registration: %v", err))
		}
	}

	logFn(fmt.Sprintf("Requesting certificate for: %v", domains))

	// Request certificate
	request := certificate.ObtainRequest{
		Domains: domains,
		Bundle:  true,
	}

	certificates, err := client.Certificate.Obtain(request)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain certificate: %w", err)
	}

	logFn("Certificate obtained successfully!")

	return certificates, nil
}

func (c *Client) loadOrCreateUser(email string) (*User, error) {
	if err := os.MkdirAll(c.accountDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create account directory: %w", err)
	}

	accountFile := filepath.Join(c.accountDir, "account.json")
	keyFile := filepath.Join(c.accountDir, "account.key")

	user := &User{Email: email}

	// Try to load existing user
	if _, err := os.Stat(accountFile); err == nil {
		data, err := os.ReadFile(accountFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read account file: %w", err)
		}
		if err := json.Unmarshal(data, user); err != nil {
			return nil, fmt.Errorf("failed to parse account file: %w", err)
		}
	}

	// Try to load existing key
	if _, err := os.Stat(keyFile); err == nil {
		keyPEM, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read key file: %w", err)
		}
		block, _ := pem.Decode(keyPEM)
		if block == nil {
			return nil, fmt.Errorf("failed to decode PEM block from key file")
		}
		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse EC private key: %w", err)
		}
		user.key = key
	} else {
		// Generate new key
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("failed to generate key: %w", err)
		}
		user.key = key

		// Save the key
		keyBytes, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal key: %w", err)
		}
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
		if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
			return nil, fmt.Errorf("failed to save key: %w", err)
		}
	}

	return user, nil
}

// saveUser saves the user registration to disk
func (c *Client) saveUser(user *User) error {
	accountFile := filepath.Join(c.accountDir, "account.json")
	data, err := json.MarshalIndent(user, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal user: %w", err)
	}
	return os.WriteFile(accountFile, data, 0600)
}

// Manager handles Let's Encrypt certificate operations
type Manager struct {
	certDir string
	staging bool
	acme    *Client
}

// NewManager creates a new certificate manager
func NewManager(certDir string, staging bool) *Manager {
	if certDir == "" {
		certDir = "/etc/ubuntu-router/certs"
	}

	accountDir := filepath.Join(certDir, "accounts")
	acmeClient := NewClient(accountDir, staging)

	return &Manager{
		certDir: certDir,
		staging: staging,
		acme:    acmeClient,
	}
}

// CertStatus represents the status of a certificate
type CertStatus struct {
	Domain     string `json:"domain"`
	CertExists bool   `json:"cert_exists"`
	CertPath   string `json:"cert_path"`
	KeyPath    string `json:"key_path"`
	ExpiryInfo string `json:"expiry_info,omitempty"`
}

// GetCertStatus returns the status of a certificate for a domain
func (m *Manager) GetCertStatus(domain string) CertStatus {
	baseDomain := strings.TrimPrefix(domain, "*.")

	status := CertStatus{
		Domain:   domain,
		CertPath: filepath.Join(m.certDir, "live", baseDomain, "fullchain.pem"),
		KeyPath:  filepath.Join(m.certDir, "live", baseDomain, "privkey.pem"),
	}

	if _, err := os.Stat(status.CertPath); err == nil {
		if _, err := os.Stat(status.KeyPath); err == nil {
			status.CertExists = true
		}
	}

	// Get expiry info
	if status.CertExists {
		cmd := exec.Command("openssl", "x509", "-enddate", "-noout", "-in", status.CertPath)
		if out, err := cmd.Output(); err == nil {
			status.ExpiryInfo = strings.TrimSpace(strings.TrimPrefix(string(out), "notAfter="))
		}
	}

	return status
}

// RequestCertificate requests a certificate for a zone
func (m *Manager) RequestCertificate(zone *config.ExternalDNSZone, logFn LogFunc) error {
	if zone.SSLEmail == "" {
		return fmt.Errorf("no email specified for zone %s", zone.Name)
	}

	// Build domain list
	domains := []string{"*." + zone.Name, zone.Name}
	if len(zone.SSLDomains) > 0 {
		domains = append(domains, zone.SSLDomains...)
	}

	// Request certificate
	certs, err := m.acme.ObtainCertificate(zone.SSLEmail, domains, &zone.Provider, logFn)
	if err != nil {
		return fmt.Errorf("failed to obtain certificate: %w", err)
	}

	// Save certificates
	certDir := filepath.Join(m.certDir, "live", zone.Name)
	if err := os.MkdirAll(certDir, 0700); err != nil {
		return fmt.Errorf("creating cert directory: %w", err)
	}

	// Save fullchain.pem
	if err := os.WriteFile(filepath.Join(certDir, "fullchain.pem"), certs.Certificate, 0600); err != nil {
		return fmt.Errorf("writing certificate: %w", err)
	}

	// Save privkey.pem
	if err := os.WriteFile(filepath.Join(certDir, "privkey.pem"), certs.PrivateKey, 0600); err != nil {
		return fmt.Errorf("writing private key: %w", err)
	}

	// Save issuer cert if available
	if certs.IssuerCertificate != nil {
		if err := os.WriteFile(filepath.Join(certDir, "chain.pem"), certs.IssuerCertificate, 0600); err != nil {
			return fmt.Errorf("writing issuer certificate: %w", err)
		}
	}

	return nil
}

// CertInfo holds detailed certificate information
type CertInfo struct {
	Domain    string   `json:"domain"`
	SANs      []string `json:"sans"`
	Issuer    string   `json:"issuer"`
	NotBefore string   `json:"not_before"`
	NotAfter  string   `json:"not_after"`
	Serial    string   `json:"serial"`
}

// GetCertInfo returns detailed information about a certificate
func GetCertInfo(certPath string) (*CertInfo, error) {
	info := &CertInfo{}

	// Get subject (CN)
	cmd := exec.Command("openssl", "x509", "-in", certPath, "-noout", "-subject")
	if out, err := cmd.Output(); err == nil {
		subject := strings.TrimSpace(string(out))
		if idx := strings.Index(subject, "CN"); idx != -1 {
			cn := subject[idx+2:]
			cn = strings.TrimPrefix(cn, " = ")
			cn = strings.TrimPrefix(cn, "=")
			cn = strings.TrimSpace(cn)
			if commaIdx := strings.Index(cn, ","); commaIdx != -1 {
				cn = cn[:commaIdx]
			}
			info.Domain = cn
		}
	}

	// Get SANs
	cmd = exec.Command("openssl", "x509", "-in", certPath, "-noout", "-ext", "subjectAltName")
	if out, err := cmd.Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "DNS:") || strings.Contains(line, "DNS:") {
				parts := strings.Split(line, ",")
				for _, part := range parts {
					part = strings.TrimSpace(part)
					if strings.HasPrefix(part, "DNS:") {
						san := strings.TrimPrefix(part, "DNS:")
						info.SANs = append(info.SANs, san)
					}
				}
			}
		}
	}

	// Get issuer
	cmd = exec.Command("openssl", "x509", "-in", certPath, "-noout", "-issuer")
	if out, err := cmd.Output(); err == nil {
		issuer := strings.TrimSpace(string(out))
		issuer = strings.TrimPrefix(issuer, "issuer=")
		issuer = strings.TrimSpace(issuer)
		info.Issuer = issuer
	}

	// Get validity dates
	cmd = exec.Command("openssl", "x509", "-in", certPath, "-noout", "-dates")
	if out, err := cmd.Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "notBefore=") {
				info.NotBefore = strings.TrimPrefix(line, "notBefore=")
			} else if strings.HasPrefix(line, "notAfter=") {
				info.NotAfter = strings.TrimPrefix(line, "notAfter=")
			}
		}
	}

	// Get serial
	cmd = exec.Command("openssl", "x509", "-in", certPath, "-noout", "-serial")
	if out, err := cmd.Output(); err == nil {
		serial := strings.TrimSpace(string(out))
		serial = strings.TrimPrefix(serial, "serial=")
		info.Serial = serial
	}

	return info, nil
}

// GetCertInfoForZone returns cert info for a zone
func (m *Manager) GetCertInfoForZone(zoneName string) (*CertInfo, error) {
	certPath := filepath.Join(m.certDir, "live", zoneName, "fullchain.pem")

	if _, err := os.Stat(certPath); err != nil {
		return nil, fmt.Errorf("certificate not found: %s", certPath)
	}

	return GetCertInfo(certPath)
}
