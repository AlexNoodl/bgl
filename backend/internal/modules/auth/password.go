package auth

import "github.com/alexedwards/argon2id"

func hashPassword(password string) (string, error) {
	return argon2id.CreateHash(password, argon2id.DefaultParams)
}

func verifyPassword(password, hash string) (bool, error) {
	return argon2id.ComparePasswordAndHash(password, hash)
}
