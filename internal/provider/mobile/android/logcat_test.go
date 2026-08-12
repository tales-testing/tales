package android

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tales-testing/tales/internal/provider/mobile/android/adb"
)

// logcatADB records the arguments CaptureDeviceLog asks adb for. Every
// other method is a stub: the lifecycle only reaches Logcat here.
type logcatADB struct {
	tag      string
	maxLines int
	serial   string
}

func (f *logcatADB) Logcat(_ context.Context, serial, tag string, maxLines int) (string, error) {
	f.serial, f.tag, f.maxLines = serial, tag, maxLines

	return "07-12 14:39:25.001 E ActivityManager: ANR in com.example\n", nil
}

func (*logcatADB) Binary() string                                            { return "adb" }
func (*logcatADB) ListDevices(context.Context) ([]adb.Device, error)         { return nil, nil }
func (*logcatADB) WaitForBoot(context.Context, string, time.Duration) error  { return nil }
func (*logcatADB) APILevel(context.Context, string) (int, error)             { return 34, nil }
func (*logcatADB) Install(context.Context, string, string) error             { return nil }
func (*logcatADB) IsInstalled(context.Context, string, string) (bool, error) { return true, nil }
func (*logcatADB) Shell(context.Context, string, string) (string, error)     { return "", nil }
func (*logcatADB) Forward(context.Context, string, int, int) error           { return nil }
func (*logcatADB) RemoveForward(context.Context, string, int)                {}
func (*logcatADB) Screencap(context.Context, string, string) error           { return nil }

func TestCaptureDeviceLogIsUnfiltered(t *testing.T) {
	t.Parallel()

	fake := &logcatADB{}
	lifecycle := &Lifecycle{ADB: fake}
	path := filepath.Join(t.TempDir(), "nested", "device.log")

	if err := lifecycle.CaptureDeviceLog(context.Background(), "emulator-5554", path); err != nil {
		t.Fatalf("capture: %v", err)
	}

	// The dump used to select the "tales-driver" tag, which is the one
	// thing that cannot explain a launcher ANR, an app crash or a slow
	// cold start — the failures actually worth diagnosing.
	if fake.tag != "" {
		t.Fatalf("logcat must not filter by tag, got %q", fake.tag)
	}

	if fake.serial != "emulator-5554" {
		t.Fatalf("serial = %q", fake.serial)
	}

	if fake.maxLines != logcatLines {
		t.Fatalf("maxLines = %d, want %d", fake.maxLines, logcatLines)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if string(body) == "" {
		t.Fatal("expected the dump to be written through")
	}
}
