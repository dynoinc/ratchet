package internal

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func TestDBConnection(dbURL string) error {
	conn, err := pgx.Connect(context.Background(), dbURL)
	if err != nil {
		return fmt.Errorf("unable to connect to database: %v", err)
	}
	defer conn.Close(context.Background())

	var result int
	err = conn.QueryRow(context.Background(), "SELECT 1").Scan(&result)
	if err != nil {
		return fmt.Errorf("query failed: %v", err)
	}

	if result != 1 {
		return fmt.Errorf("unexpected result: %d", result)
	}

	return nil
}
