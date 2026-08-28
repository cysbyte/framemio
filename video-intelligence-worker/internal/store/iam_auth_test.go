package store

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestUseIAMDatabaseAuth(t *testing.T) {
	t.Setenv("DATABASE_IAM_AUTH", "")

	t.Setenv("DATABASE_AUTH_MODE", "iam")
	if !useIAMDatabaseAuth() {
		t.Fatal("DATABASE_AUTH_MODE=iam should enable IAM auth")
	}

	t.Setenv("DATABASE_AUTH_MODE", "password")
	if useIAMDatabaseAuth() {
		t.Fatal("DATABASE_AUTH_MODE=password should disable IAM auth")
	}
}

func TestNeonDatabaseUsesExecQueryMode(t *testing.T) {
	t.Setenv("DATABASE_PROVIDER", "neon")

	config, err := pgxpool.ParseConfig("postgres://user:password@example-pooler.us-east-1.aws.neon.tech/db?sslmode=require")
	if err != nil {
		t.Fatal(err)
	}

	configureStaticPasswordAuth(config)

	if config.ConnConfig.DefaultQueryExecMode != pgx.QueryExecModeExec {
		t.Fatalf("DefaultQueryExecMode = %v, want %v", config.ConnConfig.DefaultQueryExecMode, pgx.QueryExecModeExec)
	}
}

func TestNonNeonDatabaseKeepsDefaultQueryMode(t *testing.T) {
	t.Setenv("DATABASE_PROVIDER", "rds")

	config, err := pgxpool.ParseConfig("postgres://user:password@example.us-east-1.rds.amazonaws.com/db?sslmode=require")
	if err != nil {
		t.Fatal(err)
	}
	original := config.ConnConfig.DefaultQueryExecMode

	configureStaticPasswordAuth(config)

	if config.ConnConfig.DefaultQueryExecMode != original {
		t.Fatalf("DefaultQueryExecMode = %v, want unchanged %v", config.ConnConfig.DefaultQueryExecMode, original)
	}
}

func TestJSONBParamUsesTextEncoding(t *testing.T) {
	value := jsonbParam([]byte(`{"id":"job-1"}`))
	if value != `{"id":"job-1"}` {
		t.Fatalf("jsonbParam() = %q", value)
	}
}
