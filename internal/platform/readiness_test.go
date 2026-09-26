package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type fakePinger struct{ err error }

func (p fakePinger) Ping(context.Context) error { return p.err }

type fakeQueueReader struct {
	urls []string
	err  error
}

func (f *fakeQueueReader) GetQueueAttributes(_ context.Context, input *sqs.GetQueueAttributesInput, _ ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error) {
	f.urls = append(f.urls, *input.QueueUrl)
	return &sqs.GetQueueAttributesOutput{}, f.err
}

func TestDependenciesReadinessChecksDatabaseAndEveryQueue(t *testing.T) {
	reader := &fakeQueueReader{}
	checker := DependenciesReadiness{Database: fakePinger{}, SQS: reader, QueueURLs: []string{"input.fifo", "outbox.fifo"}}
	if err := checker.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(reader.urls) != 2 || reader.urls[0] != "input.fifo" || reader.urls[1] != "outbox.fifo" {
		t.Fatalf("checked queue URLs = %v", reader.urls)
	}
}

func TestDependenciesReadinessReturnsUnavailableDependency(t *testing.T) {
	want := errors.New("SQS unavailable")
	checker := DependenciesReadiness{Database: fakePinger{}, SQS: &fakeQueueReader{err: want}, QueueURLs: []string{"input.fifo"}}
	if err := checker.Ping(context.Background()); !errors.Is(err, want) {
		t.Fatalf("readiness error = %v, want %v", err, want)
	}
}
