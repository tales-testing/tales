package runtime

import (
	"regexp"
	"testing"

	"github.com/zclconf/go-cty/cty"
)

func TestSIRENGeneratorUsesDeterministicFaker(t *testing.T) {
	t.Parallel()

	assertDeterministicStringGenerator(t, "siren", nil, regexp.MustCompile(`^\d{9}$`))

	value, err := runGenerator("siren", nil, newGeneratorRandom(1234, "scenario", "step", "request.body.json", "siren"))
	if err != nil {
		t.Fatalf("generate siren: %v", err)
	}

	if !isLuhnValid(value.AsString()) {
		t.Fatalf("siren %q must be Luhn-valid", value.AsString())
	}
}

func TestSIRETGeneratorUsesDeterministicFaker(t *testing.T) {
	t.Parallel()

	assertDeterministicStringGenerator(t, "siret", nil, regexp.MustCompile(`^\d{14}$`))

	value, err := runGenerator("siret", nil, newGeneratorRandom(1234, "scenario", "step", "request.body.json", "siret"))
	if err != nil {
		t.Fatalf("generate siret: %v", err)
	}

	siret := value.AsString()

	if !isLuhnValid(siret) {
		t.Fatalf("siret %q must be Luhn-valid", siret)
	}

	siren := siret[:9]
	if !isLuhnValid(siren) {
		t.Fatalf("siret %q must embed a Luhn-valid 9-digit siren prefix, got %q", siret, siren)
	}
}

func TestEINGeneratorUsesDeterministicFaker(t *testing.T) {
	t.Parallel()

	assertDeterministicStringGenerator(t, "ein", nil, regexp.MustCompile(`^\d{2}-\d{7}$`))
}

func TestDUNSGeneratorUsesDeterministicFaker(t *testing.T) {
	t.Parallel()

	assertDeterministicStringGenerator(t, "duns", nil, regexp.MustCompile(`^\d{9}$`))
}

func TestLEIGeneratorUsesDeterministicFaker(t *testing.T) {
	t.Parallel()

	assertDeterministicStringGenerator(t, "lei", nil, regexp.MustCompile(`^[A-Z0-9]{20}$`))
}

func TestVATNumberGeneratorUsesDeterministicFaker(t *testing.T) {
	t.Parallel()

	assertDeterministicStringGenerator(t, "vat_number", nil, regexp.MustCompile(`^[A-Z]{2}.+$`))

	params := map[string]cty.Value{"country": cty.StringVal("FR")}
	assertDeterministicStringGenerator(t, "vat_number", params, regexp.MustCompile(`^FR\d{11}$`))
}

func TestVATNumberGeneratorRejectsNonStringCountry(t *testing.T) {
	t.Parallel()

	params := map[string]cty.Value{"country": cty.NumberIntVal(33)}

	_, err := runGenerator("vat_number", params, newGeneratorRandom(1234, "scenario", "step", "request.body.json", "vat"))
	if err == nil || err.Error() != "vat_number generator country must be a string" {
		t.Fatalf("expected vat_number country type error, got %v", err)
	}
}

func TestEUIDGeneratorUsesDeterministicFaker(t *testing.T) {
	t.Parallel()

	assertDeterministicStringGenerator(t, "euid", nil, regexp.MustCompile(`^[A-Z]{2}\.[A-Z0-9]+\.\d+\.\d{2}$`))

	params := map[string]cty.Value{"country": cty.StringVal("FR")}
	assertDeterministicStringGenerator(t, "euid", params, regexp.MustCompile(`^FR\.[A-Z0-9]+\.\d+\.\d{2}$`))
}

func TestEUIDGeneratorRejectsNonStringCountry(t *testing.T) {
	t.Parallel()

	params := map[string]cty.Value{"country": cty.NumberIntVal(33)}

	_, err := runGenerator("euid", params, newGeneratorRandom(1234, "scenario", "step", "request.body.json", "euid"))
	if err == nil || err.Error() != "euid generator country must be a string" {
		t.Fatalf("expected euid country type error, got %v", err)
	}
}

// assertDeterministicStringGenerator runs the generator twice with the same
// seed, once with a different seed, and asserts the output matches the
// expected regex. It mirrors the pattern used by the per-generator tests in
// generator_test.go but factored to keep the enterprise suite compact.
func assertDeterministicStringGenerator(t *testing.T, generatorType string, params map[string]cty.Value, want *regexp.Regexp) {
	t.Helper()

	parts := []string{"scenario", "step", "request.body.json", generatorType}

	first, err := runGenerator(generatorType, params, newGeneratorRandom(1234, parts...))
	if err != nil {
		t.Fatalf("generate first %s: %v", generatorType, err)
	}

	second, err := runGenerator(generatorType, params, newGeneratorRandom(1234, parts...))
	if err != nil {
		t.Fatalf("generate second %s: %v", generatorType, err)
	}

	otherSeed, err := runGenerator(generatorType, params, newGeneratorRandom(5678, parts...))
	if err != nil {
		t.Fatalf("generate other-seed %s: %v", generatorType, err)
	}

	if first.AsString() != second.AsString() {
		t.Fatalf("same seed generated different %s values: %q vs %q", generatorType, first.AsString(), second.AsString())
	}

	if first.AsString() == otherSeed.AsString() {
		t.Fatalf("different seed generated same %s value: %q", generatorType, first.AsString())
	}

	if !want.MatchString(first.AsString()) {
		t.Fatalf("%s value %q does not match expected pattern %s", generatorType, first.AsString(), want)
	}
}

// isLuhnValid reports whether the all-digit input passes the Luhn checksum.
// Used to pin the SIREN/SIRET contract Tales relies on for assertable test
// data; a local implementation avoids any dependency on go-faker internals.
func isLuhnValid(digits string) bool {
	if len(digits) == 0 {
		return false
	}

	sum := 0
	n := len(digits)

	for i := range n {
		c := digits[i]
		if c < '0' || c > '9' {
			return false
		}

		d := int(c - '0')
		if (n-i)%2 == 0 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}

		sum += d
	}

	return sum%10 == 0
}
