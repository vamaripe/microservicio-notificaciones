-- Roles de aplicacion (least-privilege) para el dominio notification
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='notification_reader') THEN CREATE ROLE notification_reader NOLOGIN; END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='notification_writer') THEN CREATE ROLE notification_writer NOLOGIN; END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='notification_admin')  THEN CREATE ROLE notification_admin  NOLOGIN; END IF;
END $$;
