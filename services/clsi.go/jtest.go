package main

import (
	"fmt"
	"path"
)

func check(base, res string) (string, bool) {
	j := path.Join(base, res)
	prefix := base + "/"
	ok := len(j) >= (len(base)+1) && j[:len(base)+1] == prefix
	return j, ok
}
func main() {
	for _, c := range [][2]string{{"foo", "bar"}, {"foo", "baz/../../bar"}, {"foo", "../foobar/baz"}, {"/a/b", "main.tex"}, {"/a/b", "../../main.tex"}} {
		j, ok := check(c[0], c[1])
		fmt.Printf("%q %q -> %q ok=%v\n", c[0], c[1], j, ok)
	}
}
