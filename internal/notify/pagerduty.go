package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Test edilebilirlik için değiştirilebilir; gerçek adres PagerDuty Events API v2.
var pagerdutyAPI = "https://events.pagerduty.com/v2/enqueue"

func init() { Register("pagerduty", pagerduty{}) }

type pagerdutyConfig struct {
	RoutingKey string `json:"routing_key"`
	Severity   string `json:"severity"` // critical | error | warning | info
}

type pagerduty struct{}

func (pagerduty) Secrets() []string { return []string{"routing_key"} }

func (pagerduty) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c pagerdutyConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.RoutingKey = strings.TrimSpace(c.RoutingKey)
	if err := required("Routing key", c.RoutingKey); err != nil {
		return nil, err
	}
	c.Severity = strings.TrimSpace(strings.ToLower(c.Severity))
	if c.Severity == "" {
		c.Severity = "critical"
	}
	switch c.Severity {
	case "critical", "error", "warning", "info":
	default:
		return nil, invalid("Önem derecesi critical, error, warning veya info olmalı")
	}
	return encode(c), nil
}

// pagerdutyDedupKey: down/reminder ve up aynı anahtarı kullanır (tetikle/çöz);
// sertifika uyarısı ayrı bir olay olduğundan kendi anahtarını kullanır.
func pagerdutyDedupKey(ev Event) string {
	if ev.Kind == KindCert {
		return fmt.Sprintf("uptime-monitor-%d-cert", ev.MonitorID)
	}
	return fmt.Sprintf("uptime-monitor-%d", ev.MonitorID)
}

func (pagerduty) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c pagerdutyConfig
	json.Unmarshal(raw, &c)

	action := "trigger"
	if ev.Kind == KindUp {
		action = "resolve"
	}
	payload := map[string]any{
		"routing_key":  c.RoutingKey,
		"event_action": action,
		"dedup_key":    pagerdutyDedupKey(ev),
	}
	if action == "trigger" {
		severity := c.Severity
		if ev.Kind == KindCert {
			severity = "warning"
		}
		source := ev.Target
		if source == "" {
			source = "uptime-monitor"
		}
		payload["payload"] = map[string]any{
			"summary":   ev.Title(),
			"source":    source,
			"severity":  severity,
			"timestamp": ev.Time.UTC().Format(time.RFC3339),
			"custom_details": map[string]any{
				"detay": ev.Text(),
			},
		}
	}
	return postJSON(ctx, pagerdutyAPI, payload, nil)
}
