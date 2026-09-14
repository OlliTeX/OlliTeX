// ss-cipher-probe — cross-runtime secret-cipher parity probe. Uses the SAME
// cipher code as the sitesettings feature (Open/EncryptText/DecryptText) so
// it proves Go can read Node-written `ss::` tokens and produce tokens Node
// can decrypt. Runs INSIDE the overleaf container (cipher file present).
//
//	usage:
//	  ss-cipher-probe dec <ss::blob>   -> prints plaintext (exit 1 if undecryptable)
//	  ss-cipher-probe enc <plaintext>  -> prints new ss::blob
package main

import (
	"fmt"
	"os"

	ss "ollitex/go/services/web/features/sitesettings"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: ss-cipher-probe dec|enc <value>")
		os.Exit(2)
	}
	c, err := ss.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cipher load:", err)
		os.Exit(3)
	}
	switch os.Args[1] {
	case "dec":
		p, ok := c.DecryptText(os.Args[2])
		if !ok {
			fmt.Fprintln(os.Stderr, "UNDECRYPTABLE")
			os.Exit(1)
		}
		fmt.Print(p)
	case "enc":
		b, err := c.EncryptText(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "encrypt:", err)
			os.Exit(4)
		}
		fmt.Print(b)
	default:
		fmt.Fprintln(os.Stderr, "unknown cmd", os.Args[1])
		os.Exit(2)
	}
}
