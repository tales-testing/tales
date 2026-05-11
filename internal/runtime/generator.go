package runtime

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	randv2 "math/rand/v2"
	"strings"

	faker "github.com/euskadi31/go-faker"
	"github.com/zclconf/go-cty/cty"
)

const defaultBytesLength = 16

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
	case "timezone":
		return cty.StringVal(rnd.faker().Timezone()), nil
	case "locale":
		locale, err := runLocaleGenerator(params, rnd)
		if err != nil {
			return cty.NilVal, err
		}

		return cty.StringVal(locale), nil
	case "person":
		person, err := runPersonGenerator(params, rnd)
		if err != nil {
			return cty.NilVal, err
		}

		return cty.ObjectVal(map[string]cty.Value{
			"first_name": cty.StringVal(person.FirstName),
			"last_name":  cty.StringVal(person.LastName),
			"gender":     cty.StringVal(person.Gender),
			"name":       cty.StringVal(person.String()),
		}), nil
	case "mac_address":
		macAddress, err := runMACAddressGenerator(params, rnd)
		if err != nil {
			return cty.NilVal, err
		}

		return cty.StringVal(macAddress), nil
	case "bytes":
		bytesValue, err := runBytesGenerator(params, rnd)
		if err != nil {
			return cty.NilVal, err
		}

		return cty.StringVal(bytesValue), nil
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

func runLocaleGenerator(params map[string]cty.Value, rnd generatorRandom) (string, error) {
	opts := make([]faker.LocaleOption, 0, 3)

	if language, ok, err := optionalGeneratorStringParam(params, "locale", "language"); err != nil {
		return "", err
	} else if ok {
		opts = append(opts, faker.WithLocaleLanguage(language))
	}

	if country, ok, err := optionalGeneratorStringParam(params, "locale", "country"); err != nil {
		return "", err
	} else if ok {
		opts = append(opts, faker.WithLocaleCountry(country))
	}

	if separator, ok, err := optionalGeneratorStringParam(params, "locale", "separator"); err != nil {
		return "", err
	} else if ok {
		opts = append(opts, faker.WithLocaleSeparator(separator))
	}

	return rnd.faker().Locale(opts...), nil
}

func runPersonGenerator(params map[string]cty.Value, rnd generatorRandom) (faker.PersonInfo, error) {
	opts := make([]faker.PersonOption, 0, 1)

	if gender, ok, err := optionalGeneratorStringParam(params, "person", "gender"); err != nil {
		return faker.PersonInfo{}, err
	} else if ok {
		parsedGender, err := parseGender(gender)
		if err != nil {
			return faker.PersonInfo{}, err
		}

		opts = append(opts, faker.WithGender(parsedGender))
	}

	return rnd.faker().Person(opts...), nil
}

func runMACAddressGenerator(params map[string]cty.Value, rnd generatorRandom) (string, error) {
	opts := make([]faker.MACAddressOption, 0, 3)

	if prefix, ok, err := optionalGeneratorStringParam(params, "mac_address", "prefix"); err != nil {
		return "", err
	} else if ok {
		opts = append(opts, faker.WithMACPrefix(prefix))
	}

	if separator, ok, err := optionalGeneratorStringParam(params, "mac_address", "separator"); err != nil {
		return "", err
	} else if ok {
		opts = append(opts, faker.WithMACSeparator(separator))
	}

	lowercase, err := optionalGeneratorBoolParam(params, "mac_address", "lowercase", false)
	if err != nil {
		return "", err
	}

	uppercase, err := optionalGeneratorBoolParam(params, "mac_address", "uppercase", false)
	if err != nil {
		return "", err
	}

	switch {
	case lowercase:
		opts = append(opts, faker.WithMACLowercase())
	case uppercase:
		opts = append(opts, faker.WithMACUppercase())
	}

	return rnd.faker().MACAddress(opts...), nil
}

func runBytesGenerator(params map[string]cty.Value, rnd generatorRandom) (string, error) {
	length, err := optionalGeneratorIntParam(params, "bytes", "length", defaultBytesLength)
	if err != nil {
		return "", err
	}

	if length < 0 {
		return "", fmt.Errorf("bytes generator length must be greater than or equal to 0")
	}

	encoding, err := optionalGeneratorStringParamWithFallback(params, "bytes", "encoding", "hex")
	if err != nil {
		return "", err
	}

	bytesValue := rnd.faker().Bytes(length)

	switch strings.ToLower(encoding) {
	case "hex":
		return hex.EncodeToString(bytesValue), nil
	case "base64":
		return base64.StdEncoding.EncodeToString(bytesValue), nil
	default:
		return "", fmt.Errorf("bytes generator encoding must be one of: hex, base64")
	}
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

func optionalGeneratorBoolParam(params map[string]cty.Value, generatorType string, name string, fallback bool) (bool, error) {
	value, ok := params[name]
	if !ok || value.IsNull() {
		return fallback, nil
	}

	if value.Type() != cty.Bool {
		return false, fmt.Errorf("%s generator %s must be a bool", generatorType, name)
	}

	return value.True(), nil
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

func parseGender(value string) (faker.Gender, error) {
	switch strings.ToLower(value) {
	case "", "any":
		return faker.GenderAny, nil
	case "male", "man":
		return faker.GenderMale, nil
	case "female", "woman":
		return faker.GenderFemale, nil
	default:
		return faker.GenderAny, fmt.Errorf("person generator gender must be one of: any, male, female")
	}
}
