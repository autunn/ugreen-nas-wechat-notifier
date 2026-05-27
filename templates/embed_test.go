package templates

import "testing"

func TestTemplatesParse(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("embedded templates failed to parse: %v", r)
		}
	}()

	if MustParse() == nil {
		t.Fatal("expected parsed template set")
	}
}
