-- Escrita à mão (drizzle-kit generate --custom). Spec do E2, D2 (#19).
--
-- Operador SCF não é membro de tenant. O banco recusa as duas formas de
-- quebrar a regra: pôr um operador num tenant e promover um membro a
-- operador. É uma segunda barreira; o portal e o script de administração
-- já conferem antes.
CREATE FUNCTION operator_not_member() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_TABLE_NAME = 'tenant_member' THEN
    IF EXISTS (SELECT 1 FROM "user" WHERE id = NEW.user_id AND role = 'operator') THEN
      RAISE EXCEPTION 'operador não pode ser membro de tenant (D2)' USING ERRCODE = 'check_violation';
    END IF;
  ELSIF NEW.role = 'operator' AND OLD.role IS DISTINCT FROM 'operator' THEN
    IF EXISTS (SELECT 1 FROM tenant_member WHERE user_id = NEW.id) THEN
      RAISE EXCEPTION 'membro de tenant não pode virar operador (D2)' USING ERRCODE = 'check_violation';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
--> statement-breakpoint
CREATE TRIGGER tenant_member_not_operator
BEFORE INSERT OR UPDATE OF user_id ON tenant_member
FOR EACH ROW EXECUTE FUNCTION operator_not_member();
--> statement-breakpoint
CREATE TRIGGER user_operator_not_member
BEFORE UPDATE OF role ON "user"
FOR EACH ROW EXECUTE FUNCTION operator_not_member();
