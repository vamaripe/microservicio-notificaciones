package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/code-sena/design-software-notification-service/internal/application/port/in"
	"github.com/code-sena/design-software-notification-service/internal/domain/model"
)

// fakeRepo is shared by all usecase tests (Save, SaveWithOutbox, FindByID).
type fakeRepo struct {
	saved   int
	lastArg *model.SentNotification
	err     error

	outboxSaved      int
	lastNotification *model.SentNotification
	lastOutboxEvent  *model.OutboxEvent
	alreadyProcessed bool

	// FindByID-specific
	foundByID *model.SentNotification
	findErr   error
}

func (f *fakeRepo) Save(_ context.Context, n *model.SentNotification) error {
	f.saved++
	f.lastArg = n
	return f.err
}

func (f *fakeRepo) SaveWithOutbox(_ context.Context, n *model.SentNotification, evt *model.OutboxEvent) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	f.outboxSaved++
	f.lastNotification = n
	f.lastOutboxEvent = evt
	return f.alreadyProcessed, nil
}

func (f *fakeRepo) FindByID(_ context.Context, _ string) (*model.SentNotification, error) {
	return f.foundByID, f.findErr
}

// fakeTemplateRepo allows controlling FindByCode results in tests.
type fakeTemplateRepo struct {
	tmpl *model.NotificationTemplate
	err  error
}

func (f *fakeTemplateRepo) FindByCode(_ context.Context, _ string) (*model.NotificationTemplate, error) {
	return f.tmpl, f.err
}

// --- SendNotification tests ---


func TestSendNotification_SetsPendingAndPersists(t *testing.T) {
	repo := &fakeRepo{}
	uc := NewSendNotification(repo, &fakeTemplateRepo{})

	n, err := uc.Handle(context.Background(), in.SendNotificationCommand{
		RecipientID:    "r1",
		RecipientEmail: "a@b.co",
		Channel:        model.ChannelEmail,
		Subject:        "hi",
		SourceService:  "monitoring",
		SourceEventID:  "evt-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.SendStatus != model.StatusPending {
		t.Fatalf("want PENDING, got %s", n.SendStatus)
	}
	if repo.saved != 1 {
		t.Fatalf("want 1 save, got %d", repo.saved)
	}
	if repo.lastArg.SourceService != "monitoring" || repo.lastArg.SourceEventID != "evt-1" {
		t.Fatalf("source_service/source_event_id no se propagaron: %+v", repo.lastArg)
	}
}

func TestSendNotification_PropagatesRepoError(t *testing.T) {
	repo := &fakeRepo{err: errors.New("db down")}
	uc := NewSendNotification(repo, &fakeTemplateRepo{})

	_, err := uc.Handle(context.Background(), in.SendNotificationCommand{
		RecipientID:    "r1",
		RecipientEmail: "a@b.co",
		Channel:        model.ChannelEmail,
		Subject:        "hi",
	})
	if err == nil {
		t.Fatal("esperaba error del repositorio, obtuve nil")
	}
}

// E1: active template renders subject + body and stores template_id.
func TestSendNotification_ActiveTemplate_RendersAndStoresTemplateID(t *testing.T) {
	repo := &fakeRepo{}
	tmpl := &model.NotificationTemplate{
		ID:              "tmpl-uuid-1",
		Code:            "SCHEDULE_PUBLISHED",
		Channel:         model.ChannelEmail,
		SubjectTemplate: "Tu horario {{schedule_name}} fue publicado",
		BodyTemplate:    "El horario {{schedule_name}} de la ficha {{ficha}} ha sido publicado.",
		IsActive:        true,
	}
	uc := NewSendNotification(repo, &fakeTemplateRepo{tmpl: tmpl})

	n, err := uc.Handle(context.Background(), in.SendNotificationCommand{
		RecipientID:    "r1",
		RecipientEmail: "a@b.co",
		Channel:        model.ChannelEmail,
		Subject:        "fallback subject",
		TemplateCode:   "SCHEDULE_PUBLISHED",
		TemplateVars:   map[string]string{"schedule_name": "Ene-2026", "ficha": "2850621"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Subject != "Tu horario Ene-2026 fue publicado" {
		t.Fatalf("subject not rendered: %q", n.Subject)
	}
	if n.BodySummary != "El horario Ene-2026 de la ficha 2850621 ha sido publicado." {
		t.Fatalf("body not rendered: %q", n.BodySummary)
	}
	if n.TemplateID != "tmpl-uuid-1" {
		t.Fatalf("template_id not stored: %q", n.TemplateID)
	}
}

// E2: template_code not found → falls back to explicit subject.
func TestSendNotification_TemplateNotFound_FallsBackToExplicitSubject(t *testing.T) {
	repo := &fakeRepo{}
	uc := NewSendNotification(repo, &fakeTemplateRepo{tmpl: nil})

	n, err := uc.Handle(context.Background(), in.SendNotificationCommand{
		RecipientID:    "r1",
		RecipientEmail: "a@b.co",
		Channel:        model.ChannelEmail,
		Subject:        "explicit subject",
		TemplateCode:   "NONEXISTENT",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Subject != "explicit subject" {
		t.Fatalf("want fallback subject, got %q", n.Subject)
	}
	if n.TemplateID != "" {
		t.Fatalf("template_id should be empty when template not found, got %q", n.TemplateID)
	}
}

// E3: inactive template → falls back to explicit subject.
func TestSendNotification_InactiveTemplate_FallsBackToExplicitSubject(t *testing.T) {
	repo := &fakeRepo{}
	inactiveTmpl := &model.NotificationTemplate{
		ID:              "tmpl-uuid-2",
		Code:            "ALERT_TRIGGERED",
		IsActive:        false,
		SubjectTemplate: "should not be used",
	}
	uc := NewSendNotification(repo, &fakeTemplateRepo{tmpl: inactiveTmpl})

	n, err := uc.Handle(context.Background(), in.SendNotificationCommand{
		RecipientID:    "r1",
		RecipientEmail: "a@b.co",
		Channel:        model.ChannelEmail,
		Subject:        "explicit subject",
		TemplateCode:   "ALERT_TRIGGERED",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Subject != "explicit subject" {
		t.Fatalf("want fallback subject for inactive template, got %q", n.Subject)
	}
	if n.TemplateID != "" {
		t.Fatalf("template_id should be empty for inactive template, got %q", n.TemplateID)
	}
}
