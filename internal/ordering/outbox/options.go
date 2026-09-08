package outbox

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Options tune one reader.
type Options struct {
	// ConsumerName owns the checkpoint.
	ConsumerName string

	// BatchSize is the most messages read per cycle.
	BatchSize int

	// PollInterval is how long to wait when there was nothing to do.
	PollInterval time.Duration

	// MaxPublishAttempts is how often one message is retried before it is
	// given up on and the stream moves past it.
	MaxPublishAttempts int

	// RetryBaseDelay and RetryMaxDelay bound the backoff after a failed cycle.
	RetryBaseDelay time.Duration
	RetryMaxDelay  time.Duration
}

// DefaultOptions are what the worker runs with unless the environment says
// otherwise.
func DefaultOptions() Options {
	return Options{
		ConsumerName:       DefaultConsumerName,
		BatchSize:          100,
		PollInterval:       5 * time.Second,
		MaxPublishAttempts: 10,
		RetryBaseDelay:     time.Second,
		RetryMaxDelay:      time.Minute,
	}
}

// OptionsFromEnv reads the tuning knobs, keeping the defaults for anything
// unset.
func OptionsFromEnv() (Options, error) {
	options := DefaultOptions()

	if name := os.Getenv("OUTBOX_CONSUMER_NAME"); name != "" {
		options.ConsumerName = name
	}

	for _, field := range []struct {
		name   string
		target *int
	}{
		{"OUTBOX_BATCH_SIZE", &options.BatchSize},
		{"OUTBOX_MAX_PUBLISH_ATTEMPTS", &options.MaxPublishAttempts},
	} {
		if err := readInt(field.name, field.target); err != nil {
			return Options{}, err
		}
	}

	for _, field := range []struct {
		name   string
		target *time.Duration
	}{
		{"OUTBOX_POLL_INTERVAL", &options.PollInterval},
		{"OUTBOX_RETRY_BASE_DELAY", &options.RetryBaseDelay},
		{"OUTBOX_RETRY_MAX_DELAY", &options.RetryMaxDelay},
	} {
		if err := readDuration(field.name, field.target); err != nil {
			return Options{}, err
		}
	}

	if options.BatchSize <= 0 {
		return Options{}, fmt.Errorf("OUTBOX_BATCH_SIZE must be positive, got %d", options.BatchSize)
	}
	if options.MaxPublishAttempts <= 0 {
		return Options{}, fmt.Errorf(
			"OUTBOX_MAX_PUBLISH_ATTEMPTS must be positive, got %d", options.MaxPublishAttempts)
	}

	return options, nil
}

func readInt(name string, target *int) error {
	raw := os.Getenv(name)
	if raw == "" {
		return nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("%s is not a number: %w", name, err)
	}
	*target = value

	return nil
}

func readDuration(name string, target *time.Duration) error {
	raw := os.Getenv(name)
	if raw == "" {
		return nil
	}

	value, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("%s is not a duration: %w", name, err)
	}
	*target = value

	return nil
}
