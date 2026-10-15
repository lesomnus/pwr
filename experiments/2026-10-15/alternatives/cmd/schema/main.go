package main

import (
	"encoding/json"
	lab "example.com/cel-wire-lab"
	"os"
)

func main() {
	type field struct {
		Name   string `json:"name"`
		Number int    `json:"number"`
	}
	var out []field
	for _, fd := range lab.Fields {
		out = append(out, field{string(fd.Name()), int(fd.Number())})
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		panic(err)
	}
}
