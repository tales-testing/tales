package sql

import (
	"fmt"

	"github.com/zclconf/go-cty/cty"
)

// ConnectionConfig is the resolved configuration for one SQL connection.
type ConnectionConfig struct {
	Name   string
	Driver string // alias supplied by the user (postgres / pgx / mysql)
	DSN    string
}

// resolveConnection extracts the connection configuration for a named entry
// under config.sql.connections.<name>. It returns descriptive errors when the
// connection is missing or fields are empty so users get an actionable
// diagnostic without leaking the DSN.
func resolveConnection(config map[string]cty.Value, name string) (ConnectionConfig, error) {
	if name == "" {
		return ConnectionConfig{}, fmt.Errorf("sql step requires a non-empty connection name")
	}

	connValue, err := lookupConnectionValue(config, name)
	if err != nil {
		return ConnectionConfig{}, err
	}

	driver, err := requiredString(connValue, "driver", name)
	if err != nil {
		return ConnectionConfig{}, err
	}

	dsn, err := requiredString(connValue, "dsn", name)
	if err != nil {
		return ConnectionConfig{}, err
	}

	return ConnectionConfig{Name: name, Driver: driver, DSN: dsn}, nil
}

// lookupConnectionValue navigates config.sql.connections.<name> and returns
// the entry, or a descriptive not-found error.
func lookupConnectionValue(config map[string]cty.Value, name string) (cty.Value, error) {
	sqlBlock, ok := config["sql"]
	if !ok || sqlBlock.IsNull() || !sqlBlock.IsKnown() {
		return cty.NilVal, fmt.Errorf("sql connection %q not found: config.sql is not defined", name)
	}

	if !sqlBlock.Type().IsObjectType() && !sqlBlock.Type().IsMapType() {
		return cty.NilVal, fmt.Errorf("sql connection %q not found: config.sql must be an object", name)
	}

	connsValue, err := indexValue(sqlBlock, "connections")
	if err != nil {
		return cty.NilVal, fmt.Errorf("sql connection %q not found: %w", name, err)
	}

	if connsValue.IsNull() || !connsValue.IsKnown() {
		return cty.NilVal, fmt.Errorf("sql connection %q not found: config.sql.connections is empty", name)
	}

	if !connsValue.Type().IsObjectType() && !connsValue.Type().IsMapType() {
		return cty.NilVal, fmt.Errorf("sql connection %q not found: config.sql.connections must be an object", name)
	}

	connValue, err := indexValue(connsValue, name)
	if err != nil {
		return cty.NilVal, fmt.Errorf("sql connection %q not found", name)
	}

	if connValue.IsNull() || !connValue.IsKnown() {
		return cty.NilVal, fmt.Errorf("sql connection %q not found", name)
	}

	return connValue, nil
}

// requiredString reads an attribute that must be a non-empty string. The
// error message uses the connection name rather than the attribute path so
// no DSN data leaks.
func requiredString(conn cty.Value, attr, connName string) (string, error) {
	value, err := indexValue(conn, attr)
	if err != nil {
		return "", fmt.Errorf("sql connection %q has empty %s", connName, attr)
	}

	str := stringFromCty(value)
	if str == "" {
		return "", fmt.Errorf("sql connection %q has empty %s", connName, attr)
	}

	return str, nil
}

// indexValue reads an attribute or map key from a cty value. It supports both
// object and map types so config consumers do not need to special-case the
// schema.
func indexValue(value cty.Value, key string) (cty.Value, error) {
	if !value.IsKnown() || value.IsNull() {
		return cty.NilVal, fmt.Errorf("missing key %q", key)
	}

	switch {
	case value.Type().IsObjectType():
		if !value.Type().HasAttribute(key) {
			return cty.NilVal, fmt.Errorf("missing attribute %q", key)
		}

		return value.GetAttr(key), nil
	case value.Type().IsMapType():
		index := cty.StringVal(key)
		if !value.HasIndex(index).True() {
			return cty.NilVal, fmt.Errorf("missing key %q", key)
		}

		return value.Index(index), nil
	default:
		return cty.NilVal, fmt.Errorf("unsupported config type %s", value.Type().FriendlyName())
	}
}

// stringFromCty returns the underlying string when value carries one, or an
// empty string otherwise (null, unknown, non-string).
func stringFromCty(value cty.Value) string {
	if value.IsNull() || !value.IsKnown() {
		return ""
	}

	if value.Type() != cty.String {
		return ""
	}

	return value.AsString()
}
