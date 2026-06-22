// Package rpc holds the dynamic ConnectRPC + gRPC provider implementation.
// The subpackages do the heavy lifting (descriptor / codec / transport);
// the files at this level expose the public Provider entry point plus the
// small shared helpers used across subpackages: status code normalization,
// config resolution, header / metadata masking, and TLS configuration.
package rpc

import (
	"fmt"
	"strings"
)

// Canonical lowercase snake_case status names matching the gRPC code set.
// These are the values Tales surfaces under response.status; the user writes
// expect { status = "ok" | "invalid_argument" | ... }.
const (
	StatusOK                 = "ok"
	StatusCancelled          = "cancelled"
	StatusUnknown            = "unknown"
	StatusInvalidArgument    = "invalid_argument"
	StatusDeadlineExceeded   = "deadline_exceeded"
	StatusNotFound           = "not_found"
	StatusAlreadyExists      = "already_exists"
	StatusPermissionDenied   = "permission_denied"
	StatusResourceExhausted  = "resource_exhausted"
	StatusFailedPrecondition = "failed_precondition"
	StatusAborted            = "aborted"
	StatusOutOfRange         = "out_of_range"
	StatusUnimplemented      = "unimplemented"
	StatusInternal           = "internal"
	StatusUnavailable        = "unavailable"
	StatusDataLoss           = "data_loss"
	StatusUnauthenticated    = "unauthenticated"
)

// codeNames maps the numeric gRPC code (also used by Connect, which mirrors
// the gRPC code numbers 1:1) to its canonical lowercase snake_case name.
// Code 0 is OK; codes 1..16 follow the standard set.
//
//nolint:gochecknoglobals // immutable lookup table; effectively a const.
var codeNames = [...]string{
	0:  StatusOK,
	1:  StatusCancelled,
	2:  StatusUnknown,
	3:  StatusInvalidArgument,
	4:  StatusDeadlineExceeded,
	5:  StatusNotFound,
	6:  StatusAlreadyExists,
	7:  StatusPermissionDenied,
	8:  StatusResourceExhausted,
	9:  StatusFailedPrecondition,
	10: StatusAborted,
	11: StatusOutOfRange,
	12: StatusUnimplemented,
	13: StatusInternal,
	14: StatusUnavailable,
	15: StatusDataLoss,
	16: StatusUnauthenticated,
}

// StatusFromCode returns the canonical name for a numeric gRPC / Connect
// code. Out-of-range values fall back to "unknown" because both protocols
// reserve unspecified codes for forward compatibility.
func StatusFromCode(code uint32) string {
	if int(code) < len(codeNames) {
		return codeNames[code]
	}

	return StatusUnknown
}

// NormalizeStatus accepts the canonical form the user wrote in
// expect { status = "..." } and returns it unchanged, or an error if the
// string does not match a known code. The check is case-insensitive so
// "OK" / "Ok" / "ok" all pass, but the returned value is always lowercase
// so assertion comparisons stay direct.
func NormalizeStatus(s string) (string, error) {
	lower := strings.ToLower(strings.TrimSpace(s))
	if lower == "" {
		return "", fmt.Errorf("rpc status is empty")
	}

	for _, name := range codeNames {
		if name == lower {
			return name, nil
		}
	}

	return "", fmt.Errorf("rpc status %q is not a canonical gRPC code (ok, invalid_argument, not_found, ...)", s)
}
