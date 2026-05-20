package ring

import (
	"testing"
)

func TestTokenFor(t *testing.T) {
	if TokenFor("userID", "labels") != 2908432762 {
		t.Errorf("TokenFor(userID, labels) = %v, want 2908432762", TokenFor("userID", "labels"))
	}
}
