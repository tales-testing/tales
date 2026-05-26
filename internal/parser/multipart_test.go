package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMultipartTale(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()

	path := filepath.Join(dir, "multipart.tales")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestLoadPathMultipartBlockDecodes(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "upload" {
  step "http" "send" {
    request {
      method = "POST"
      url    = "http://example.test/upload"

      body {
        multipart {
          file {
            field        = "avatar"
            path         = "./avatar.png"
            filename     = "avatar.png"
            content_type = "image/png"
          }
          field {
            name  = "description"
            value = "user avatar"
          }
          file {
            field   = "fingerprint"
            content = "deadbeef"
          }
        }
      }
    }
  }
}
`

	suite, diags := LoadPath(writeMultipartTale(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	body := suite.Scenarios[0].Steps[0].Request.Body
	if body == nil || body.Multipart == nil {
		t.Fatalf("multipart body not decoded: %#v", body)
	}

	parts := body.Multipart.Parts
	if len(parts) != 3 {
		t.Fatalf("want 3 parts, got %d", len(parts))
	}

	if parts[0].File == nil || parts[1].Field == nil || parts[2].File == nil {
		t.Fatalf("parts not in source order: %#v", parts)
	}

	if parts[1].Field == nil {
		t.Fatalf("middle part should be a field")
	}
}

func TestLoadPathMultipartRejectsCombinedWithJSON(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "upload" {
  step "http" "send" {
    request {
      method = "POST"
      url    = "http://example.test/upload"

      body {
        json = { hello = "world" }
        multipart {
          file {
            field   = "blob"
            content = "abc"
          }
        }
      }
    }
  }
}
`

	_, diags := LoadPath(writeMultipartTale(t, content))
	if !diags.HasErrors() {
		t.Fatalf("expected error when combining json and multipart")
	}

	if !strings.Contains(diags.Error(), "exactly one of json, form, raw, or a multipart block") {
		t.Fatalf("unexpected diagnostic: %s", diags.Error())
	}
}

func TestLoadPathMultipartRejectsFileMissingBothSources(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "upload" {
  step "http" "send" {
    request {
      method = "POST"
      url    = "http://example.test/upload"

      body {
        multipart {
          file {
            field = "avatar"
          }
        }
      }
    }
  }
}
`

	_, diags := LoadPath(writeMultipartTale(t, content))
	if !diags.HasErrors() {
		t.Fatalf("expected error when multipart file has no source")
	}

	if !strings.Contains(diags.Error(), "exactly one of path or content") {
		t.Fatalf("unexpected diagnostic: %s", diags.Error())
	}
}

func TestLoadPathMultipartRejectsFileWithBothSources(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "upload" {
  step "http" "send" {
    request {
      method = "POST"
      url    = "http://example.test/upload"

      body {
        multipart {
          file {
            field   = "avatar"
            path    = "./avatar.png"
            content = "inline"
          }
        }
      }
    }
  }
}
`

	_, diags := LoadPath(writeMultipartTale(t, content))
	if !diags.HasErrors() {
		t.Fatalf("expected error when multipart file declares both path and content")
	}

	if !strings.Contains(diags.Error(), "not both") {
		t.Fatalf("unexpected diagnostic: %s", diags.Error())
	}
}

func TestLoadPathMultipartRejectsFieldMissingValue(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "upload" {
  step "http" "send" {
    request {
      method = "POST"
      url    = "http://example.test/upload"

      body {
        multipart {
          field {
            name = "description"
          }
        }
      }
    }
  }
}
`

	_, diags := LoadPath(writeMultipartTale(t, content))
	if !diags.HasErrors() {
		t.Fatalf("expected error when multipart field has no value")
	}
}

func TestLoadPathMultipartPreservesFileFieldInterleavedOrder(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "upload" {
  step "http" "send" {
    request {
      method = "POST"
      url    = "http://example.test/upload"

      body {
        multipart {
          field {
            name  = "first"
            value = "1"
          }
          file {
            field   = "blob"
            content = "x"
          }
          field {
            name  = "last"
            value = "2"
          }
        }
      }
    }
  }
}
`

	suite, diags := LoadPath(writeMultipartTale(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	parts := suite.Scenarios[0].Steps[0].Request.Body.Multipart.Parts
	if len(parts) != 3 {
		t.Fatalf("want 3 parts, got %d", len(parts))
	}

	if parts[0].Field == nil || parts[1].File == nil || parts[2].Field == nil {
		t.Fatalf("interleaved order not preserved: %#v", parts)
	}
}
