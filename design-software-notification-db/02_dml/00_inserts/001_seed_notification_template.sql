INSERT INTO notification.notification_template (code, name, channel, subject_template, body_template)
VALUES ('SCHEDULE_PUBLISHED', 'Horario publicado', 'EMAIL',
        'Tu horario {{schedule_name}} fue publicado',
        'El horario {{schedule_name}} de la ficha {{ficha}} ha sido publicado.')
ON CONFLICT (code) DO NOTHING;

INSERT INTO notification.notification_template (code, name, channel, subject_template, body_template)
VALUES ('ALERT_TRIGGERED', 'Alerta de seguimiento', 'IN_APP',
        'Alerta: {{alert_type}}',
        'Se genero una alerta {{alert_type}} en la ficha {{ficha}}.')
ON CONFLICT (code) DO NOTHING;
