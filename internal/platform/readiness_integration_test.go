package platform_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/johnvictor01/challenge-jungle-gaming/internal/adapters/postgres"
	sqsadapter "github.com/johnvictor01/challenge-jungle-gaming/internal/adapters/sqs"
	"github.com/johnvictor01/challenge-jungle-gaming/internal/platform"
)

func TestDependenciesReadinessAgainstPostgresAndLocalStack(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	endpoint := os.Getenv("TEST_SQS_ENDPOINT")
	inputQueue := os.Getenv("TEST_SQS_INPUT_QUEUE_URL")
	outputQueue := os.Getenv("TEST_SQS_QUEUE_URL")
	if outputQueue == "" {
		outputQueue = os.Getenv("SQS_QUEUE_URL")
	}
	if databaseURL == "" || endpoint == "" || inputQueue == "" || outputQueue == "" {
		t.Skip("set PostgreSQL and LocalStack integration variables to verify combined readiness")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := postgres.NewPool(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	client, err := sqsadapter.NewAWSClient(ctx, "us-east-1", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	checker := platform.DependenciesReadiness{Database: store, SQS: client, QueueURLs: []string{inputQueue, outputQueue}}
	if err := checker.Ping(ctx); err != nil {
		t.Fatalf("combined dependency readiness failed: %v", err)
	}
}
