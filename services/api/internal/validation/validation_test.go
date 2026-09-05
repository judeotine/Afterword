package validation_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/validation"
)

func TestEmptyValidatorPasses(t *testing.T) {
	v := validation.New()
	if v.Failed() {
		t.Fatal("a validator with no checks reported a failure")
	}
	if len(v.Fields()) != 0 {
		t.Fatalf("unexpected fields: %+v", v.Fields())
	}
}

func TestRequiredStringReportsBlankValues(t *testing.T) {
	v := validation.New()
	if got := v.RequiredString("title", "   ", 10); got != "" {
		t.Fatalf("blank required string returned %q", got)
	}
	if !v.Failed() {
		t.Fatal("a blank required string did not fail validation")
	}
	fields := v.Fields()
	if len(fields) != 1 || fields[0].Field != "title" {
		t.Fatalf("unexpected fields: %+v", fields)
	}
}

func TestRequiredStringTrimsAndEnforcesLength(t *testing.T) {
	v := validation.New()
	if got := v.RequiredString("title", "  Standup  ", 20); got != "Standup" {
		t.Fatalf("required string returned %q", got)
	}
	if v.Failed() {
		t.Fatalf("unexpected failure: %+v", v.Fields())
	}

	v = validation.New()
	v.RequiredString("title", "abcdef", 3)
	if !v.Failed() {
		t.Fatal("an over-long value passed validation")
	}
}

func TestOptionalStringOnlyChecksLength(t *testing.T) {
	v := validation.New()
	if got := v.OptionalString("note", "  ", 5); got != "" {
		t.Fatalf("blank optional string returned %q", got)
	}
	if v.Failed() {
		t.Fatalf("a blank optional string failed: %+v", v.Fields())
	}
	v.OptionalString("note", "abcdef", 3)
	if !v.Failed() {
		t.Fatal("an over-long optional value passed validation")
	}
}

func TestOneOfRejectsUnknownValues(t *testing.T) {
	allowed := []string{"desktop", "bot", "import"}

	v := validation.New()
	if got := v.OneOf("source", "bot", allowed, ""); got != "bot" {
		t.Fatalf("OneOf returned %q", got)
	}
	if v.Failed() {
		t.Fatalf("unexpected failure: %+v", v.Fields())
	}

	v = validation.New()
	v.OneOf("source", "carrier pigeon", allowed, "")
	if !v.Failed() {
		t.Fatal("an unknown value passed validation")
	}

	v = validation.New()
	if got := v.OneOf("source", "", allowed, "desktop"); got != "desktop" {
		t.Fatalf("OneOf fallback returned %q", got)
	}
	if v.Failed() {
		t.Fatalf("a blank value with a fallback failed: %+v", v.Fields())
	}

	v = validation.New()
	v.OneOf("source", "", allowed, "")
	if !v.Failed() {
		t.Fatal("a blank value without a fallback passed validation")
	}
}

func TestUUIDParsesAndRejects(t *testing.T) {
	id := uuid.New()

	v := validation.New()
	if got := v.UUID("folder_id", id.String()); got != id {
		t.Fatalf("UUID returned %v", got)
	}

	v = validation.New()
	v.UUID("folder_id", "not-a-uuid")
	if !v.Failed() {
		t.Fatal("a malformed uuid passed validation")
	}

	v = validation.New()
	v.UUID("folder_id", uuid.Nil.String())
	if !v.Failed() {
		t.Fatal("the nil uuid passed validation")
	}
}

func TestRangeChecks(t *testing.T) {
	v := validation.New()
	v.Min("duration_s", -1, 0)
	v.Max("page_size", 500, 100)
	if len(v.Fields()) != 2 {
		t.Fatalf("unexpected fields: %+v", v.Fields())
	}

	v = validation.New()
	v.Min("duration_s", 0, 0)
	v.Max("page_size", 100, 100)
	if v.Failed() {
		t.Fatalf("boundary values failed: %+v", v.Fields())
	}
}

func TestTimeRangeRejectsAnInvertedWindow(t *testing.T) {
	from := time.Now().UTC()
	to := from.Add(-time.Hour)

	v := validation.New()
	v.Ordered("to", from, to)
	if !v.Failed() {
		t.Fatal("an inverted window passed validation")
	}
}

func TestFieldsAreReportedInInsertionOrderWithoutDuplicates(t *testing.T) {
	v := validation.New()
	v.Add("title", "first")
	v.Add("source", "second")
	v.Add("title", "third")

	fields := v.Fields()
	if len(fields) != 2 {
		t.Fatalf("expected two fields, got %+v", fields)
	}
	if fields[0].Field != "title" || fields[0].Message != "first" || fields[1].Field != "source" {
		t.Fatalf("unexpected fields: %+v", fields)
	}
}

func TestMessageSummarisesTheFailures(t *testing.T) {
	v := validation.New()
	if v.Message() != "" {
		t.Fatalf("an empty validator produced %q", v.Message())
	}
	v.Add("title", "is required")
	if v.Message() == "" {
		t.Fatal("a failing validator produced no message")
	}
}
