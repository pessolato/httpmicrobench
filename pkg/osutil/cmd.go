package osutil

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// ExitOnErr writes the error to stderr and exits with a
// 1 status code, only if the error provided is not nil.
func ExitOnErr(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// DrainCloseErr checks if inErr is not nil and adds the content
// of the reader to the error message for additional context.
//
// It joins all the errors that can happen when reading and closing
// the [io.ReadCloser]. It discards the remaining content of the rc
// if the inErr is nil.
func DrainCloseErr(rc io.ReadCloser, inErr error) error {
	errs := inErr
	if errs != nil {
		d, err := io.ReadAll(rc)
		errs = errors.Join(fmt.Errorf("error '%w' with content:\n%s", errs, d), err)
	}
	_, err := io.Copy(io.Discard, rc)
	return errors.Join(errs, err, rc.Close())
}
