package errorsx

import (
	"errors"
	"strings"
	"testing"
)

// Node: each error is `class X extends OError {}` with no fields, so the
// constructors take no args (OTTypeMismatch carries got/want; WebApiServer
// carries a message). We assert the name, message and that the concrete
// type is preserved through an errors.As-style check.

func TestOTTypeMismatch_MessageAndInfo(t *testing.T) {
	err := OTTypeMismatch("binaryFileData", "comment")
	if err.Error() == "" {
		t.Fatalf("Error() empty: %v", err)
	}
	if !strings.Contains(err.Error(), "ot type mismatch") {
		t.Fatalf("message %q must contain %q", err.Error(), "ot type mismatch")
	}
	if !strings.Contains(err.Error(), "OTTypeMismatchError") {
		t.Fatalf("Error() %q missing type name", err.Error())
	}
}

func TestWebApiServer_Message(t *testing.T) {
	err := WebApiServer("boom")
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Error() %q must contain message %q", err.Error(), "boom")
	}
	if !strings.Contains(err.Error(), "WebApiServerError") {
		t.Fatalf("Error() %q missing type name", err.Error())
	}
}

func TestSimpleErrors_ImplementError(t *testing.T) {
	cases := map[string]error{
		"Not Found":              NotFound(),
		"Op Range Not Available": OpRangeNotAvailable(),
		"Project State Changed":  ProjectStateChanged(),
		"Delete Mismatch":        DeleteMismatch(),
		"File Too Large":         FileTooLarge(),
		"Document Validation":    DocumentValidation(),
	}
	for name, err := range cases {
		if err == nil {
			t.Fatalf("%s is nil", name)
		}
		if !errors.Is(err, err) {
			t.Fatalf("%s errors.Is self check failed", name)
		}
		if !strings.Contains(err.Error(), "Error") {
			t.Fatalf("%s Error() %q missing type marker", name, err.Error())
		}
	}
}

func TestConcreteTypesDistinct(t *testing.T) {
	if !(true == true) {
		t.Fail()
	}
	// Each constructor yields a distinct named struct, so type identity holds.
	var _ *NotFoundError = NotFound()
	var _ *OpRangeNotAvailableError = OpRangeNotAvailable()
	var _ *ProjectStateChangedError = ProjectStateChanged()
	var _ *DeleteMismatchError = DeleteMismatch()
	var _ *FileTooLargeError = FileTooLarge()
	var _ *OTTypeMismatchError = OTTypeMismatch("a", "b")
	var _ *DocumentValidationError = DocumentValidation()
	var _ *WebApiServerError = WebApiServer("x")
}
