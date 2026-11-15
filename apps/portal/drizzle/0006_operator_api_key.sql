CREATE TABLE "operator_api_key" (
	"id" uuid PRIMARY KEY DEFAULT gen_random_uuid() NOT NULL,
	"name" text NOT NULL,
	"prefix" text NOT NULL,
	"key_hash" "bytea" NOT NULL,
	"created_by" text NOT NULL,
	"created_at" timestamp with time zone DEFAULT now() NOT NULL,
	"revoked_at" timestamp with time zone,
	"last_used_on" date,
	CONSTRAINT "operator_api_key_prefix_unique" UNIQUE("prefix"),
	CONSTRAINT "operator_api_key_prefix_format" CHECK ("operator_api_key"."prefix" ~ '^[a-z2-7]{8}$'),
	CONSTRAINT "operator_api_key_hash_length" CHECK (octet_length("operator_api_key"."key_hash") = 32),
	CONSTRAINT "operator_api_key_name_length" CHECK (char_length("operator_api_key"."name") between 1 and 100)
);
--> statement-breakpoint
ALTER TABLE "operator_api_key" ADD CONSTRAINT "operator_api_key_created_by_user_id_fk" FOREIGN KEY ("created_by") REFERENCES "public"."user"("id") ON DELETE no action ON UPDATE no action;