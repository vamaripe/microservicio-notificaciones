-- Grants least-privilege dominio notification
GRANT USAGE ON SCHEMA notification TO notification_reader, notification_writer, notification_admin;
GRANT SELECT ON ALL TABLES IN SCHEMA notification TO notification_reader;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA notification TO notification_writer;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA notification TO notification_writer;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA notification TO notification_admin;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA notification TO notification_admin;
ALTER DEFAULT PRIVILEGES IN SCHEMA notification GRANT SELECT ON TABLES TO notification_reader;
ALTER DEFAULT PRIVILEGES IN SCHEMA notification GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO notification_writer;
ALTER DEFAULT PRIVILEGES IN SCHEMA notification GRANT ALL ON TABLES TO notification_admin;
