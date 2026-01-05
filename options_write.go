package audiometa

// SaveOption configures behavior when saving audio files.
//
// Options use the functional options pattern for clean, extensible APIs.
//
// Example:
//
//	err := file.Save(
//	    audiometa.WithBackup(".bak"),
//	    audiometa.WithValidation(),
//	)
type SaveOption func(*saveOptions)

// saveOptions holds configuration for saving files.
type saveOptions struct {
	backupSuffix    string // Suffix for backup file (e.g., ".bak")
	validate        bool   // Re-read after write to verify
	preserveModTime bool   // Keep original modification time
}

// defaultSaveOptions returns the default configuration for saving.
func defaultSaveOptions() *saveOptions {
	return &saveOptions{
		backupSuffix:    "",
		validate:        false,
		preserveModTime: false,
	}
}

// WithBackup creates a backup of the original file before saving.
//
// The backup file will have the specified suffix appended to the original
// filename. For example, WithBackup(".bak") will create "song.mp3.bak"
// before modifying "song.mp3".
//
// If the backup file already exists, it will be overwritten.
//
// Example:
//
//	err := file.Save(audiometa.WithBackup(".bak"))
//	// Original file preserved as song.mp3.bak
func WithBackup(suffix string) SaveOption {
	return func(o *saveOptions) {
		o.backupSuffix = suffix
	}
}

// WithValidation re-reads the file after writing to verify integrity.
//
// After saving, the file is re-opened and parsed to ensure the written
// data can be read back correctly. This adds overhead but provides
// confidence that the save operation succeeded.
//
// Use this for critical operations where data integrity is paramount.
//
// Example:
//
//	err := file.Save(audiometa.WithValidation())
//	// File is re-read after save to verify
func WithValidation() SaveOption {
	return func(o *saveOptions) {
		o.validate = true
	}
}

// WithPreserveModTime keeps the original file modification time.
//
// By default, saving updates the file's modification time to the current
// time. This option preserves the original modification time.
//
// Use this when you want to maintain the original file timestamps,
// such as when updating metadata without changing the "modified" date.
//
// Example:
//
//	err := file.Save(audiometa.WithPreserveModTime())
//	// File modification time unchanged
func WithPreserveModTime() SaveOption {
	return func(o *saveOptions) {
		o.preserveModTime = true
	}
}
