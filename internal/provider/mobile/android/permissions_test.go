package android

import (
	"errors"
	"strings"
	"testing"
)

func TestPermissionsForExpandsAServiceToItsWholeCapability(t *testing.T) {
	t.Parallel()

	// A scenario asking for "location" means the capability, not one
	// half of it, so both the fine and coarse permissions are granted.
	got, err := permissionsFor("location", 34)
	if err != nil {
		t.Fatalf("permissions: %v", err)
	}

	want := []string{
		"android.permission.ACCESS_FINE_LOCATION",
		"android.permission.ACCESS_COARSE_LOCATION",
	}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("location = %v, want %v", got, want)
	}
}

func TestPhotosFollowsTheMediaPermissionSplit(t *testing.T) {
	t.Parallel()

	modern, err := permissionsFor("photos", APILevelMediaPermissions)
	if err != nil {
		t.Fatalf("permissions: %v", err)
	}

	if strings.Join(modern, ",") != "android.permission.READ_MEDIA_IMAGES,android.permission.READ_MEDIA_VIDEO" {
		t.Fatalf("photos on API %d = %v", APILevelMediaPermissions, modern)
	}

	// Below API 33 the media permissions do not exist and granting them
	// fails, so the storage pair is the only thing that works.
	legacy, err := permissionsFor("photos", APILevelMediaPermissions-1)
	if err != nil {
		t.Fatalf("permissions: %v", err)
	}

	if !strings.Contains(strings.Join(legacy, ","), "READ_EXTERNAL_STORAGE") {
		t.Fatalf("photos below API %d = %v", APILevelMediaPermissions, legacy)
	}
}

func TestNotificationsAreANoopBeforeTheyBecameRuntime(t *testing.T) {
	t.Parallel()

	// Granted at install time before API 33: there is nothing to
	// toggle, and the scenario's intent is already satisfied.
	got, err := permissionsFor("notifications", APILevelNotifications-1)
	if err != nil {
		t.Fatalf("expected a no-op rather than an error, got %v", err)
	}

	if len(got) != 0 {
		t.Fatalf("expected no permissions below API %d, got %v", APILevelNotifications, got)
	}
}

func TestUnknownServiceListsTheSupportedOnes(t *testing.T) {
	t.Parallel()

	_, err := permissionsFor("bluetooth", 34)
	if err == nil {
		t.Fatal("expected an error for an unsupported service")
	}

	msg := err.Error()

	// The error is what a user reads when a cross-platform scenario
	// names a service only the other platform has, so it must say what
	// this one does support.
	for _, want := range []string{"android", "camera", "location", "photos"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error should mention %q, got: %s", want, msg)
		}
	}
}

func TestUnchangeablePermissionIsRecognized(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		output string
		err    error
		want   bool
	}{
		{
			name:   "install-time permission",
			output: "Operation not allowed: java.lang.SecurityException: Permission INTERNET is not a changeable permission type",
			want:   true,
		},
		{
			name:   "permission the app never declared",
			output: "java.lang.SecurityException: Package org.example.app has not requested permission android.permission.CAMERA",
			want:   true,
		},
		{
			name: "reason carried on the error rather than stdout",
			err:  errors.New("exit status 255: not a changeable permission type"),
			want: true,
		},
		{
			name:   "a genuine failure",
			output: "Unknown package: org.example.app",
			err:    errors.New("exit status 255"),
			want:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := isUnchangeablePermission(tc.output, tc.err); got != tc.want {
				t.Fatalf("isUnchangeablePermission = %v, want %v", got, tc.want)
			}
		})
	}
}
