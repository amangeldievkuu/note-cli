package store

import (
	"fmt"
	"strconv"
	"strings"
)

// NotFoundError reports that no note matches a reference.
type NotFoundError struct {
	Ref     string
	InTrash bool
}

func (e *NotFoundError) Error() string {
	where := ""
	if e.InTrash {
		where = " in trash"
	}
	if _, err := strconv.Atoi(e.Ref); err == nil {
		return fmt.Sprintf("note %s not found%s", e.Ref, where)
	}
	return fmt.Sprintf("no note matching %q%s", e.Ref, where)
}

// AmbiguousError reports that a title prefix matches several notes.
type AmbiguousError struct {
	Ref string
	IDs []int
}

func (e *AmbiguousError) Error() string {
	ids := make([]string, len(e.IDs))
	for i, id := range e.IDs {
		ids[i] = strconv.Itoa(id)
	}
	return fmt.Sprintf("%q matches notes %s; be more specific", e.Ref, strings.Join(ids, ", "))
}
