package types

// Warn appends a warning to f.Warnings, subject to the [MaxWarnings] cap. off is the file
// offset of the problem (0 when not applicable); err is the underlying error, if any.
//
// It is a function, not a method, so that the public File embedding [File] does not
// promote it.
func Warn(f *File, stage string, off int64, err error, msg string) {
	f.Warnings = AppendWarning(f.Warnings, Warning{Stage: stage, Message: msg, Err: err, Offset: off})
}
