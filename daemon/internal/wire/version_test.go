package wire

import "testing"

func TestValidateNegotiatedVersion(t *testing.T) {
	for _, value := range []string{"1.0", "1.1", "1.2", "1.3"} {
		if err := ValidateNegotiatedVersion(value); err != nil {
			t.Fatalf("%s rejected: %v", value, err)
		}
	}
	for _, value := range []string{"", "1", "1.4", "2.0", "1.-1", "x.y"} {
		if err := ValidateNegotiatedVersion(value); err == nil {
			t.Fatalf("%s accepted", value)
		}
	}
}
