package app

import (
	"errors"
	"log"
	"path/filepath"
	"runtime/debug"

	"attune/internal/model"
)

func recoverImportPanic(name string, err *error) {
	if recovered := recover(); recovered != nil {
		log.Printf("Recovered import panic for %q (%T)\n%s", filepath.Base(name), recovered, debug.Stack())
		*err = errors.New("unexpected error while importing this item")
	}
}

func importFileSafely(name string, importFile func() (model.Book, bool, error)) (book model.Book, duplicate bool, err error) {
	defer recoverImportPanic(name, &err)
	return importFile()
}
