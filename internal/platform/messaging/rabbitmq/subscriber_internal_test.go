package rabbitmq

import (
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

func delivery(headers amqp.Table) amqp.Delivery {
	return amqp.Delivery{
		MessageId:     uuid.NewString(),
		Type:          "basket.checkout-requested",
		CorrelationId: "trace-1",
		Timestamp:     time.Now(),
		Body:          []byte(`{}`),
		Headers:       headers,
	}
}

// The schema version arrives as whatever integer type the publishing library
// chose. Accepting only our own encoding rejected well-formed messages from
// anything else - including RabbitMQ's own management API, which sends a
// 64-bit integer.
func TestTheSchemaVersionIsReadWhateverIntegerTypeItArrivesAs(t *testing.T) {
	t.Parallel()

	for name, value := range map[string]any{
		"int":     int(2),
		"int32":   int32(2),
		"int64":   int64(2),
		"float64": float64(2),
		"string":  "2",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			envelope, err := envelopeFrom(delivery(amqp.Table{
				"schema-version": value,
				"aggregate-id":   "an-aggregate",
			}))
			if err != nil {
				t.Fatalf("reading the delivery: %v", err)
			}
			if envelope.Contract.Version != 2 {
				t.Errorf("version = %d, want 2", envelope.Contract.Version)
			}
		})
	}
}

func TestAMessageWithoutAVersionIsRefused(t *testing.T) {
	t.Parallel()

	// Without it there is no way to tell which shape the payload has, and
	// guessing would be worse than refusing.
	if _, err := envelopeFrom(delivery(amqp.Table{"aggregate-id": "an-aggregate"})); err == nil {
		t.Error("a message with no schema version should be refused")
	}
	if _, err := envelopeFrom(delivery(amqp.Table{"schema-version": "not a number"})); err == nil {
		t.Error("a message whose version is not a number should be refused")
	}
}
