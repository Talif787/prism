package app

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/Talif787/prism/internal/alerting/domain"
)

type fakeStore struct{ instances map[string]*domain.Instance }

func newFakeStore() *fakeStore { return &fakeStore{instances: map[string]*domain.Instance{}} }

func (f *fakeStore) ListInstances(_ context.Context, _ string) ([]domain.Instance, error) {
	out := make([]domain.Instance, 0, len(f.instances))
	for _, v := range f.instances {
		out = append(out, *v)
	}
	return out, nil
}
func (f *fakeStore) UpsertInstance(_ context.Context, i *domain.Instance) error {
	cp := *i
	f.instances[i.Fingerprint] = &cp
	return nil
}
func (f *fakeStore) DeleteInstance(_ context.Context, _ string, fp string) error {
	delete(f.instances, fp)
	return nil
}

// Unused by evaluateRule; present to satisfy the interface.
func (f *fakeStore) CreateRule(context.Context, *domain.Rule) error                 { return nil }
func (f *fakeStore) GetRule(context.Context, string, string) (*domain.Rule, error)  { return nil, nil }
func (f *fakeStore) ListRules(context.Context, string) ([]domain.Rule, error)       { return nil, nil }
func (f *fakeStore) UpdateRule(context.Context, *domain.Rule) error                 { return nil }
func (f *fakeStore) DeleteRule(context.Context, string, string) error               { return nil }
func (f *fakeStore) LoadDueRules(context.Context, time.Time) ([]domain.Rule, error) { return nil, nil }
func (f *fakeStore) MarkEvaluated(context.Context, string, time.Time) error         { return nil }
func (f *fakeStore) ListTenantInstances(context.Context, string) ([]domain.Instance, error) {
	return nil, nil
}

type fakeReader struct{ vals []SeriesValue }

func (f fakeReader) Read(context.Context, string, Condition) ([]SeriesValue, error) {
	return f.vals, nil
}

type fakeNotifier struct{ events []string }

func (f *fakeNotifier) Notify(_ context.Context, _ string, n Notification) error {
	f.events = append(f.events, n.Event)
	return nil
}

func newEval(store RuleStore, vals []SeriesValue, note Notifier) *Evaluator {
	return NewEvaluator(store, fakeReader{vals: vals}, note, slog.Default())
}

func ruleGT(threshold float64, forDur time.Duration) *domain.Rule {
	return &domain.Rule{
		ID: "rule1", TenantID: "t1", Name: "cpu high", Metric: "cpu", Agg: "avg",
		GroupBy: []string{"route"}, Operator: domain.OpGreaterThan, Threshold: threshold,
		Window: time.Minute, Interval: 30 * time.Second, For: forDur, Webhook: "http://example.test/hook",
	}
}

func TestEvaluate_ImmediateFiring(t *testing.T) {
	store := newFakeStore()
	note := &fakeNotifier{}
	e := newEval(store, []SeriesValue{{Labels: map[string]string{"route": "/a"}, Value: 20}}, note)

	if err := e.evaluateRule(context.Background(), ruleGT(10, 0), time.Now()); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(store.instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(store.instances))
	}
	for _, inst := range store.instances {
		if inst.State != domain.StateFiring {
			t.Fatalf("expected firing, got %s", inst.State)
		}
	}
	if len(note.events) != 1 || note.events[0] != "firing" {
		t.Fatalf("expected one firing notification, got %v", note.events)
	}
}

func TestEvaluate_PendingThenFiring(t *testing.T) {
	store := newFakeStore()
	note := &fakeNotifier{}
	vals := []SeriesValue{{Labels: map[string]string{"route": "/a"}, Value: 20}}
	rule := ruleGT(10, 2*time.Minute)
	t0 := time.Now()

	// First evaluation: pending, no notification.
	e := newEval(store, vals, note)
	_ = e.evaluateRule(context.Background(), rule, t0)
	for _, inst := range store.instances {
		if inst.State != domain.StatePending {
			t.Fatalf("expected pending, got %s", inst.State)
		}
	}
	if len(note.events) != 0 {
		t.Fatalf("expected no notification while pending, got %v", note.events)
	}

	// After the 'for' duration: fires.
	_ = e.evaluateRule(context.Background(), rule, t0.Add(2*time.Minute))
	if len(note.events) != 1 || note.events[0] != "firing" {
		t.Fatalf("expected firing after 'for', got %v", note.events)
	}
}

func TestEvaluate_Resolves(t *testing.T) {
	store := newFakeStore()
	note := &fakeNotifier{}
	rule := ruleGT(10, 0)
	t0 := time.Now()

	// Fire.
	newEval(store, []SeriesValue{{Labels: map[string]string{"route": "/a"}, Value: 20}}, note).
		evaluateRule(context.Background(), rule, t0)
	// Next cycle the value is back to normal: resolves and is removed.
	newEval(store, []SeriesValue{{Labels: map[string]string{"route": "/a"}, Value: 1}}, note).
		evaluateRule(context.Background(), rule, t0.Add(time.Minute))

	if len(store.instances) != 0 {
		t.Fatalf("expected instance removed on resolve, got %d", len(store.instances))
	}
	if len(note.events) != 2 || note.events[1] != "resolved" {
		t.Fatalf("expected firing then resolved, got %v", note.events)
	}
}
