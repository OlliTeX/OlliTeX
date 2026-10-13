package main
import ("fmt"; "os"; "strings"; "golang.org/x/crypto/bcrypt")
func main() {
  h := strings.TrimSpace(os.Args[1]); pw := os.Args[2]
  fmt.Println("cmp:", bcrypt.CompareHashAndPassword([]byte(h), []byte(pw)))
}
