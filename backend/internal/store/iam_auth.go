package store

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newPostgresPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	if useIAMDatabaseAuth() {
		if err := configureRDSIAMAuth(ctx, poolConfig); err != nil {
			return nil, err
		}
	} else {
		configureStaticPasswordAuth(poolConfig)
	}
	return pgxpool.NewWithConfig(ctx, poolConfig)
}

func configureRDSIAMAuth(ctx context.Context, poolConfig *pgxpool.Config) error {
	awsConfig, err := awscfg.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config for rds iam auth: %w", err)
	}
	region := awsConfig.Region
	if value := strings.TrimSpace(os.Getenv("AWS_REGION")); value != "" {
		region = value
	}
	if region == "" {
		return fmt.Errorf("AWS_REGION is required for rds iam auth")
	}

	buildAuthToken := func(ctx context.Context, connConfig *pgx.ConnConfig) (string, error) {
		port := connConfig.Port
		if port == 0 {
			port = 5432
		}
		endpoint := net.JoinHostPort(connConfig.Host, strconv.Itoa(int(port)))
		token, err := auth.BuildAuthToken(ctx, endpoint, region, connConfig.User, awsConfig.Credentials)
		if err != nil {
			return "", fmt.Errorf("build rds iam auth token: %w", err)
		}
		return normalizeRDSAuthToken(token), nil
	}

	poolConfig.ConnConfig.Password, err = buildAuthToken(ctx, poolConfig.ConnConfig)
	if err != nil {
		return err
	}

	poolConfig.BeforeConnect = func(ctx context.Context, connConfig *pgx.ConnConfig) error {
		token, err := buildAuthToken(ctx, connConfig)
		if err != nil {
			return err
		}
		connConfig.Password = token
		return nil
	}
	return nil
}

func configureStaticPasswordAuth(poolConfig *pgxpool.Config) {
	if usesNeonDatabase() {
		poolConfig.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
	}
}

func normalizeRDSAuthToken(token string) string {
	queryIndex := strings.IndexByte(token, '?')
	if queryIndex > 0 && token[queryIndex-1] != '/' {
		return token[:queryIndex] + "/" + token[queryIndex:]
	}
	return token
}

func useIAMDatabaseAuth() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DATABASE_AUTH_MODE"))) {
	case "iam", "rds_iam", "rds-iam":
		return true
	case "password", "postgres", "postgresql":
		return false
	}
	value := strings.ToLower(strings.TrimSpace(os.Getenv("DATABASE_IAM_AUTH")))
	return value == "1" || value == "true" || value == "yes"
}

func usesNeonDatabase() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DATABASE_PROVIDER"))) {
	case "neon", "neon-postgres", "neonpostgres":
		return true
	}
	return false
}
