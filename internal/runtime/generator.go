package runtime

import (
	"fmt"
	randv2 "math/rand/v2"

	faker "github.com/euskadi31/go-faker"
	"github.com/zclconf/go-cty/cty"
)

func runGenerator(generatorType string, params map[string]cty.Value, rnd generatorRandom) (cty.Value, error) {
	switch generatorType {
	case "email":
		email, err := runEmailGenerator(params, rnd)
		if err != nil {
			return cty.NilVal, err
		}

		return cty.StringVal(email), nil
	case "password":
		password, err := runPasswordGenerator(params, rnd)
		if err != nil {
			return cty.NilVal, err
		}

		return cty.StringVal(password), nil
	default:
		return cty.NilVal, fmt.Errorf("generator type %q is not supported", generatorType)
	}
}

type generatorRandom struct {
	fakerRand *randv2.Rand
}

func newGeneratorRandom(globalSeed int64, parts ...string) generatorRandom {
	return generatorRandom{fakerRand: NewDeterministicRandV2(globalSeed, parts...)}
}

func (r generatorRandom) faker() *faker.Faker {
	return faker.New(faker.WithRand(r.fakerRand))
}

func runEmailGenerator(params map[string]cty.Value, rnd generatorRandom) (string, error) {
	opts := make([]faker.EmailOption, 0, 2)

	if prefix, ok, err := optionalGeneratorStringParam(params, "email", "prefix"); err != nil {
		return "", err
	} else if ok {
		opts = append(opts, faker.WithEmailPrefix(prefix))
	}

	if domain, ok, err := optionalGeneratorStringParam(params, "email", "domain"); err != nil {
		return "", err
	} else if ok {
		opts = append(opts, faker.WithEmailDomain(domain))
	}

	return rnd.faker().Email(opts...), nil
}

func runPasswordGenerator(params map[string]cty.Value, rnd generatorRandom) (string, error) {
	opts, err := passwordGeneratorOptionsFromParams(params)
	if err != nil {
		return "", err
	}

	password, err := rnd.faker().Password(opts)
	if err != nil {
		return "", fmt.Errorf("generate password with faker: %w", err)
	}

	return password, nil
}

func passwordGeneratorOptionsFromParams(params map[string]cty.Value) (faker.PasswordOptions, error) {
	opts := faker.DefaultPasswordOptions()

	var err error
	if opts.Length, err = optionalGeneratorIntParam(params, "password", "length", opts.Length); err != nil {
		return opts, err
	}

	if opts.MinUpper, err = optionalGeneratorIntParam(params, "password", "min_upper", opts.MinUpper); err != nil {
		return opts, err
	}

	if opts.MinLower, err = optionalGeneratorIntParam(params, "password", "min_lower", opts.MinLower); err != nil {
		return opts, err
	}

	if opts.MinDigit, err = optionalGeneratorIntParam(params, "password", "min_digit", opts.MinDigit); err != nil {
		return opts, err
	}

	if opts.MinSpecial, err = optionalGeneratorIntParam(params, "password", "min_special", opts.MinSpecial); err != nil {
		return opts, err
	}

	if opts.Specials, err = optionalGeneratorStringParamWithFallback(params, "password", "specials", opts.Specials); err != nil {
		return opts, err
	}

	if err := opts.Validate(); err != nil {
		return opts, fmt.Errorf("validate password generator options: %w", err)
	}

	return opts, nil
}

func optionalGeneratorIntParam(params map[string]cty.Value, generatorType string, name string, fallback int) (int, error) {
	value, ok := params[name]
	if !ok || value.IsNull() {
		return fallback, nil
	}

	if value.Type() != cty.Number {
		return 0, fmt.Errorf("%s generator %s must be a number", generatorType, name)
	}

	parsed, accuracy := value.AsBigFloat().Int64()
	if accuracy != 0 {
		return 0, fmt.Errorf("%s generator %s must be an integer", generatorType, name)
	}

	return int(parsed), nil
}

func optionalGeneratorStringParamWithFallback(params map[string]cty.Value, generatorType string, name string, fallback string) (string, error) {
	value, ok, err := optionalGeneratorStringParam(params, generatorType, name)
	if err != nil {
		return "", err
	}

	if !ok {
		return fallback, nil
	}

	return value, nil
}

func optionalGeneratorStringParam(params map[string]cty.Value, generatorType string, name string) (string, bool, error) {
	value, ok := params[name]
	if !ok || value.IsNull() {
		return "", false, nil
	}

	if value.Type() != cty.String {
		return "", false, fmt.Errorf("%s generator %s must be a string", generatorType, name)
	}

	return value.AsString(), true, nil
}
