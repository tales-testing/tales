package android

import (
	"fmt"
	"sort"
	"strings"
)

// The two actions a permissions block can express, shared with iOS.
const (
	permissionGrant  = "grant"
	permissionRevoke = "revoke"
)

// APILevelMediaPermissions is the first API level where READ_MEDIA_*
// replaced READ_EXTERNAL_STORAGE for photo and video access.
const APILevelMediaPermissions = 33

// APILevelNotifications is the first API level where posting
// notifications became a runtime permission.
const APILevelNotifications = 33

// servicePermissions maps a Tales service name onto the Android runtime
// permissions it stands for, as a function of the device's API level.
//
// The DSL names services semantically (`camera`, `photos`) rather than
// by platform identifier, so one `permissions { }` block reads the same
// on both platforms: iOS hands the name to simctl privacy, Android
// expands it here. A service that means several permissions (location's
// fine and coarse pair) is granted as a set, because a scenario asking
// for "location" means the capability, not one half of it.
//
// Returning nil with no error means "nothing to toggle": the platform
// grants the capability at install time, so the scenario's intent is
// already satisfied.
var servicePermissions = map[string]func(apiLevel int) []string{
	"camera": func(int) []string {
		return []string{"android.permission.CAMERA"}
	},
	"microphone": func(int) []string {
		return []string{"android.permission.RECORD_AUDIO"}
	},
	"location": func(int) []string {
		return []string{
			"android.permission.ACCESS_FINE_LOCATION",
			"android.permission.ACCESS_COARSE_LOCATION",
		}
	},
	"contacts": func(int) []string {
		return []string{
			"android.permission.READ_CONTACTS",
			"android.permission.WRITE_CONTACTS",
		}
	},
	"calendar": func(int) []string {
		return []string{
			"android.permission.READ_CALENDAR",
			"android.permission.WRITE_CALENDAR",
		}
	},
	"photos": func(apiLevel int) []string {
		if apiLevel >= APILevelMediaPermissions {
			return []string{
				"android.permission.READ_MEDIA_IMAGES",
				"android.permission.READ_MEDIA_VIDEO",
			}
		}

		return []string{
			"android.permission.READ_EXTERNAL_STORAGE",
			"android.permission.WRITE_EXTERNAL_STORAGE",
		}
	},
	"notifications": func(apiLevel int) []string {
		if apiLevel < APILevelNotifications {
			// Granted at install time before API 33, so there is
			// nothing to toggle.
			return nil
		}

		return []string{"android.permission.POST_NOTIFICATIONS"}
	},
}

// permissionsFor expands a service name for the given API level.
func permissionsFor(service string, apiLevel int) ([]string, error) {
	expand, ok := servicePermissions[service]
	if !ok {
		return nil, fmt.Errorf(
			"permission service %q is not supported on android (supported: %s)",
			service, strings.Join(supportedServices(), ", "),
		)
	}

	return expand(apiLevel), nil
}

// supportedServices lists the service names, sorted, for error messages.
func supportedServices() []string {
	services := make([]string, 0, len(servicePermissions))
	for service := range servicePermissions {
		services = append(services, service)
	}

	sort.Strings(services)

	return services
}

// isUnchangeablePermission recognizes the platform's refusal to toggle a
// permission that is not a runtime one.
//
// `pm grant` fails this way when an app declares, say, INTERNET — which
// is granted at install time and cannot be revoked. A scenario that
// lists such a service is asking for a state the app already has, so
// treating the refusal as success matches the intent and keeps the step
// from failing over a no-op.
func isUnchangeablePermission(output string, err error) bool {
	haystack := strings.ToLower(output)
	if err != nil {
		haystack += " " + strings.ToLower(err.Error())
	}

	return strings.Contains(haystack, "not a changeable permission type") ||
		strings.Contains(haystack, "has not requested permission")
}
