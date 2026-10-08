package main

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	h := "$2a$12$DTKs5pI8pTCJ1E6Mb8UIjOOR1s.WfKyJu1Fv9Sy9W5kNywtT0QskK"
	for _, pw := range []string{"Ol-Fixture-9x7K", "ol-fixture-9x7k"} {
		fmt.Printf("compare(%q) -> %v
", pw, bcrypt.CompareHashAndPassword([]byte(h), []byte(pw)))
	}
}
