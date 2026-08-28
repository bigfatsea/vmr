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
	assertNoNilOrEmpty(t, "StoryHTMLText", func(l Lang) any { return StoryHTML(l) })
}

func TestCompareHTML_NoNilOrEmptyFields(t *testing.T) {
	assertNoNilOrEmpty(t, "CompareHTMLText", func(l Lang) any { return CompareHTML(l) })
}

func assertNoNilOrEmpty(t *testing.T, name string, get func(Lang) any) {
	t.Helper()
	for _, lang := range []Lang{EN, ZH} {
		v := reflect.ValueOf(get(lang))
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			fn := v.Type().Field(i).Name
			switch f.Kind() {
			case reflect.String:
				if f.String() == "" {
					t.Errorf("lang %v: %s.%s is empty", lang, name, fn)
				}
			case reflect.Func:
				if f.IsNil() {
					t.Errorf("lang %v: %s.%s is nil", lang, name, fn)
				}
			}
		}
	}
}
