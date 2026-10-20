package tools

import "testing"

func TestJWTUsesConfiguredSecret(t *testing.T) {
	t.Setenv("GOFLY_JWT_SECRET", "test-secret")
	token, err := MakeToken(map[string]interface{}{"kefu_name": "agent"})
	if err != nil {
		t.Fatal(err)
	}
	claims := ParseToken(token)
	if claims == nil || claims["kefu_name"] != "agent" {
		t.Fatalf("unexpected claims: %#v", claims)
	}

	t.Setenv("GOFLY_JWT_SECRET", "other-secret")
	if ParseToken(token) != nil {
		t.Fatal("token should fail after the signing secret changes")
	}
}
