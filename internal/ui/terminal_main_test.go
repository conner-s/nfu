package ui

import (
	"os"
	"testing"

	"github.com/conner-s/termsafe"
)

// utf8Terminal is the verdict the tests of this package run under unless one
// sets another for itself: a terminal that decodes UTF-8. Without it nothing
// is known about the terminal and everything is drawn in ASCII.
var utf8Terminal = termsafe.Terminal{
	Encoding: termsafe.EncodingUTF8,
	Reason:   termsafe.Drop.Line("LANG=en_US.UTF-8 names UTF-8"),
}

// notUTF8Terminal is a terminal that does not decode UTF-8, as the check
// reports one under LANG=C.
var notUTF8Terminal = termsafe.Terminal{
	Encoding: termsafe.EncodingNotUTF8,
	Reason:   termsafe.Drop.Line("LANG=C is the C locale, whose characters are ASCII"),
}

func TestMain(m *testing.M) {
	SetTerminal(utf8Terminal)
	os.Exit(m.Run())
}
