// Package webhook implements the alerting Notifier by POSTing a JSON payload to a
// rule's configured URL on firing and resolved transitions.
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Talif787/prism/internal/alerting/app"
)

type Notifier struct{ client *http.Client }

func New(timeout time.Duration) *Notifier {
	return &Notifier{client: &http.Client{Timeout: timeout}}
}

type payload struct {
	Event       string            `json:"event"`
	State       string            `json:"state"`
	RuleID      string            `json:"rule_id"`
	RuleName    string            `json:"rule_name"`
	Severity    string            `json:"severity"`
	TenantID    string            `json:"tenant_id"`
	Metric      string            `json:"metric"`
	Operator    string            `json:"operator"`
	Threshold   float64           `json:"threshold"`
	Value       float64           `json:"value"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	ActiveSince time.Time         `json:"active_since"`
	FiredAt     *time.Time        `json:"fired_at,omitempty"`
}

func (n *Notifier) Notify(ctx context.Context, webhook string, note app.Notification) error {
	p := payload{
		Event:       note.Event,
		State:       string(note.Instance.State),
		RuleID:      note.Rule.ID,
		RuleName:    note.Rule.Name,
		Severity:    note.Rule.Severity,
		TenantID:    note.Rule.TenantID,
		Metric:      note.Rule.Metric,
		Operator:    string(note.Rule.Operator),
		Threshold:   note.Rule.Threshold,
		Value:       note.Instance.Value,
		Labels:      note.Instance.Labels,
		Annotations: note.Rule.Annotations,
		ActiveSince: note.Instance.ActiveSince,
		FiredAt:     note.Instance.FiredAt,
	}
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}
