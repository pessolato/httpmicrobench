package osutil

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

// valPtr is a type constraint for pointers to string, int, or bool.
// It is used to ensure type safety when passing pointers to EnvVar.
type valPtr interface {
	*string | *int | *bool
}

// EnvVar represents an environment variable to be loaded.
// It contains the variable's name, a pointer to its value, and whether it is required.
type EnvVar struct {
	name     string // The name of the environment variable.
	value    any    // A pointer to the variable's value.
	required bool   // Whether the variable is required.
}

// NewEnvVar creates an [EnvVar] instance for the given environment variable.
//   - name: the name of the environment variable.
//   - varP: a pointer to the variable where the value will be stored.
//   - required: whether the variable is required.
//
// Panics if varP is nil.
func NewEnvVar[T valPtr](name string, varP T, required bool) EnvVar {
	if varP == nil {
		panic(fmt.Sprintf("variable pointer for var %s must not be nil", name))
	}
	return EnvVar{
		name:     name,
		value:    varP,
		required: required,
	}
}

// Load loads the values of the provided environment variables into their respective pointers.
// Accepts a variadic list of Var.
// Returns an error if any required variable is missing or if a value cannot be converted to the expected type.
func Load(vars ...EnvVar) error {
	var errs error
	for _, ev := range vars {
		v := os.Getenv(ev.name)
		if v == "" {
			if ev.required {
				errs = errors.Join(fmt.Errorf("missing required variable %s", ev.name), errs)
			}
			continue
		}

		switch typed := ev.value.(type) {
		case *string:
			*typed = v
		case *int:
			cov, err := strconv.Atoi(v)
			if err != nil {
				errs = errors.Join(fmt.Errorf("unable to convert %s to type int", v), errs)
				continue
			}
			*typed = cov
		case *bool:
			cov, err := strconv.ParseBool(v)
			if err != nil {
				errs = errors.Join(fmt.Errorf("unable to convert %s to type bool", v), errs)
				continue
			}
			*typed = cov
		default:
			errs = errors.Join(fmt.Errorf("unrecognized env var type %T", ev.value), errs)
		}
	}
	return errs
}
