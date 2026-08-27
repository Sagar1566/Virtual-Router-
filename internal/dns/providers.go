package dns

import (
	"context"
	"fmt"
	"strings"

	"github.com/iodesystems/ubuntu-router/internal/config"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/libdns/cloudflare"
	"github.com/libdns/digitalocean"
	"github.com/libdns/duckdns"
	"github.com/libdns/gandi"
	"github.com/libdns/googleclouddns"
	"github.com/libdns/hetzner"
	"github.com/libdns/libdns"
	libdnsroute53 "github.com/libdns/route53"
)

// Route53Wrapper wraps the libdns route53 provider and adds zone listing support
type Route53Wrapper struct {
	*libdnsroute53.Provider
	cfg *config.DNSProviderConfig
}

// ListZones implements ZoneLister for Route53 using the AWS SDK
func (w *Route53Wrapper) ListZones(ctx context.Context) ([]libdns.Zone, error) {
	// Build AWS config
	opts := []func(*awsconfig.LoadOptions) error{}

	if w.cfg.AWSRegion != "" {
		opts = append(opts, awsconfig.WithRegion(w.cfg.AWSRegion))
	}

	if w.cfg.AWSProfile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(w.cfg.AWSProfile))
	}

	if w.cfg.AWSAccessKeyID != "" && w.cfg.AWSSecretAccessKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				w.cfg.AWSAccessKeyID,
				w.cfg.AWSSecretAccessKey,
				"",
			),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := route53.NewFromConfig(awsCfg)

	var zones []libdns.Zone
	var marker *string

	for {
		input := &route53.ListHostedZonesInput{
			Marker: marker,
		}

		output, err := client.ListHostedZones(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("failed to list hosted zones: %w", err)
		}

		for _, hz := range output.HostedZones {
			name := aws.ToString(hz.Name)
			zones = append(zones, libdns.Zone{
				Name: name,
			})
		}

		if !output.IsTruncated {
			break
		}
		marker = output.NextMarker
	}

	return zones, nil
}

// NewRoute53Provider creates a new Route53 DNS provider using libdns
func NewRoute53Provider(cfg *config.DNSProviderConfig, zoneName string) (Provider, error) {
	// Either AWS profile or explicit credentials are required
	if cfg.AWSProfile == "" && (cfg.AWSAccessKeyID == "" || cfg.AWSSecretAccessKey == "") {
		return nil, fmt.Errorf("route53 requires aws_profile or aws_access_key_id + aws_secret_access_key")
	}

	libdnsProvider := &libdnsroute53.Provider{
		Profile:         cfg.AWSProfile,
		AccessKeyId:     cfg.AWSAccessKeyID,
		SecretAccessKey: cfg.AWSSecretAccessKey,
		Region:          cfg.AWSRegion,
		HostedZoneID:    cfg.AWSHostedZoneID,
	}

	// Wrap to add ListZones support
	wrapper := &Route53Wrapper{
		Provider: libdnsProvider,
		cfg:      cfg,
	}

	// Ensure zone has trailing dot
	zone := zoneName
	if zone != "" && !strings.HasSuffix(zone, ".") {
		zone += "."
	}

	return &LibdnsAdapter{
		name:        "route53",
		zone:        zone,
		provider:    wrapper,
		rawProvider: wrapper,
	}, nil
}

// NewCloudflareProvider creates a new Cloudflare DNS provider using libdns
func NewCloudflareProvider(cfg *config.DNSProviderConfig, zoneName string) (Provider, error) {
	if cfg.CloudflareAPIToken == "" && cfg.APIToken == "" {
		return nil, fmt.Errorf("cloudflare requires cloudflare_api_token or api_token")
	}

	token := cfg.CloudflareAPIToken
	if token == "" {
		token = cfg.APIToken
	}

	provider := &cloudflare.Provider{
		APIToken: token,
	}

	return NewLibdnsAdapter("cloudflare", zoneName, provider), nil
}

// NewDigitalOceanProvider creates a new DigitalOcean DNS provider using libdns
func NewDigitalOceanProvider(cfg *config.DNSProviderConfig, zoneName string) (Provider, error) {
	if cfg.APIToken == "" {
		return nil, fmt.Errorf("digitalocean requires api_token")
	}

	provider := &digitalocean.Provider{
		APIToken: cfg.APIToken,
	}

	return NewLibdnsAdapter("digitalocean", zoneName, provider), nil
}

// NewHetznerProvider creates a new Hetzner DNS provider using libdns
func NewHetznerProvider(cfg *config.DNSProviderConfig, zoneName string) (Provider, error) {
	if cfg.APIToken == "" {
		return nil, fmt.Errorf("hetzner requires api_token")
	}

	provider := hetzner.New(cfg.APIToken)

	return NewLibdnsAdapter("hetzner", zoneName, provider), nil
}

// NewGandiProvider creates a new Gandi DNS provider using libdns
func NewGandiProvider(cfg *config.DNSProviderConfig, zoneName string) (Provider, error) {
	if cfg.APIToken == "" {
		return nil, fmt.Errorf("gandi requires api_token (bearer token)")
	}

	provider := &gandi.Provider{
		BearerToken: cfg.APIToken,
	}

	return NewLibdnsAdapter("gandi", zoneName, provider), nil
}

// NewGoogleCloudProvider creates a new Google Cloud DNS provider using libdns
func NewGoogleCloudProvider(cfg *config.DNSProviderConfig, zoneName string) (Provider, error) {
	if cfg.GCPProject == "" {
		return nil, fmt.Errorf("googlecloud requires gcp_project")
	}

	provider := &googleclouddns.Provider{
		Project:            cfg.GCPProject,
		ServiceAccountJSON: cfg.GCPServiceAccountJSON,
	}

	return NewLibdnsAdapter("googlecloud", zoneName, provider), nil
}

// NewDuckDNSProvider creates a new DuckDNS provider using libdns
// Note: DuckDNS only supports A/AAAA/TXT records for *.duckdns.org subdomains
func NewDuckDNSProvider(cfg *config.DNSProviderConfig, zoneName string) (Provider, error) {
	if cfg.APIToken == "" {
		return nil, fmt.Errorf("duckdns requires api_token")
	}

	provider := &duckdns.Provider{
		APIToken: cfg.APIToken,
	}

	return NewLibdnsAdapter("duckdns", zoneName, provider), nil
}
