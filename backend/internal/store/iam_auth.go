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
	if !useIAMDatabaseAuth() {
		return pgxpool.New(ctx, databaseURL)
	}

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}

	awsConfig, err := awscfg.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load aws config for rds iam auth: %w", err)
	}
	region := awsConfig.Region
	if value := strings.TrimSpace(os.Getenv("AWS_REGION")); value != "" {
		region = value
	}
	if region == "" {
		return nil, fmt.Errorf("AWS_REGION is required for rds iam auth")
	}

	poolConfig.BeforeConnect = func(ctx context.Context, connConfig *pgx.ConnConfig) error {
		port := connConfig.Port
		if port == 0 {
			port = 5432
		}
		endpoint := net.JoinHostPort(connConfig.Host, strconv.Itoa(int(port)))
		token, err := auth.BuildAuthToken(ctx, endpoint, region, connConfig.User, awsConfig.Credentials)
		if err != nil {
			return fmt.Errorf("build rds iam auth token: %w", err)
		}
		connConfig.Password = token
		return nil
	}

	return pgxpool.NewWithConfig(ctx, poolConfig)
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
