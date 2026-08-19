package usecase

import (
	"context"
	"time"

	"github.com/code-sena/design-software-notification-service/internal/application/port/in"
	"github.com/code-sena/design-software-notification-service/internal/application/port/out"
	"github.com/code-sena/design-software-notification-service/internal/domain/model"
	"github.com/code-sena/design-software-notification-service/internal/domain/service"
)

type SendNotification struct {
	repo     out.NotificationRepository
	tmplRepo out.TemplateRepository
}

func NewSendNotification(repo out.NotificationRepository, tmplRepo out.TemplateRepository) *SendNotification {
	return &SendNotification{repo: repo, tmplRepo: tmplRepo}
}

// Handle asume que cmd ya paso la validacion de contrato del adapter HTTP.
// Si cmd.TemplateCode no esta vacio, carga la plantilla y renderiza subject/body;
// si la plantilla no existe o esta inactiva, cae al subject explicito de cmd (E2/E3).
func (s *SendNotification) Handle(ctx context.Context, cmd in.SendNotificationCommand) (*model.SentNotification, error) {
	n := &model.SentNotification{
		RecipientID:    cmd.RecipientID,
		RecipientEmail: cmd.RecipientEmail,
		Channel:        cmd.Channel,
		Subject:        cmd.Subject,
		SendStatus:     model.StatusPending,
		SourceService:  cmd.SourceService,
		SourceEventID:  cmd.SourceEventID,
		CreatedAt:      time.Now(),
	}

	if cmd.TemplateCode != "" {
		if tmpl, err := s.tmplRepo.FindByCode(ctx, cmd.TemplateCode); err == nil && tmpl != nil && tmpl.IsActive {
			n.Subject = service.Render(tmpl.SubjectTemplate, cmd.TemplateVars)
			n.BodySummary = service.Render(tmpl.BodyTemplate, cmd.TemplateVars)
			n.TemplateID = tmpl.ID
		}
		// E2: template not found → keep explicit cmd.Subject (always present, required field)
		// E3: template inactive  → keep explicit cmd.Subject
	}

	if err := s.repo.Save(ctx, n); err != nil {
		return nil, err
	}
	return n, nil
}
