package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/benmuth/go-muon/src/muon"
)

func main() {
	args := os.Args
	ifn, ofn := args[1], args[2]

	f, err := os.Open(ifn)
	if err != nil {
		panic(err) // TODO: err handling
	}
	defer f.Close()
	dec := json.NewDecoder(f)

	fmt.Println("Analysing JSON")

	d := muon.NewDictBuilder()

	d.Add(dec)
	t := d.GetDict(512)

	fmt.Println("Generating MuON")

	of, err := os.Create(ofn)
	if err != nil {
		panic(err) // TODO: err handling
	}
	defer of.Close()

	m := muon.NewMuWriter(of)
	m.TagMuon()
	if len(t) > 128 {
		tRev := make([]string, len(t))
		for i, j := 0, len(t)-1; i < len(t); i, j = i+1, j-1 {
			tRev[i] = t[j]
		}
		m.AddLRUList(tRev)
	} else {
		m.AddLRUDynamic(t)
	}
	m.Add(dec)
}
