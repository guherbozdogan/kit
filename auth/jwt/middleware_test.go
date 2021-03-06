package jwt

import (
	"context"
	"testing"

	"crypto/subtle"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/guherbozdogan/kit/endpoint"
)

type customClaims struct {
	MyProperty string `json:"my_property"`
	jwt.StandardClaims
}

func (c customClaims) VerifyMyProperty(p string) bool {
	return subtle.ConstantTimeCompare([]byte(c.MyProperty), []byte(p)) != 0
}

var (
	kid            = "kid"
	key            = []byte("test_signing_key")
	myProperty     = "some value"
	method         = jwt.SigningMethodHS256
	invalidMethod  = jwt.SigningMethodRS256
	mapClaims      = jwt.MapClaims{"user": "go-kit"}
	standardClaims = jwt.StandardClaims{Audience: "go-kit"}
	myCustomClaims = customClaims{MyProperty: myProperty, StandardClaims: standardClaims}
	// Signed tokens generated at https://jwt.io/
	signedKey         = "eyJhbGciOiJIUzI1NiIsImtpZCI6ImtpZCIsInR5cCI6IkpXVCJ9.eyJ1c2VyIjoiZ28ta2l0In0.14M2VmYyApdSlV_LZ88ajjwuaLeIFplB8JpyNy0A19E"
	standardSignedKey = "eyJhbGciOiJIUzI1NiIsImtpZCI6ImtpZCIsInR5cCI6IkpXVCJ9.eyJhdWQiOiJnby1raXQifQ.L5ypIJjCOOv3jJ8G5SelaHvR04UJuxmcBN5QW3m_aoY"
	customSignedKey   = "eyJhbGciOiJIUzI1NiIsImtpZCI6ImtpZCIsInR5cCI6IkpXVCJ9.eyJteV9wcm9wZXJ0eSI6InNvbWUgdmFsdWUiLCJhdWQiOiJnby1raXQifQ.s8F-IDrV4WPJUsqr7qfDi-3GRlcKR0SRnkTeUT_U-i0"
	invalidKey        = "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.e30.vKVCKto-Wn6rgz3vBdaZaCBGfCBDTXOENSo_X2Gq7qA"
)

func signingValidator(t *testing.T, signer endpoint.Endpoint, expectedKey string) {
	ctx, err := signer(context.Background(), struct{}{})
	if err != nil {
		t.Fatalf("Signer returned error: %s", err)
	}

	token, ok := ctx.(context.Context).Value(JWTTokenContextKey).(string)
	if !ok {
		t.Fatal("Token did not exist in context")
	}

	if token != expectedKey {
		t.Fatalf("JWT tokens did not match: expecting %s got %s", expectedKey, token)
	}
}

func TestNewSigner(t *testing.T) {
	e := func(ctx context.Context, i interface{}) (interface{}, error) { return ctx, nil }

	signer := NewSigner(kid, key, method, mapClaims)(e)
	signingValidator(t, signer, signedKey)

	signer = NewSigner(kid, key, method, standardClaims)(e)
	signingValidator(t, signer, standardSignedKey)

	signer = NewSigner(kid, key, method, myCustomClaims)(e)
	signingValidator(t, signer, customSignedKey)
}

func TestJWTParser(t *testing.T) {
	e := func(ctx context.Context, i interface{}) (interface{}, error) { return ctx, nil }

	keys := func(token *jwt.Token) (interface{}, error) {
		return key, nil
	}

	parser := NewParser(keys, method, jwt.MapClaims{})(e)

	// No Token is passed into the parser
	_, err := parser(context.Background(), struct{}{})
	if err == nil {
		t.Error("Parser should have returned an error")
	}

	if err != ErrTokenContextMissing {
		t.Errorf("unexpected error returned, expected: %s got: %s", ErrTokenContextMissing, err)
	}

	// Invalid Token is passed into the parser
	ctx := context.WithValue(context.Background(), JWTTokenContextKey, invalidKey)
	_, err = parser(ctx, struct{}{})
	if err == nil {
		t.Error("Parser should have returned an error")
	}

	// Invalid Method is used in the parser
	badParser := NewParser(keys, invalidMethod, jwt.MapClaims{})(e)
	ctx = context.WithValue(context.Background(), JWTTokenContextKey, signedKey)
	_, err = badParser(ctx, struct{}{})
	if err == nil {
		t.Error("Parser should have returned an error")
	}

	if err != ErrUnexpectedSigningMethod {
		t.Errorf("unexpected error returned, expected: %s got: %s", ErrUnexpectedSigningMethod, err)
	}

	// Invalid key is used in the parser
	invalidKeys := func(token *jwt.Token) (interface{}, error) {
		return []byte("bad"), nil
	}

	badParser = NewParser(invalidKeys, method, jwt.MapClaims{})(e)
	ctx = context.WithValue(context.Background(), JWTTokenContextKey, signedKey)
	_, err = badParser(ctx, struct{}{})
	if err == nil {
		t.Error("Parser should have returned an error")
	}

	// Correct token is passed into the parser
	ctx = context.WithValue(context.Background(), JWTTokenContextKey, signedKey)
	ctx1, err := parser(ctx, struct{}{})
	if err != nil {
		t.Fatalf("Parser returned error: %s", err)
	}

	cl, ok := ctx1.(context.Context).Value(JWTClaimsContextKey).(jwt.MapClaims)
	if !ok {
		t.Fatal("Claims were not passed into context correctly")
	}

	if cl["user"] != mapClaims["user"] {
		t.Fatalf("JWT Claims.user did not match: expecting %s got %s", mapClaims["user"], cl["user"])
	}

	parser = NewParser(keys, method, &jwt.StandardClaims{})(e)
	ctx = context.WithValue(context.Background(), JWTTokenContextKey, standardSignedKey)
	ctx1, err = parser(ctx, struct{}{})
	if err != nil {
		t.Fatalf("Parser returned error: %s", err)
	}
	stdCl, ok := ctx1.(context.Context).Value(JWTClaimsContextKey).(*jwt.StandardClaims)
	if !ok {
		t.Fatal("Claims were not passed into context correctly")
	}
	if !stdCl.VerifyAudience("go-kit", true) {
		t.Fatalf("JWT jwt.StandardClaims.Audience did not match: expecting %s got %s", standardClaims.Audience, stdCl.Audience)
	}

	parser = NewParser(keys, method, &customClaims{})(e)
	ctx = context.WithValue(context.Background(), JWTTokenContextKey, customSignedKey)
	ctx1, err = parser(ctx, struct{}{})
	if err != nil {
		t.Fatalf("Parser returned error: %s", err)
	}
	custCl, ok := ctx1.(context.Context).Value(JWTClaimsContextKey).(*customClaims)
	if !ok {
		t.Fatal("Claims were not passed into context correctly")
	}
	if !custCl.VerifyAudience("go-kit", true) {
		t.Fatalf("JWT customClaims.Audience did not match: expecting %s got %s", standardClaims.Audience, custCl.Audience)
	}
	if !custCl.VerifyMyProperty(myProperty) {
		t.Fatalf("JWT customClaims.MyProperty did not match: expecting %s got %s", myProperty, custCl.MyProperty)
	}
}
