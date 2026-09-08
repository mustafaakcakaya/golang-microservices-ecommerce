// Package broker chooses a publisher from configuration.
//
// Which broker a service talks to is an operational decision, not a code one:
// the publish path works against messaging.Publisher, so moving between
// RabbitMQ and Kafka is a change of environment variables. This package is the
// one place that knows both exist.
package broker

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/kafka"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/messaging/rabbitmq"
)

// Provider names a supported broker.
type Provider string

// The providers a service can be pointed at.
const (
	ProviderRabbitMQ Provider = "rabbitmq"
	ProviderKafka    Provider = "kafka"
)

// Config selects and configures a broker.
type Config struct {
	Provider Provider
	RabbitMQ rabbitmq.Config
	Kafka    kafka.Config
}

// ConfigFromEnv reads the broker settings.
//
// Addresses and credentials come from the environment because they differ per
// deployment and, in RabbitMQ's case, the URL carries a password - which must
// not be in the source.
func ConfigFromEnv() (Config, error) {
	provider := Provider(strings.ToLower(strings.TrimSpace(os.Getenv("MESSAGE_BROKER_PROVIDER"))))
	if provider == "" {
		provider = ProviderRabbitMQ
	}

	config := Config{
		Provider: provider,
		RabbitMQ: rabbitmq.Config{
			URL:      os.Getenv("MESSAGE_BROKER_RABBITMQ_URL"),
			Exchange: os.Getenv("MESSAGE_BROKER_RABBITMQ_EXCHANGE"),
		},
		Kafka: kafka.Config{
			Brokers:     splitBrokers(os.Getenv("MESSAGE_BROKER_KAFKA_BROKERS")),
			TopicPrefix: os.Getenv("MESSAGE_BROKER_KAFKA_TOPIC_PREFIX"),
		},
	}

	if raw := os.Getenv("MESSAGE_BROKER_KAFKA_WRITE_TIMEOUT"); raw != "" {
		timeout, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("MESSAGE_BROKER_KAFKA_WRITE_TIMEOUT is not a duration: %w", err)
		}
		config.Kafka.WriteTimeout = timeout
	}

	if err := config.validate(); err != nil {
		return Config{}, err
	}

	return config, nil
}

// New builds the publisher the configuration selects.
func New(config Config, registry *messaging.Registry) (messaging.Publisher, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}

	switch config.Provider {
	case ProviderRabbitMQ:
		return rabbitmq.New(config.RabbitMQ, registry), nil
	case ProviderKafka:
		return kafka.New(config.Kafka, registry), nil
	default:
		return nil, unknownProvider(config.Provider)
	}
}

// validate refuses a configuration that cannot work, at startup rather than on
// the first message.
func (c Config) validate() error {
	switch c.Provider {
	case ProviderRabbitMQ:
		if c.RabbitMQ.URL == "" {
			return errors.New("MESSAGE_BROKER_RABBITMQ_URL is required when the provider is rabbitmq")
		}
	case ProviderKafka:
		if len(c.Kafka.Brokers) == 0 {
			return errors.New("MESSAGE_BROKER_KAFKA_BROKERS is required when the provider is kafka")
		}
	default:
		return unknownProvider(c.Provider)
	}

	return nil
}

func unknownProvider(provider Provider) error {
	return fmt.Errorf("unknown message broker provider %q; supported values are %q and %q",
		provider, ProviderRabbitMQ, ProviderKafka)
}

func splitBrokers(raw string) []string {
	var brokers []string
	for _, address := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(address); trimmed != "" {
			brokers = append(brokers, trimmed)
		}
	}

	return brokers
}
