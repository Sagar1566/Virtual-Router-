package acme

import (
	"fmt"
	"os"

	"github.com/iodesystems/ubuntu-router/internal/config"

	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/providers/dns/cloudflare"
	"github.com/go-acme/lego/v4/providers/dns/digitalocean"
	"github.com/go-acme/lego/v4/providers/dns/gandi"
	"github.com/go-acme/lego/v4/providers/dns/gcloud"
	"github.com/go-acme/lego/v4/providers/dns/hetzner"
	"github.com/go-acme/lego/v4/providers/dns/route53"
)

// CreateChallengeProvider creates a Lego DNS challenge provider from configuration
func CreateChallengeProvider(cfg *config.DNSProviderConfig) (challenge.Provider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("dns provider config is nil")
	}

	switch cfg.Type {
	case config.DNSProviderRoute53:
		return createRoute53Provider(cfg)
	case config.DNSProviderCloudflare:
		return createCloudflareProvider(cfg)
	case config.DNSProviderDigitalOcean:
		return createDigitalOceanProvider(cfg)
	case config.DNSProviderHetzner:
		return createHetznerProvider(cfg)
	case config.DNSProviderGandi:
		return createGandiProvider(cfg)
	case config.DNSProviderGoogleCloud:
		return createGoogleCloudProvider(cfg)
	default:
		return nil, fmt.Errorf("unsupported dns provider type for ACME: %s", cfg.Type)
	}
}

// createRoute53Provider creates a Lego Route53 provider
func createRoute53Provider(cfg *config.DNSProviderConfig) (challenge.Provider, error) {
	// Set environment variables for lego's route53 provider
	if cfg.AWSAccessKeyID != "" {
		os.Setenv("AWS_ACCESS_KEY_ID", cfg.AWSAccessKeyID)
	}
	if cfg.AWSSecretAccessKey != "" {
		os.Setenv("AWS_SECRET_ACCESS_KEY", cfg.AWSSecretAccessKey)
	}
	if cfg.AWSRegion != "" {
		os.Setenv("AWS_REGION", cfg.AWSRegion)
	}
	if cfg.AWSHostedZoneID != "" {
		os.Setenv("AWS_HOSTED_ZONE_ID", cfg.AWSHostedZoneID)
	}
	if cfg.AWSProfile != "" {
		os.Setenv("AWS_PROFILE", cfg.AWSProfile)
	}

	provider, err := route53.NewDNSProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to create route53 provider: %w", err)
	}

	return provider, nil
}

// createCloudflareProvider creates a Lego Cloudflare provider
func createCloudflareProvider(cfg *config.DNSProviderConfig) (challenge.Provider, error) {
	token := cfg.CloudflareAPIToken
	if token == "" {
		token = cfg.APIToken
	}
	if token != "" {
		os.Setenv("CF_DNS_API_TOKEN", token)
	}

	provider, err := cloudflare.NewDNSProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to create cloudflare provider: %w", err)
	}

	return provider, nil
}

// createDigitalOceanProvider creates a Lego DigitalOcean provider
func createDigitalOceanProvider(cfg *config.DNSProviderConfig) (challenge.Provider, error) {
	if cfg.APIToken != "" {
		os.Setenv("DO_AUTH_TOKEN", cfg.APIToken)
	}

	provider, err := digitalocean.NewDNSProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to create digitalocean provider: %w", err)
	}

	return provider, nil
}

// createHetznerProvider creates a Lego Hetzner provider
func createHetznerProvider(cfg *config.DNSProviderConfig) (challenge.Provider, error) {
	if cfg.APIToken != "" {
		os.Setenv("HETZNER_API_KEY", cfg.APIToken)
	}

	provider, err := hetzner.NewDNSProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to create hetzner provider: %w", err)
	}

	return provider, nil
}

// createGandiProvider creates a Lego Gandi provider
func createGandiProvider(cfg *config.DNSProviderConfig) (challenge.Provider, error) {
	if cfg.APIToken != "" {
		os.Setenv("GANDI_API_KEY", cfg.APIToken)
	}

	provider, err := gandi.NewDNSProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to create gandi provider: %w", err)
	}

	return provider, nil
}

// createGoogleCloudProvider creates a Lego Google Cloud DNS provider
func createGoogleCloudProvider(cfg *config.DNSProviderConfig) (challenge.Provider, error) {
	if cfg.GCPProject != "" {
		os.Setenv("GCE_PROJECT", cfg.GCPProject)
	}
	if cfg.GCPServiceAccountJSON != "" {
		os.Setenv("GCE_SERVICE_ACCOUNT", cfg.GCPServiceAccountJSON)
	}

	provider, err := gcloud.NewDNSProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to create googlecloud provider: %w", err)
	}

	return provider, nil
}

// ProviderName returns the name of the provider for a given config
func ProviderName(cfg *config.DNSProviderConfig) string {
	if cfg == nil {
		return "unknown"
	}
	return string(cfg.Type)
}
