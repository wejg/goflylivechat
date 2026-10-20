package tools

import (
	"errors"
	"github.com/golang-jwt/jwt"
	"os"
)

const SECRET = "golivechat"

func jwtSecret() []byte {
	if value := os.Getenv("GOFLY_JWT_SECRET"); value != "" {
		return []byte(value)
	}
	return []byte(SECRET)
}

func MakeToken(obj map[string]interface{}) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims(obj))
	tokenString, err := token.SignedString(jwtSecret())
	return tokenString, err
}
func ParseToken(tokenStr string) map[string]interface{} {
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (i interface{}, e error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return jwtSecret(), nil
	})
	if err != nil || token == nil || !token.Valid {
		return nil
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil
	}
	return claims
}
