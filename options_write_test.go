package audiometa

import "testing"

func TestSaveOptions(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		opts := defaultSaveOptions()

		if opts.backupSuffix != "" {
			t.Errorf("expected empty backupSuffix, got %q", opts.backupSuffix)
		}
		if opts.validate {
			t.Error("expected validate to be false")
		}
		if opts.preserveModTime {
			t.Error("expected preserveModTime to be false")
		}
	})

	t.Run("WithBackup", func(t *testing.T) {
		opts := defaultSaveOptions()
		WithBackup(".bak")(opts)

		if opts.backupSuffix != ".bak" {
			t.Errorf("expected backupSuffix %q, got %q", ".bak", opts.backupSuffix)
		}
	})

	t.Run("WithValidation", func(t *testing.T) {
		opts := defaultSaveOptions()
		WithValidation()(opts)

		if !opts.validate {
			t.Error("expected validate to be true")
		}
	})

	t.Run("WithPreserveModTime", func(t *testing.T) {
		opts := defaultSaveOptions()
		WithPreserveModTime()(opts)

		if !opts.preserveModTime {
			t.Error("expected preserveModTime to be true")
		}
	})

	t.Run("all options combined", func(t *testing.T) {
		opts := defaultSaveOptions()

		// Apply all options
		options := []SaveOption{
			WithBackup(".backup"),
			WithValidation(),
			WithPreserveModTime(),
		}
		for _, opt := range options {
			opt(opts)
		}

		if opts.backupSuffix != ".backup" {
			t.Errorf("expected backupSuffix %q, got %q", ".backup", opts.backupSuffix)
		}
		if !opts.validate {
			t.Error("expected validate to be true")
		}
		if !opts.preserveModTime {
			t.Error("expected preserveModTime to be true")
		}
	})
}
