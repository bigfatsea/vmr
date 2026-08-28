// Ver 2026-08-28, by Sonnet 5

package i18n

import (
	"reflect"
	"testing"
)

// TestStoryHTML_NoNilOrEmptyFields guards against a half-translated
// StoryHTMLText: every string field must be non-empty and every func field
// non-nil in both EN and ZH, so a new dashboard label can't ship rendered
// as "" in one language.
func TestStoryHTML_NoNilOrEmptyFields(t *testing.T) {
	for _, lang := range []Lang{EN, ZH} {
		v := reflect.ValueOf(StoryHTML(lang))
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			name := v.Type().Field(i).Name
			switch f.Kind() {
			case reflect.String:
				if f.String() == "" {
					t.Errorf("lang %v: StoryHTMLText.%s is empty", lang, name)
				}
			case reflect.Func:
				if f.IsNil() {
					t.Errorf("lang %v: StoryHTMLText.%s is nil", lang, name)
				}
			}
		}
	}
}
