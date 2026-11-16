-- Contas de desenvolvimento, só para o Compose de dev (SEED_DEV_DATA=true).
-- NUNCA entram em produção: as senhas são conhecidas e uma das contas é operador SCF.
-- Idempotente: se a conta já existe (criada à mão), fica como está.
--   admin@example.com  senha admin321admin   (usuário comum, ainda sem projeto)
--   scf@example.org    senha scf-admin-12345  (operador SCF)
INSERT INTO "user" (id, name, email, email_verified, role)
VALUES
	('00000000-0000-4000-8000-0000000000a1', 'Admin Teste', 'admin@example.com', true, 'tenant'),
	('00000000-0000-4000-8000-0000000000a2', 'SCF', 'scf@example.org', true, 'operator')
ON CONFLICT (email) DO NOTHING;--> statement-breakpoint
INSERT INTO account (id, account_id, provider_id, user_id, password)
SELECT
	gen_random_uuid()::text, u.id, 'credential', u.id,
	CASE u.email
		WHEN 'admin@example.com' THEN '8024fa96de007b1263fadee9ebe7aa33:1ab9ed9dd28c90c22f66eb76c352e5c3e771eb78169c2f4d0e9c0795f334de8b2903ba246398129c5d1ec97d7238b58a1cdefdde4a048562d31891541158556c'
		ELSE '932e3f2a9ae36c2e1d25ff84dc418de3:527b594ac8a41c16c0f1d23708ae9e82812bbf9879a35f59e615c3df43354194afa91dd130a50c736edf058f12056cab6d057df94fc9fa73d948aa74f9041eb5'
	END
FROM "user" u
WHERE u.email IN ('admin@example.com', 'scf@example.org')
	AND NOT EXISTS (SELECT 1 FROM account a WHERE a.user_id = u.id AND a.provider_id = 'credential');--> statement-breakpoint
INSERT INTO audit_log (actor_user_id, action, details)
SELECT NULL, 'operator.created', jsonb_build_object('userId', u.id)
FROM "user" u
WHERE u.email = 'scf@example.org'
	AND NOT EXISTS (SELECT 1 FROM audit_log l WHERE l.action = 'operator.created' AND l.details->>'userId' = u.id);
