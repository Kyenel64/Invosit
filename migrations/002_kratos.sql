-- Move identity, sessions, and password hashing to Ory Kratos.
-- Local users table now references Kratos identities by UUID.

ALTER TABLE users DROP COLUMN password_hash;
ALTER TABLE users DROP COLUMN email_verified;

ALTER TABLE users ADD COLUMN kratos_identity_id UUID NOT NULL;
ALTER TABLE users ADD CONSTRAINT users_kratos_identity_id_unique UNIQUE (kratos_identity_id);

DROP TABLE refresh_tokens;
DROP TABLE user_identities;
