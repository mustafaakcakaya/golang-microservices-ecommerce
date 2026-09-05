// Package validation checks command input before a handler runs.
//
// Rules live in struct tags and a cqrs middleware turns failures into
// apperr.Validation, so the transport layer reports them as problem details
// with a field-by-field breakdown rather than a single opaque message.
package validation

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"
	"github.com/shopspring/decimal"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
)

var (
	once     sync.Once
	validate *validator.Validate
)

func instance() *validator.Validate {
	once.Do(func() {
		validate = validator.New(validator.WithRequiredStructEnabled())

		// decimal.Decimal is a struct, so numeric rules such as gt=0 would
		// otherwise not apply to it. Exposing it as a float lets money fields
		// use the same tags as any other number.
		validate.RegisterCustomTypeFunc(func(field reflect.Value) any {
			if value, ok := field.Interface().(decimal.Decimal); ok {
				result, _ := value.Float64()
				return result
			}
			return nil
		}, decimal.Decimal{})
	})

	return validate
}

// Struct validates v and reports failures as an apperr.Validation error whose
// Fields map is keyed by field name. It returns nil when v is valid.
func Struct(v any) error {
	err := instance().Struct(v)
	if err == nil {
		return nil
	}

	var invalid *validator.InvalidValidationError
	if errors.As(err, &invalid) {
		// A misuse of the validator, not user input.
		return apperr.Internal(err, "validating request")
	}

	var validationErrors validator.ValidationErrors
	if !errors.As(err, &validationErrors) {
		return apperr.Internal(err, "validating request")
	}

	fields := make(map[string]string, len(validationErrors))
	for _, fieldErr := range validationErrors {
		fields[fieldErr.Field()] = message(fieldErr)
	}

	return apperr.Validation(fields)
}

// message renders a rule failure in prose ("Name is required") rather than
// exposing raw tag names to the caller.
func message(err validator.FieldError) string {
	name := err.Field()

	switch err.Tag() {
	case "required":
		return fmt.Sprintf("%s is required", name)
	case "min":
		return fmt.Sprintf("%s must be at least %s characters", name, err.Param())
	case "max":
		return fmt.Sprintf("%s must be at most %s characters", name, err.Param())
	case "gt":
		return fmt.Sprintf("%s must be greater than %s", name, err.Param())
	case "gte":
		return fmt.Sprintf("%s must be %s or greater", name, err.Param())
	case "uuid", "uuid4":
		return fmt.Sprintf("%s must be a UUID", name)
	default:
		return fmt.Sprintf("%s failed the %s rule", name, strings.ToLower(err.Tag()))
	}
}
