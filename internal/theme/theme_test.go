package theme

import (
	"reflect"
	"strings"
	"testing"
)

// Every registered name resolves, carries its own name, and fills every color
// slot — an empty slot renders invisibly rather than erroring.
func TestRegistryComplete(t *testing.T) {
	for _, name := range Names() {
		th, err := Get(name)
		if err != nil {
			t.Fatalf("Get(%q): %v", name, err)
		}
		if th.Name != name {
			t.Errorf("Get(%q).Name = %q", name, th.Name)
		}
		v := reflect.ValueOf(th)
		for i := 0; i < v.NumField(); i++ {
			if v.Field(i).String() == "" {
				t.Errorf("theme %q: slot %s is empty", name, v.Type().Field(i).Name)
			}
		}
	}
}

func TestGetEmptyIsDefault(t *testing.T) {
	th, err := Get("")
	if err != nil {
		t.Fatalf("Get(\"\"): %v", err)
	}
	if th.Name != DefaultName {
		t.Errorf("Get(\"\").Name = %q, want %q", th.Name, DefaultName)
	}
}

func TestGetUnknownListsValid(t *testing.T) {
	_, err := Get("solarized")
	if err == nil {
		t.Fatal("Get(\"solarized\") succeeded, want error")
	}
	if !strings.Contains(err.Error(), "gruvbox-dark") {
		t.Errorf("error should list valid themes, got: %v", err)
	}
}
