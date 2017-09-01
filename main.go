package main

import (
	"fmt"
	"github.com/skycoin/cxo/node"
	"github.com/skycoin/cxo/skyobject"
	"github.com/skycoin/skycoin/src/cipher"
	"github.com/skycoin/skycoin/src/cipher/encoder"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"time"
)

func waitInterrupt(quit <-chan struct{}) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	select {
	case <-sig:
	case <-quit:
	}
}

type User struct {
	Name string
	Age  uint64
}

type List struct {
	Users skyobject.Refs `skyobject:"schema=test.User"`
}

func newNode() *node.Node {
	c := node.NewConfig()
	c.Skyobject.Registry = skyobject.NewRegistry(func(r *skyobject.Reg) {
		r.Register("test.User", User{})
		r.Register("test.List", List{})
	})
	c.InMemoryDB = true

	n, e := node.NewNode(c)
	if e != nil {
		panic(e)
	}
	return n
}

func main() {
	pk, sk := cipher.GenerateKeyPair()

	var n = newNode()
	defer n.Close()

	var quit = make(chan struct{})
	var wg sync.WaitGroup

	if e := n.AddFeed(pk); e != nil {
		panic(e)
	}

	go writeLoop(n, &wg, quit, pk, sk)

	waitInterrupt(n.Quiting())
	select {
	case quit <- struct{}{}:
	default:
	}
}

func writeLoop(
	n *node.Node,
	wg *sync.WaitGroup,
	quit chan struct{},
	pk cipher.PubKey,
	sk cipher.SecKey,
) {
	wg.Add(1)
	defer wg.Done()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	// Start : Init.

	c := n.Container()
	pack, e := c.NewRoot(
		pk, sk,
		0, // skyobject.HashTableIndex,
		c.CoreRegistry().Types(),
	)
	if e != nil {
		panic(e)
	}

	pack.Append(&List{})
	if e := pack.Save(); e != nil {
		panic(e)
	}
	n.Publish(pack.Root())

	// End : Init.

	// Start : Write Loop.

	var i uint64

	for {
		select {
		case <-quit:
			return

		case <-ticker.C:
			// New User.
			newUser := &User{
				Name: "User " + strconv.Itoa(int(i)),
				Age:  i,
			}

			// Append User to List.
			obj, _ := pack.RefByIndex(0)
			list := obj.(*List)
			if e := list.Users.Append(newUser); e != nil {
				panic(e)
			}

			// Test. Get hash.
			if elem, e := list.Users.RefByHash(cipher.SumSHA256(encoder.Serialize(newUser))); e != nil {
				panic(e)
			} else {

				// Modify user.
				newUser.Name += " (Modified)"
				if e := elem.SetValue(newUser); e != nil {
					panic(e)
				}

				// Get new hash.
				if _, e := list.Users.RefByHash(elem.Hash); e != nil {
					fmt.Println("Failed here:", e)
				}
			}

			// Save pack.
			if e := pack.SetRefByIndex(0, list); e != nil {
				panic(e)
			}
			if e := pack.Save(); e != nil {
				panic(e)
			}

			fmt.Printf("[%d] INSPECT:\n %s", i, n.Container().Inspect(pack.Root()))

			pack.Close()
			fmt.Printf("[%d] Closed pack!\n", i)

			i++
		}
	}
}
