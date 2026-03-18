package processor

import (
	"context"
	"fmt"
	"strings"
)

const SpamFilterType = "spam_filter"

// SpamFilterConfig represents spam filter configuration
type SpamFilterConfig struct {
	Threshold   float64  `json:"threshold"`     // Spam score threshold (0.0 - 1.0)
	Action      string   `json:"action"`        // Action on spam: "tag", "move", "delete"
	SpamFolder  string   `json:"spam_folder"`   // Folder to move spam to (if action=move)
	Keywords    []string `json:"keywords"`      // Custom spam keywords
	Whitelist   []string `json:"whitelist"`     // Whitelisted senders
	Blacklist   []string `json:"blacklist"`     // Blacklisted senders
	AbortOnSpam bool     `json:"abort_on_spam"` // Abort pipeline if spam detected
}

// SpamFilterDetector determines if spam filtering should run
type SpamFilterDetector struct {
	whitelist []string
	blacklist []string
}

func (d *SpamFilterDetector) ShouldProcess(ctx context.Context, email *Email, state *PipelineState) (bool, error) {
	// Always run spam filter unless already processed
	if state.GetProcessorResult(SpamFilterType) != nil {
		return false, nil
	}

	return true, nil
}

// SpamFilter scores emails for spam likelihood
type SpamFilter struct {
	threshold   float64
	action      string
	spamFolder  string
	keywords    []string
	whitelist   []string
	blacklist   []string
	abortOnSpam bool
}

// SpamFilterMeta extracts configuration from ProcessorMeta
func SpamFilterMeta(meta ProcessorMeta) (*SpamFilterConfig, error) {
	config := &SpamFilterConfig{
		Threshold:   0.8,   // Default threshold
		Action:      "tag", // Default action
		SpamFolder:  "Spam",
		AbortOnSpam: false,
	}

	// Parse threshold
	if t := meta.Get("threshold"); t != nil {
		if threshold, ok := t.(float64); ok {
			config.Threshold = threshold
		}
	}

	config.Action = meta.GetString("action")
	if config.Action == "" {
		config.Action = "tag"
	}

	config.SpamFolder = meta.GetString("spam_folder")
	if config.SpamFolder == "" {
		config.SpamFolder = "Spam"
	}

	config.AbortOnSpam = meta.GetBool("abort_on_spam")
	config.Keywords = meta.GetStringSlice("keywords")
	config.Whitelist = meta.GetStringSlice("whitelist")
	config.Blacklist = meta.GetStringSlice("blacklist")

	return config, nil
}

// NewSpamFilter creates a new spam filter processor
func NewSpamFilter(meta ProcessorMeta) (Processor, error) {
	config, err := SpamFilterMeta(meta)
	if err != nil {
		return nil, err
	}

	return &SpamFilter{
		threshold:   config.Threshold,
		action:      config.Action,
		spamFolder:  config.SpamFolder,
		keywords:    config.Keywords,
		whitelist:   config.Whitelist,
		blacklist:   config.Blacklist,
		abortOnSpam: config.AbortOnSpam,
	}, nil
}

// NewSpamFilterDetector creates a new spam filter detector
func NewSpamFilterDetector(meta ProcessorMeta) (Detector, error) {
	config, err := SpamFilterMeta(meta)
	if err != nil {
		return nil, err
	}

	return &SpamFilterDetector{
		whitelist: config.Whitelist,
		blacklist: config.Blacklist,
	}, nil
}

func (p *SpamFilter) Type() string {
	return SpamFilterType
}

func (p *SpamFilter) Detector() Detector {
	return &SpamFilterDetector{
		whitelist: p.whitelist,
		blacklist: p.blacklist,
	}
}

func (p *SpamFilter) Process(ctx context.Context, email *Email, state *PipelineState) error {
	// Check whitelist first
	if p.isWhitelisted(email.From) {
		state.Set("spam_whitelisted", true)
		return nil
	}

	// Check blacklist - auto-mark as spam
	if p.isBlacklisted(email.From) {
		p.markAsSpam(email, 1.0, state)
		if p.abortOnSpam {
			state.Abort("Sender blacklisted")
		}
		return nil
	}

	// Calculate spam score
	score := p.calculateSpamScore(email)

	// Store spam score in headers
	if email.Headers == nil {
		email.Headers = make(map[string]string)
	}
	email.Headers["X-Spam-Score"] = fmt.Sprintf("%.2f", score)

	// Take action if above threshold
	if score >= p.threshold {
		p.markAsSpam(email, score, state)
		if p.abortOnSpam {
			state.Abort(fmt.Sprintf("Spam score %.2f exceeds threshold %.2f", score, p.threshold))
		}
	} else {
		state.Set("spam_flagged", false)
		state.Set("spam_score", score)
	}

	return nil
}

func (p *SpamFilter) calculateSpamScore(email *Email) float64 {
	score := 0.0

	// Check subject for spam indicators
	subjectLower := strings.ToLower(email.Subject)
	spamSubjects := []string{
		"free", "winner", "congratulations", "urgent", "verify your account",
		"lottery", "inheritance", "million dollars", "click here",
	}
	for _, keyword := range spamSubjects {
		if strings.Contains(subjectLower, keyword) {
			score += 0.1
		}
	}

	// Check custom keywords in body
	bodyText := p.getBodyText(email)
	bodyLower := strings.ToLower(bodyText)
	for _, keyword := range p.keywords {
		if strings.Contains(bodyLower, strings.ToLower(keyword)) {
			score += 0.05
		}
	}

	// Check for excessive punctuation
	if strings.Count(email.Subject, "!") > 3 {
		score += 0.1
	}
	if strings.Count(email.Subject, "$") > 2 {
		score += 0.1
	}

	// Cap score at 1.0
	if score > 1.0 {
		score = 1.0
	}

	return score
}

func (p *SpamFilter) markAsSpam(email *Email, score float64, state *PipelineState) {
	if email.Headers == nil {
		email.Headers = make(map[string]string)
	}
	email.Headers["X-Spam-Status"] = "Yes"
	email.Headers["X-Spam-Action"] = p.action

	// Add spam flag
	email.Flags = append(email.Flags, "$Spam")

	// Store in pipeline state for other processors
	state.Set("spam_flagged", true)
	state.Set("spam_score", score)
}

func (p *SpamFilter) isWhitelisted(sender string) bool {
	for _, w := range p.whitelist {
		if strings.EqualFold(sender, w) {
			return true
		}
	}
	return false
}

func (p *SpamFilter) isBlacklisted(sender string) bool {
	for _, b := range p.blacklist {
		if strings.EqualFold(sender, b) {
			return true
		}
	}
	return false
}

func (p *SpamFilter) getBodyText(email *Email) string {
	if email.Body == nil {
		return ""
	}
	if email.Body.PlainText != "" {
		return email.Body.PlainText
	}
	if email.Body.Text != "" {
		return email.Body.Text
	}
	return stripHTML(email.Body.HTML)
}

func init() {
	MustRegister(SpamFilterType, NewSpamFilter, NewSpamFilterDetector, "Scores emails for spam likelihood")
}
