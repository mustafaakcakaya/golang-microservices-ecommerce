package broker_test

import (
	"testing"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/broker"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/kafka"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/rabbitmq"
)

func TestTheProviderDefaultsToRabbitMQ(t *testing.T) {
	t.Setenv("MESSAGE_BROKER_RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")

	config, err := broker.ConfigFromEnv()
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}

	if config.Provider != broker.ProviderRabbitMQ {
		t.Errorf("provider = %q, want rabbitmq", config.Provider)
	}
}

func TestKafkaBrokersAreReadAsAList(t *testing.T) {
	t.Setenv("MESSAGE_BROKER_PROVIDER", "kafka")
	t.Setenv("MESSAGE_BROKER_KAFKA_BROKERS", "one:9092, two:9092 ,")

	config, err := broker.ConfigFromEnv()
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}

	if len(config.Kafka.Brokers) != 2 || config.Kafka.Brokers[1] != "two:9092" {
		t.Errorf("brokers = %q, want the two addresses without blanks", config.Kafka.Brokers)
	}
}

func TestAnIncompleteConfigurationFailsAtStartup(t *testing.T) {
	cases := map[string]map[string]string{
		"rabbitmq without a url":  {"MESSAGE_BROKER_PROVIDER": "rabbitmq"},
		"kafka without brokers":   {"MESSAGE_BROKER_PROVIDER": "kafka"},
		"a provider that is typo": {"MESSAGE_BROKER_PROVIDER": "rabbit"},
	}

	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("MESSAGE_BROKER_RABBITMQ_URL", "")
			t.Setenv("MESSAGE_BROKER_KAFKA_BROKERS", "")
			for key, value := range env {
				t.Setenv(key, value)
			}

			// Better here than on the first message, hours after a deploy.
			if _, err := broker.ConfigFromEnv(); err == nil {
				t.Fatal("an unusable configuration should be refused at startup")
			}
		})
	}
}

func TestTheProviderDecidesWhichPublisherIsBuilt(t *testing.T) {
	registry := messaging.NewRegistry()

	cases := []struct {
		provider broker.Provider
		config   broker.Config
	}{
		{
			broker.ProviderRabbitMQ,
			broker.Config{Provider: broker.ProviderRabbitMQ, RabbitMQ: rabbitmq.Config{URL: "amqp://localhost"}},
		},
		{
			broker.ProviderKafka,
			broker.Config{Provider: broker.ProviderKafka, Kafka: kafka.Config{Brokers: []string{"localhost:9092"}}},
		},
	}

	for _, tc := range cases {
		t.Run(string(tc.provider), func(t *testing.T) {
			publisher, err := broker.New(tc.config, registry)
			if err != nil {
				t.Fatalf("building publisher: %v", err)
			}
			t.Cleanup(func() { _ = publisher.Close() })

			// Nothing is dialled here: the publish path is identical either
			// way, which is what makes the choice a matter of configuration.
			if got := providerOf(publisher); got != string(tc.provider) {
				t.Errorf("publisher = %s, want %s", got, tc.provider)
			}
		})
	}
}

func providerOf(publisher messaging.Publisher) string {
	switch publisher.(type) {
	case *rabbitmq.Publisher:
		return "rabbitmq"
	case *kafka.Publisher:
		return "kafka"
	default:
		return "unknown"
	}
}
