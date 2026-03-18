package processor

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

const LinkTrackerType = "link_tracker"

// LinkTrackerConfig represents link tracker configuration
type LinkTrackerConfig struct {
	BaseURL       string   `json:"base_url"`        // Base URL for tracking links
	Salt          string   `json:"salt"`            // Salt for HMAC signing
	TrackOnlyHTML bool     `json:"track_only_html"` // Only process HTML emails
	IgnoreDomains []string `json:"ignore_domains"`  // Domains to skip tracking
}

// LinkTrackerDetector determines if link tracking should run
type LinkTrackerDetector struct {
	trackOnlyHTML bool
	ignoreDomains []string
}

func (d *LinkTrackerDetector) ShouldProcess(ctx context.Context, email *Email, state *PipelineState) (bool, error) {
	// Skip if no body
	if email.Body == nil {
		return false, nil
	}

	// If trackOnlyHTML is set, skip non-HTML emails
	if d.trackOnlyHTML && email.Body.HTML == "" {
		return false, nil
	}

	// Check if a previous processor (e.g., spam filter) marked this as spam
	spamResult := state.GetProcessorResult("spam_filter")
	if spamResult != nil && spamResult.Success {
		if state.GetBool("spam_flagged") {
			return false, nil // Don't track links in spam
		}
	}

	return true, nil
}

// LinkTracker rewrites links in email body for click tracking
type LinkTracker struct {
	baseURL       string
	salt          string
	linkRE        *regexp.Regexp
	ignoreDomains map[string]struct{}
}

// LinkTrackerMeta extracts configuration from ProcessorMeta
func LinkTrackerMeta(meta ProcessorMeta) (*LinkTrackerConfig, error) {
	config := &LinkTrackerConfig{
		BaseURL:       meta.GetString("base_url"),
		Salt:          meta.GetString("salt"),
		TrackOnlyHTML: meta.GetBool("track_only_html"),
		IgnoreDomains: meta.GetStringSlice("ignore_domains"),
	}

	if config.BaseURL == "" {
		return nil, fmt.Errorf("link_tracker: base_url is required")
	}
	if config.Salt == "" {
		return nil, fmt.Errorf("link_tracker: salt is required")
	}

	return config, nil
}

// NewLinkTracker creates a new link tracker processor
func NewLinkTracker(meta ProcessorMeta) (Processor, error) {
	config, err := LinkTrackerMeta(meta)
	if err != nil {
		return nil, err
	}

	// Build ignore domains map for O(1) lookup
	ignoreMap := make(map[string]struct{})
	for _, d := range config.IgnoreDomains {
		ignoreMap[strings.ToLower(d)] = struct{}{}
	}

	return &LinkTracker{
		baseURL:       strings.TrimSuffix(config.BaseURL, "/"),
		salt:          config.Salt,
		linkRE:        regexp.MustCompile(`href=["']([^"']+)["']`),
		ignoreDomains: ignoreMap,
	}, nil
}

// NewLinkTrackerDetector creates a new link tracker detector
func NewLinkTrackerDetector(meta ProcessorMeta) (Detector, error) {
	config, err := LinkTrackerMeta(meta)
	if err != nil {
		return nil, err
	}

	return &LinkTrackerDetector{
		trackOnlyHTML: config.TrackOnlyHTML,
		ignoreDomains: config.IgnoreDomains,
	}, nil
}

func (p *LinkTracker) Type() string {
	return LinkTrackerType
}

func (p *LinkTracker) Detector() Detector {
	return &LinkTrackerDetector{
		trackOnlyHTML: false,
	}
}

func (p *LinkTracker) Process(ctx context.Context, email *Email, state *PipelineState) error {
	if email.Body == nil || (email.Body.HTML == "" && email.Body.Text == "") {
		return nil // No body to process
	}

	linksProcessed := 0

	// Process HTML body
	if email.Body.HTML != "" {
		email.Body.HTML, linksProcessed = p.rewriteLinks(email.Body.HTML, email.ID)
	}

	// Process plain text body
	if email.Body.Text != "" {
		email.Body.Text = p.rewriteTextLinks(email.Body.Text, email.ID)
	}

	// Store result in pipeline state for other processors
	state.Set("links_processed", linksProcessed)

	return nil
}

func (p *LinkTracker) rewriteLinks(html string, emailID string) (string, int) {
	count := 0
	result := p.linkRE.ReplaceAllStringFunc(html, func(match string) string {
		// Extract original URL
		parts := strings.SplitN(match, "=", 2)
		if len(parts) != 2 {
			return match
		}

		url := strings.Trim(parts[1], `" '`)
		if strings.HasPrefix(url, "http") {
			// Check if domain should be ignored
			if p.shouldIgnoreURL(url) {
				return match
			}

			trackingURL := p.buildTrackingURL(url, emailID)
			count++
			return fmt.Sprintf(`href="%s"`, trackingURL)
		}
		return match
	})
	return result, count
}

func (p *LinkTracker) shouldIgnoreURL(url string) bool {
	// Extract domain from URL
	if strings.Contains(url, "://") {
		parts := strings.SplitN(url, "://", 2)
		if len(parts) > 1 {
			domain := strings.Split(parts[1], "/")[0]
			if _, ignored := p.ignoreDomains[strings.ToLower(domain)]; ignored {
				return true
			}
		}
	}
	return false
}

func (p *LinkTracker) rewriteTextLinks(text string, emailID string) string {
	// For plain text, append tracking info to URLs
	urlRE := regexp.MustCompile(`(https?://\S+)`)
	return urlRE.ReplaceAllStringFunc(text, func(url string) string {
		if !p.shouldIgnoreURL(url) {
			return fmt.Sprintf("%s [tracked]", url)
		}
		return url
	})
}

func (p *LinkTracker) buildTrackingURL(originalURL, emailID string) string {
	// Generate HMAC signature for security
	h := hmac.New(sha256.New, []byte(p.salt))
	h.Write([]byte(originalURL))
	signature := hex.EncodeToString(h.Sum(nil))

	return fmt.Sprintf("%s/track/%s?u=%s&s=%s",
		p.baseURL,
		emailID,
		hex.EncodeToString([]byte(originalURL)),
		signature,
	)
}

func init() {
	MustRegister(LinkTrackerType, NewLinkTracker, NewLinkTrackerDetector, "Rewrites links in emails for click tracking")
}
